package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/totp"
)

// aloneInWorkshop оставляет в мастерской одного названного человека.
//
// Отказ «последний мастер» считает мастеров по всей установке, и увидеть
// его можно только там, где их пересчитали. Проверочная база одноразовая,
// но проверки в ней идут подряд, и прежнее состояние возвращается в конце —
// иначе следующая проверка получила бы установку без единого мастера.
func aloneInWorkshop(t *testing.T, gate *dbgate.Gate, keep string) {
	t.Helper()
	ctx := context.Background()
	rows, err := gate.Query(ctx,
		`SELECT login FROM users
		  WHERE login <> $1 AND disabled_at IS NULL AND $2 = ANY (permissions)`,
		keep, string(PermWorkshop))
	if err != nil {
		t.Fatal(err)
	}
	others := []string{}
	for rows.Next() {
		var login string
		if err := rows.Scan(&login); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		others = append(others, login)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for _, login := range others {
		if _, err := gate.Exec(ctx,
			`UPDATE users SET disabled_at = now() WHERE login = $1`, login); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, login := range others {
			if _, err := gate.Exec(context.Background(),
				`UPDATE users SET disabled_at = NULL WHERE login = $1`, login); err != nil {
				t.Errorf("мастер %q не возвращён на место: %v", login, err)
			}
		}
	})
}

func TestPgСписокПользователейОтдаётПраваИНеОтдаётСекрет(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users := NewUsers(gate, nil)

	login := newLogin()
	if _, _, err := users.Create(ctx, login, "Продавец",
		[]Permission{PermSales, PermClients}); err != nil {
		t.Fatal(err)
	}

	list, err := users.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found *Shown
	for i := range list {
		if list[i].Login == login {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("заведённого %q нет в списке", login)
	}
	if len(found.Permissions) != 2 {
		t.Errorf("прав %d, ожидалось 2: %v", len(found.Permissions), found.Permissions)
	}
	if found.Disabled {
		t.Error("только что заведённый значится отключённым")
	}
	if found.CreatedAt.IsZero() {
		t.Error("дата заведения пуста")
	}
}

func TestPgПраваМеняютсяИНеизвестноеОтвергается(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users := NewUsers(gate, nil)

	login := newLogin()
	if _, _, err := users.Create(ctx, login, "Составитель",
		[]Permission{PermSourceRead}); err != nil {
		t.Fatal(err)
	}

	err := users.SetPermissions(ctx, login, []Permission{Permission("сам-себе-право")})
	if err == nil {
		t.Fatal("право не из словаря принято: словарь закрыт именно затем, чтобы такого не было")
	}

	if err := users.SetPermissions(ctx, login,
		[]Permission{PermSourceRead, PermCaseWrite}); err != nil {
		t.Fatal(err)
	}
	user, _, err := users.ByLogin(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	if !user.Can(PermCaseWrite) {
		t.Error("выданное право не читается обратно")
	}
}

func TestPgОтключениеЗакрываетВходИОткрываетОбратно(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users := NewUsers(gate, nil)

	login := newLogin()
	if _, _, err := users.Create(ctx, login, "Уволенный",
		[]Permission{PermSourceRead}); err != nil {
		t.Fatal(err)
	}
	_, secret, err := users.ByLogin(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	code := func() string {
		c, err := totp.Code(secret, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	if err := users.SetDisabled(ctx, login, true); err != nil {
		t.Fatal(err)
	}
	if _, err := users.VerifyCode(ctx, login, code(), time.Now()); err == nil {
		t.Error("отключённый вошёл по годному коду")
	}

	// Отметка, а не удаление строки: имя в приходах и правках обязано
	// остаться читаемым, и вернуть человека можно без заведения заново.
	if err := users.SetDisabled(ctx, login, false); err != nil {
		t.Fatal(err)
	}
	if _, err := users.VerifyCode(ctx, login, code(), time.Now()); err != nil {
		t.Errorf("возвращённый на место не вошёл: %v", err)
	}
}

func TestPgПоследнегоМастераНеСнимают(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	users := NewUsers(gate, nil)

	master := newLogin()
	if _, _, err := users.Create(ctx, master, "Мастер",
		[]Permission{PermWorkshop}); err != nil {
		t.Fatal(err)
	}
	aloneInWorkshop(t, gate, master)

	err := users.SetPermissions(ctx, master, []Permission{PermSourceRead})
	if !errors.Is(err, ErrLastWorkshop) {
		t.Fatalf("право мастерской снято с последнего: %v", err)
	}
	if err := users.SetDisabled(ctx, master, true); !errors.Is(err, ErrLastWorkshop) {
		t.Fatalf("последний мастер отключён: %v", err)
	}

	// Появился второй — первого отпускают: считается не «сам ли ты это
	// делаешь», а сколько мастеров останется.
	second := newLogin()
	if _, _, err := users.Create(ctx, second, "Второй мастер",
		[]Permission{PermWorkshop}); err != nil {
		t.Fatal(err)
	}
	if err := users.SetPermissions(ctx, master, []Permission{PermSourceRead}); err != nil {
		t.Errorf("при втором мастере право не снялось: %v", err)
	}
}

// workshopDesk собирает стол с ручками пользователей и входом мастера.
func workshopDesk(t *testing.T, gate *dbgate.Gate, perms ...Permission) (*Desk, string, string) {
	t.Helper()
	ctx := context.Background()
	users, sessions := NewUsers(gate, nil), NewSessions(gate)
	desk := NewDesk(users, sessions)
	UserRoutes(desk)

	login := newLogin()
	user, _, err := users.Create(ctx, login, "Мастер", perms)
	if err != nil {
		t.Fatal(err)
	}
	token, err := sessions.Issue(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return desk, token, login
}

func ask(t *testing.T, desk *Desk, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	desk.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPgРучкиПользователейЗакрытыПравомМастерской(t *testing.T) {
	gate := testGate(t)
	desk, token, _ := workshopDesk(t, gate, PermSales)

	rec := ask(t, desk, token, http.MethodGet, "/admin/api/users", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("код %d, ожидался 403: список пользователей открылся без права мастерской", rec.Code)
	}
}

func TestPgЗаведениеЧерезРучкуОтдаётРабочийСекретОдинРаз(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	desk, token, _ := workshopDesk(t, gate, PermWorkshop)

	login := newLogin()
	rec := ask(t, desk, token, http.MethodPost, "/admin/api/users", map[string]any{
		"login": login, "displayName": "Новичок",
		"permissions": []string{"case:read"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	var made struct {
		Secret string `json:"secret"`
		Note   string `json:"note"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	if made.Secret == "" || made.Note == "" {
		t.Fatalf("секрет или предупреждение не отданы: %s", rec.Body.String())
	}
	if _, err := NewUsers(gate, nil).VerifyCode(ctx, login,
		codeFor(t, made.Secret), time.Now()); err != nil {
		t.Errorf("по отданному секрету вход не работает: %v", err)
	}

	// Второй раз секрет не показывают: в списке его нет и быть не может.
	rec = ask(t, desk, token, http.MethodGet, "/admin/api/users", nil)
	if bytes.Contains(rec.Body.Bytes(), []byte(made.Secret)) {
		t.Error("секрет аутентификатора виден в списке пользователей")
	}
}

func codeFor(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestPgРучкаОтвечаетНаПоследнегоМастераОтдельнымКодом(t *testing.T) {
	gate := testGate(t)
	// Мастер снимает себя сам, и это ровно тот случай, ради которого
	// отказ заведён: он и есть последний, кому есть кого заводить.
	desk, token, master := workshopDesk(t, gate, PermWorkshop)
	aloneInWorkshop(t, gate, master)

	rec := ask(t, desk, token, http.MethodPut,
		"/admin/api/users/"+master+"/disabled", map[string]any{"disabled": true})
	// 409, а не 400: запрос верный, негоден момент, и студия должна сказать
	// «сначала дайте право второму», а не «исправьте поле».
	if rec.Code != http.StatusConflict {
		t.Fatalf("код %d, ожидался 409: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error == "" {
		t.Error("отказ приехал без текста")
	}
}
