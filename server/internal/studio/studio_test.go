package studio

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/totp"
)

// testGate открывает дверь к проверочной базе.
//
// Без строки подключения — отказ, а не пропуск: проверка, которая молча
// пропускается и возвращает успех, выдаёт зелёное за непроверенное.
func testGate(t *testing.T) *dbgate.Gate {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут. " +
			"Это отказ, а не пропуск — см. server/.env.example")
	}
	gate, err := dbgate.Open(context.Background(), dsn, dbgate.Options{})
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

func newLogin() string {
	return fmt.Sprintf("проверка-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
}

func TestPgВходПоОдноразовомуКоду(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users, sessions := NewUsers(gate, nil), NewSessions(gate)
	desk := NewDesk(users, sessions)

	login := newLogin()
	if _, _, err := users.Create(ctx, login, "Составитель", []Permission{PermSourceRead}); err != nil {
		t.Fatal(err)
	}
	_, secret, err := users.ByLogin(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	token, err := desk.Login(ctx, login, code)
	if err != nil {
		t.Fatalf("вход по годному коду отказал: %v", err)
	}
	if token == "" {
		t.Fatal("вход прошёл, а токен пуст")
	}

	user, err := sessions.User(ctx, users, token, time.Now())
	if err != nil {
		t.Fatalf("сессия не найдена сразу после выдачи: %v", err)
	}
	if user.Login != login {
		t.Errorf("сессия принадлежит %q, а выдана %q", user.Login, login)
	}
}

func TestPgЧужойКодНеПускает(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users, sessions := NewUsers(gate, nil), NewSessions(gate)
	desk := NewDesk(users, sessions)

	login := newLogin()
	if _, _, err := users.Create(ctx, login, "Составитель", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := desk.Login(ctx, login, "000000"); err == nil {
		t.Fatal("вход прошёл по коду, которого не выдавали")
	}
}

func TestPgОтключённыйНеВходитИНеХодитПоСтаройСессии(t *testing.T) {
	// Увольнение обязано вступать в силу сразу, а не через сутки — столько
	// живёт уже выданная сессия.
	ctx := context.Background()
	gate := testGate(t)
	users, sessions := NewUsers(gate, nil), NewSessions(gate)

	login := newLogin()
	user, secret, err := users.Create(ctx, login, "Бывший", []Permission{PermSourceRead})
	if err != nil {
		t.Fatal(err)
	}
	token, err := sessions.Issue(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx, `UPDATE users SET disabled_at = NOW() WHERE id = $1`, user.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := sessions.User(ctx, users, token, time.Now()); err == nil {
		t.Error("отключённый пользователь ходит по выданной ранее сессии")
	}
	code, _ := totp.Code(secret, time.Now())
	if _, err := users.VerifyCode(ctx, login, code, time.Now()); err == nil {
		t.Error("отключённый пользователь вошёл заново")
	}
}

func TestPgИстёкшаяСессияОтказывает(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users, sessions := NewUsers(gate, nil), NewSessions(gate)

	login := newLogin()
	user, _, err := users.Create(ctx, login, "Составитель", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Часы двигаются, а не ожидание: ждать сутки проверка не может.
	token, err := sessions.Issue(ctx, user.ID, time.Now().Add(-2*SessionTTL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.User(ctx, users, token, time.Now()); err == nil {
		t.Error("истёкшая сессия пустила")
	}
}

func TestPgМаршрутЗакрытПравом(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users, sessions := NewUsers(gate, nil), NewSessions(gate)
	desk := NewDesk(users, sessions)

	desk.Handle(PermSourceAccept, "GET /admin/api/проверка", func(w http.ResponseWriter, r *http.Request, u User) {
		WriteJSON(w, http.StatusOK, map[string]string{"кто": u.Login})
	})

	// У пользователя есть право читать источники, но не принимать разбор.
	login := newLogin()
	user, _, err := users.Create(ctx, login, "Читатель", []Permission{PermSourceRead})
	if err != nil {
		t.Fatal(err)
	}
	token, err := sessions.Issue(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/проверка", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	desk.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("код %d, ожидался 403: маршрут открылся без права", rec.Code)
	}

	// Без сессии вовсе — «войдите заново», а не «нет права»: человеку надо
	// понять, что делать.
	rec = httptest.NewRecorder()
	desk.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/api/проверка", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("код %d, ожидался 401", rec.Code)
	}

	// А с правом — открывается.
	login2 := newLogin()
	user2, _, err := users.Create(ctx, login2, "Приёмщик", []Permission{PermSourceAccept})
	if err != nil {
		t.Fatal(err)
	}
	token2, err := sessions.Issue(ctx, user2.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/admin/api/проверка", nil)
	req.Header.Set("Authorization", "Bearer "+token2)
	rec = httptest.NewRecorder()
	desk.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("код %d, ожидался 200: право есть, а маршрут закрыт", rec.Code)
	}
}

func TestPgСекретНеЛежитВТокене(t *testing.T) {
	// Утёкшая база не должна давать входа: в admin_sessions лежит
	// отпечаток, а не токен.
	ctx := context.Background()
	gate := testGate(t)
	users, sessions := NewUsers(gate, nil), NewSessions(gate)

	login := newLogin()
	user, _, err := users.Create(ctx, login, "Составитель", nil)
	if err != nil {
		t.Fatal(err)
	}
	token, err := sessions.Issue(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var stored int
	err = gate.QueryRow(ctx,
		`SELECT count(*) FROM admin_sessions WHERE token_hash = $1`, token).Scan(&stored)
	if err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Error("токен лежит в базе как есть")
	}
}

func TestPgСессииУбираютсяПоСроку(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users, sessions := NewUsers(gate, nil), NewSessions(gate)

	login := newLogin()
	user, _, err := users.Create(ctx, login, "Составитель", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Issue(ctx, user.ID, time.Now().Add(-2*SessionTTL)); err != nil {
		t.Fatal(err)
	}
	n, err := sessions.Sweep(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Error("истёкшая сессия не убрана")
	}
}

func TestНеизвестноеПравоОтказывает(t *testing.T) {
	if _, err := ParsePermissions([]string{"source:read", "придуманное"}); err == nil {
		t.Fatal("несуществующее право принято")
	}
	if _, err := ParsePermissions([]string{"source:read"}); err != nil {
		t.Fatalf("известное право отвергнуто: %v", err)
	}
}

func TestМаршрутНеизвестнымПравомНеОбъявляется(t *testing.T) {
	// Маршрут, закрытый несуществующим правом, закрыт для всех — и
	// заметили бы это не сразу и не там. Поэтому отказ при сборке.
	defer func() {
		if recover() == nil {
			t.Fatal("маршрут с несуществующим правом объявлен молча")
		}
	}()
	desk := NewDesk(nil, nil)
	desk.Handle(Permission("придуманное"), "GET /admin/api/что-то", nil)
}

func TestPgПереборКодаУпираетсяВПредел(t *testing.T) {
	// Шесть цифр — это миллион вариантов на три годных в каждый момент.
	// Защитой это становится только тогда, когда попытки считают.
	ctx := context.Background()
	gate := testGate(t)
	desk := NewDesk(NewUsers(gate, nil), NewSessions(gate))
	// Задержка подменяется пустышкой: она здесь не предмет проверки, а
	// настоящая растянула бы дюжину попыток на минуту.
	desk.SetHold(func(context.Context, time.Duration) {})

	login := newLogin()
	if _, _, err := NewUsers(gate, nil).Create(ctx, login, "Составитель", nil); err != nil {
		t.Fatal(err)
	}
	_, secret, err := NewUsers(gate, nil).ByLogin(ctx, login)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < guardLimit; i++ {
		if _, err := desk.Login(ctx, login, "000000"); err == nil {
			t.Fatalf("попытка %d прошла по коду, которого не выдавали", i+1)
		}
	}

	// Годный код после исчерпанных попыток тоже не пускает: считается
	// число попыток, а не их удачность, — иначе перебор просто продолжался
	// бы до совпадения.
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := desk.Login(ctx, login, code); err == nil {
		t.Fatal("после исчерпанных попыток вход прошёл: перебор ничем не ограничен")
	}
}

func TestPgГодныйКодНеПроходитДважды(t *testing.T) {
	// Код годен своё окно и оба соседних — почти полторы минуты. Всё это
	// время подсмотренный через плечо код работал бы второй раз.
	ctx := context.Background()
	gate := testGate(t)
	users := NewUsers(gate, nil)
	desk := NewDesk(users, NewSessions(gate))
	desk.SetHold(func(context.Context, time.Duration) {})

	login := newLogin()
	if _, _, err := users.Create(ctx, login, "Составитель", nil); err != nil {
		t.Fatal(err)
	}
	_, secret, err := users.ByLogin(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := desk.Login(ctx, login, code); err != nil {
		t.Fatalf("вход по годному коду отказал: %v", err)
	}
	if _, err := desk.Login(ctx, login, code); err == nil {
		t.Fatal("тот же код пустил второй раз")
	}
}

func TestPgОтказВходаНичегоНеРассказывает(t *testing.T) {
	// Неизвестное имя и негодный код обязаны отличаться только тем, что
	// пишется в журнал. Разные отказы наружу — это ответ на вопрос,
	// существует ли имя.
	ctx := context.Background()
	gate := testGate(t)
	desk := NewDesk(NewUsers(gate, nil), NewSessions(gate))
	desk.SetHold(func(context.Context, time.Duration) {})

	login := newLogin()
	if _, _, err := NewUsers(gate, nil).Create(ctx, login, "Составитель", nil); err != nil {
		t.Fatal(err)
	}

	known := errorOf(desk.Login(ctx, login, "000000"))
	unknown := errorOf(desk.Login(ctx, newLogin(), "000000"))
	if known == nil || unknown == nil {
		t.Fatal("вход прошёл по коду, которого не выдавали")
	}
	if !errors.Is(known, ErrGate) || !errors.Is(unknown, ErrGate) {
		t.Fatalf("отказ входа не единственный: %v против %v", known, unknown)
	}
}

func errorOf(_ string, err error) error { return err }

// Оберег мастерской держится против двух снятий разом.
//
// Показанный аудитом дефект: счёт оставшихся и правка шли двумя запросами
// без замка, и два снятия, пришедшие в один миг, видели друг друга живыми
// и проходили оба. Из сорока кругов тридцать девять оставляли установку
// без единого человека с правом мастерской.
//
// Кругов здесь столько же: один круг проходит и на сломанном коде — беда
// эта из тех, что случаются не каждый раз, и проверка в один круг
// объявила бы её почищенной, ничего не проверив.
func TestPgПоследнийМастерНеСнимаетсяДвумяСразу(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users := NewUsers(gate, nil)
	const rounds = 40

	for round := 0; round < rounds; round++ {
		a, b := newLogin(), newLogin()
		for _, login := range []string{a, b} {
			if _, _, err := users.Create(ctx, login, login, []Permission{PermWorkshop}); err != nil {
				t.Fatal(err)
			}
		}
		// Кроме этих двоих мастеров в базе быть не должно, иначе оберег
		// и не обязан срабатывать.
		if _, err := gate.Exec(ctx, `UPDATE users SET disabled_at = NOW()
		     WHERE login <> $1 AND login <> $2 AND 'workshop' = ANY (permissions)`,
			a, b); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, login := range []string{a, b} {
			wg.Add(1)
			go func(login string) {
				defer wg.Done()
				<-start
				_ = users.SetPermissions(ctx, login, []Permission{PermCaseRead})
			}(login)
		}
		close(start)
		wg.Wait()

		var left int
		if err := gate.QueryRow(ctx, `SELECT count(*) FROM users
		     WHERE disabled_at IS NULL AND 'workshop' = ANY (permissions)`).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left == 0 {
			t.Fatalf("круг %d: права мастерской не осталось ни у кого", round)
		}
	}
}

// Отказ оберега называет себя, а не тупик в базе.
//
// Замок мог бы стоять по строкам оставшихся мастеров, и тогда два снятия
// встали бы во взаимное ожидание: человек получил бы «обнаружен тупик»
// вместо объяснения, почему право не снято. Проверка сторожит именно
// слова отказа.
func TestPgОберегМастерскойОтказываетВнятно(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users := NewUsers(gate, nil)

	only := newLogin()
	if _, _, err := users.Create(ctx, only, only, []Permission{PermWorkshop}); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx, `UPDATE users SET disabled_at = NOW()
	     WHERE login <> $1 AND 'workshop' = ANY (permissions)`, only); err != nil {
		t.Fatal(err)
	}

	err := users.SetPermissions(ctx, only, []Permission{PermCaseRead})
	if !errors.Is(err, ErrLastWorkshop) {
		t.Fatalf("снятие с последнего мастера отказало не оберегом: %v", err)
	}

	// И правка при отказе не применилась наполовину: транзакция откатана.
	user, _, err := users.ByLogin(ctx, only)
	if err != nil {
		t.Fatal(err)
	}
	has := false
	for _, p := range user.Permissions {
		if p == PermWorkshop {
			has = true
		}
	}
	if !has {
		t.Error("право мастерской снято, хотя оберег отказал")
	}
}

func TestPgПеченьеПускаетПослеПерезагрузкиСтраницы(t *testing.T) {
	// Токен студии живёт в памяти страницы, и до печенья F5, закрытая
	// вкладка и уснувший ноутбук выбрасывали на вход при какой угодно
	// сессии на сервере. Перезагрузка изображена здесь честно: обращение
	// без заголовка Authorization, с одним печеньем.
	ctx := context.Background()
	gate := testGate(t)
	users := NewUsers(gate, nil)
	desk := NewDesk(users, NewSessions(gate))
	Routes(desk)

	login := newLogin()
	if _, _, err := users.Create(ctx, login, "Составитель", nil); err != nil {
		t.Fatal(err)
	}
	_, secret, err := users.ByLogin(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(fmt.Sprintf(`{"login":%q,"code":%q}`, login, code))
	rec := httptest.NewRecorder()
	desk.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/api/login", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("вход по годному коду ответил %d", rec.Code)
	}
	jar := onlyCookie(t, rec)

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/me", nil)
	req.AddCookie(jar)
	desk.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("после перезагрузки страницы студия ответила %d: вход не пережил её", rec.Code)
	}

	// Выход закрывает сессию и гасит печенье. Второе без первого оставило
	// бы живой токен в браузере до конца месяца.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/logout", nil)
	req.AddCookie(jar)
	desk.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("выход ответил %d", rec.Code)
	}
	if onlyCookie(t, rec).MaxAge >= 0 {
		t.Error("выход не велел браузеру забыть печенье")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/me", nil)
	req.AddCookie(jar)
	desk.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("после выхода печенье всё ещё пускает: ответ %d", rec.Code)
	}
}
