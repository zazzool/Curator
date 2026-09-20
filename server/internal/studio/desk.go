package studio

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"
)

// Desk — маршруты студии.
//
// # Маршрута без права не бывает
//
// Право стоит первым доводом при объявлении каждого маршрута, и этого не
// даёт забыть компилятор: другого способа объявить маршрут у студии нет.
// Это не соглашение «не забывайте проверять права» — соглашения забывают,
// а подпись функции забыть нельзя.
//
// Маршрут, открытый всем (вход, выход), объявляется отдельным именем
// Public — чтобы в коде было видно, что открытость тут названа вслух, а не
// получилась из недосмотра.
type Desk struct {
	mux      *http.ServeMux
	users    *Users
	sessions *Sessions
	guard    *guard

	// now — источник времени. Полем, а не time.Now по месту: проверке
	// нужно подвинуть часы, чтобы увидеть истёкшую сессию, а ждать сутки
	// она не может.
	now func() time.Time

	// hold — чем придерживается ответ на неудачный вход. Полем по тому же
	// доводу, что и часы: проверке сторожа нужны десятки попыток подряд, а
	// настоящая задержка растянула бы её на минуты.
	hold func(context.Context, time.Duration)

	// secureCookies — ставить ли печенью сессии признак Secure.
	//
	// Решается по схеме адреса контура, и решает это main: адрес объявлен
	// там в одном месте, и второе место для того же расходится молча.
	// Умолчание — false: собранный без настройки стол стоит в проверке или
	// на своей машине, то есть по http, а Secure-печенье браузер по http
	// молча выбрасывает.
	secureCookies bool
}

// NewDesk собирает стол студии.
func NewDesk(users *Users, sessions *Sessions) *Desk {
	return &Desk{
		mux:      http.NewServeMux(),
		users:    users,
		sessions: sessions,
		guard:    newGuard(),
		now:      time.Now,
		hold:     sleep,
	}
}

// sleep придерживает ответ, но не дольше, чем ждёт сам обратившийся.
//
// Через контекст, а не голым time.Sleep: оборвавший обращение не должен
// держать поток сервера — перебирающий рвёт соединения именно затем.
func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// Handler отдаёт собранные маршруты.
func (d *Desk) Handler() http.Handler { return d.mux }

// Handle объявляет маршрут, закрытый правом.
//
// Обработчик получает пользователя уже проверенным: искать его самому —
// значит повторять проверку в каждом обработчике, а повторённая проверка
// однажды окажется другой.
func (d *Desk) Handle(perm Permission, pattern string, h func(http.ResponseWriter, *http.Request, User)) {
	if !Known(perm) {
		// Отказ при сборке, а не при обращении: маршрут, закрытый
		// несуществующим правом, закрыт для всех, и заметили бы это не
		// сразу и не там.
		panic("маршрут " + pattern + " объявлен несуществующим правом " + string(perm))
	}
	d.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		user, err := d.sessions.User(r.Context(), d.users, presented(r), d.now())
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "Войдите в студию заново")
			return
		}
		if !user.Can(perm) {
			// Отказ именно «нет права», а не «не найдено»: человек,
			// которому не дали права, должен понять, что просить, а не
			// думать, что раздел сломался.
			WriteError(w, http.StatusForbidden, "У вас нет права на этот раздел студии")
			return
		}
		h(w, r, user)
	})
}

// Public объявляет маршрут, открытый всем.
func (d *Desk) Public(pattern string, h http.HandlerFunc) {
	d.mux.HandleFunc(pattern, h)
}

// SetClock подменяет часы. Только для проверок.
func (d *Desk) SetClock(now func() time.Time) { d.now = now }

// SetHold подменяет задержку неудачного входа. Только для проверок.
func (d *Desk) SetHold(hold func(context.Context, time.Duration)) { d.hold = hold }

// SetSecureCookies объявляет, что контур отдаётся по https.
func (d *Desk) SetSecureCookies(secure bool) { d.secureCookies = secure }

// ErrGate — единственный отказ входа.
//
// Один на все случаи намеренно: неизвестное имя, негодный код, закрытый
// вход, непривязанный аутентификатор, исчерпанные попытки — снаружи всё это
// обязано выглядеть одинаково. Разные отказы отвечают на вопрос, который
// ответом не считается: существует ли такое имя и что с ним не так.
//
// Причина при этом не теряется: она уходит в журнал, где её читаем мы, а не
// тот, кто стучится.
var ErrGate = errors.New("имя или код не подошли")

// Login — вход по имени и одноразовому коду.
//
// Порядок здесь не случайный, и переставить его нельзя:
//
//  1. сторож спрашивается ДО обращения к базе — иначе перебор бесплатно
//     нагружает базу запросом на каждую попытку;
//  2. удачный код гасится сразу — годен он три окна подряд, и подсмотренный
//     через плечо код иначе работает второй раз;
//  3. ответ на неудачу придерживается, и тем дольше, чем больше их было
//     подряд.
//
// Придержанная попытка всё равно считается: не считай мы упёршихся в
// предел, окно наблюдения истекало бы прямо под перебором.
func (d *Desk) Login(ctx context.Context, login, code string) (string, error) {
	now := d.now()
	if !d.guard.allow(login, now) {
		d.hold(ctx, d.guard.note(login, now))
		log.Printf("вход не состоялся: имя %q, попытки исчерпаны", guardKey(login))
		return "", ErrGate
	}

	user, err := d.users.VerifyCode(ctx, login, code, now)
	if err == nil && !d.guard.consume(login, code, now) {
		err = errors.New("код уже предъявляли")
	}
	if err != nil {
		d.hold(ctx, d.guard.note(login, now))
		// Сам код в журнал не пишется: он годен ещё полторы минуты, а
		// журнал читают не только те, кому можно входить.
		log.Printf("вход не состоялся: имя %q, %v", guardKey(login), err)
		return "", ErrGate
	}

	d.guard.clear(login)
	return d.sessions.Issue(ctx, user.ID, now)
}

// WriteError отдаёт отказ разговорным текстом.
//
// Текст пишется так, будто его читает врач, а не программист: «Войдите в
// студию заново» вместо «unauthorized». Человек, увидевший второе, зовёт
// нас; увидевший первое — входит заново.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// WriteJSON отдаёт ответ.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Ответ уже начат, и сказать клиенту больше нечего — остаётся
		// журнал. Молчать здесь нельзя: это единственный след того, что
		// ответ уехал оборванным.
		log.Printf("ответ не дописан: %v", err)
	}
}
