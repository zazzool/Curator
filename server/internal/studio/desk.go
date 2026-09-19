package studio

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
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

	// now — источник времени. Полем, а не time.Now по месту: проверке
	// нужно подвинуть часы, чтобы увидеть истёкшую сессию, а ждать сутки
	// она не может.
	now func() time.Time
}

// NewDesk собирает стол студии.
func NewDesk(users *Users, sessions *Sessions) *Desk {
	return &Desk{
		mux:      http.NewServeMux(),
		users:    users,
		sessions: sessions,
		now:      time.Now,
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
		user, err := d.sessions.User(r.Context(), d.users, bearer(r), d.now())
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

// Login — вход по имени и одноразовому коду.
func (d *Desk) Login(ctx context.Context, login, code string) (string, error) {
	user, err := d.users.VerifyCode(ctx, login, code, d.now())
	if err != nil {
		return "", err
	}
	return d.sessions.Issue(ctx, user.ID, d.now())
}

// bearer достаёт токен из заголовка.
func bearer(r *http.Request) string {
	head := r.Header.Get("Authorization")
	if head == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(head, prefix) {
		return ""
	}
	return strings.TrimSpace(head[len(prefix):])
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
