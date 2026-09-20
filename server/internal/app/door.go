// Дверь приложения: /v1.
//
// Третий этап сквозного пути — раздача. Сюда стучится приложение врача, и
// устроена эта дверь иначе, чем дверь студии, по одной причине: за ней
// стоит не сотрудник, а установленная сборка, которую никто не обновит по
// нашей просьбе.
//
// # Формат меняется только добавлением
//
// Смысл существующего поля не меняется никогда, поле не исчезает никогда.
// Стережёт это записанный эталон ответов (shared/wire-contract.json), и
// отсутствие эталона роняет проверку, а не пропускает её. Правило начинает
// действовать с первой сборки на руках у врача — до неё формат правится
// свободно, и это единственное окно.
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"curator/server/internal/limits"
)

// Door — маршруты приложения.
//
// # Маршрута без двери не бывает
//
// Маршрут объявляется одним из трёх имён, и каждое называет вслух, кого
// оно пускает: Open — всякого, Keyed — сборку с ключом программы, Device —
// устройство с токеном. Четвёртого способа объявить маршрут у приложения
// нет, и этого не даёт забыть компилятор — ровно как у студии с правами.
type Door struct {
	mux      *http.ServeMux
	keys     *Keys
	accounts *Accounts
}

func NewDoor(keys *Keys, accounts *Accounts) *Door {
	return &Door{mux: http.NewServeMux(), keys: keys, accounts: accounts}
}

func (d *Door) Handler() http.Handler { return d.mux }

// Open объявляет маршрут, открытый всякому.
//
// Отдельным именем, чтобы в коде было видно: открытость тут названа
// вслух, а не получилась из недосмотра.
func (d *Door) Open(pattern string, h http.HandlerFunc) { d.mux.HandleFunc(pattern, h) }

// Keyed объявляет маршрут, закрытый ключом программы.
//
// Ключ — это «какая сборка стучится», а не «кто стучится». Им закрыто то,
// что происходит до учётной записи: заведение устройства и справка о
// службе. Без ключа их пришлось бы открыть всякому, и первый же
// любопытный завёл бы нам миллион учётных записей.
func (d *Door) Keyed(pattern string, h func(http.ResponseWriter, *http.Request, string)) {
	d.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		keyID, err := d.keys.Check(r.Context(), r.Header.Get("X-App-Key"))
		if err != nil {
			// Один и тот же отказ на отсутствующий и на негодный ключ:
			// разные ответы рассказали бы постороннему, какой ключ
			// существует.
			WriteError(w, http.StatusUnauthorized, "Приложение не опознано. Обновите его до свежей сборки")
			return
		}
		h(w, r, keyID)
	})
}

// Metered оборачивает обработчик ведром жетонов по адресу обращающегося.
//
// Стоит на объявлении маршрута, а не внутри обработчика: так видно,
// какие ручки считаются, прямо в списке маршрутов. Считаются те, что
// работают ДО учётной записи и тратят наше от имени постороннего:
// заведение устройства и письмо на чужой адрес. Ключ программы им не
// защита — он лежит в сборке, то есть у всякого, кто её разобрал, и
// пояснение к Keyed это признаёт.
func Metered(
	b *limits.Bucket,
	now func() time.Time,
	message string,
	h func(http.ResponseWriter, *http.Request, string),
) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, keyID string) {
		if !b.Allow(limits.Address(r), now()) {
			// Ни срока, ни счёта в ответе: и то, и другое говорит
			// перебирающему, когда возвращаться. Retry-After — для
			// исправного клиента, который читает заголовки, а не текст.
			w.Header().Set("Retry-After", "60")
			WriteError(w, http.StatusTooManyRequests, message)
			return
		}
		h(w, r, keyID)
	}
}

// Device объявляет маршрут, закрытый токеном устройства.
//
// Обработчик получает учётную запись уже найденной: искать её самому —
// значит повторять проверку в каждом обработчике, а повторённая проверка
// однажды окажется другой.
func (d *Door) Device(pattern string, h func(http.ResponseWriter, *http.Request, Caller)) {
	d.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		caller, err := d.accounts.ByToken(r.Context(), bearer(r))
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "Устройство не опознано. Войдите заново")
			return
		}
		if caller.Blocked {
			// Отдельный отказ, а не «не опознано»: заблокированный врач
			// должен понять, что писать в поддержку, а не переустанавливать
			// приложение по кругу.
			WriteError(w, http.StatusForbidden, "Доступ к этой учётной записи закрыт. Напишите нам")
			return
		}
		h(w, r, caller)
	})
}

// Caller — кто стучится: устройство и его учётная запись.
type Caller struct {
	AccountID int64
	DeviceID  int64
	Blocked   bool
}

// WriteJSON отдаёт ответ.
//
// Заголовок ставится всегда и с кодировкой: приложение разбирает ответ по
// нему, а не по догадке, и ответ без кодировки приезжает на части
// устройств кракозябрами — на тех самых, где текст по-русски.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Заголовки уже ушли, и поправить ответ нечем. Но промолчать
		// нельзя: отказ кодирования означает, что врач получил обрывок.
		log.Printf("/v1: ответ не дописан: %v", err)
	}
}

// WriteError отдаёт отказ.
//
// Текст пишется по-русски и говорит, что делать: его читает врач, а не
// программист. «Ошибка 401» отправляет его переустанавливать приложение.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// DecodeBody разбирает тело запроса.
//
// В отличие от студии, лишние поля НЕ запрещены: на руках у врачей стоят
// сборки, которые обновятся не завтра, и новая сборка, пославшая поле,
// которого старый сервер не знает, должна работать, а не падать. Это та же
// обратная совместимость, только с другой стороны провода.
func DecodeBody(r *http.Request, into any) error {
	return json.NewDecoder(r.Body).Decode(into)
}

// bearer достаёт токен из заголовка.
func bearer(r *http.Request) string {
	raw := r.Header.Get("Authorization")
	if len(raw) > 7 && strings.EqualFold(raw[:7], "Bearer ") {
		return strings.TrimSpace(raw[7:])
	}
	return ""
}

// fingerprint — отпечаток секрета.
//
// Ни токен устройства, ни ключ программы, ни код с почты не хранятся
// текстом: утёкшая база не должна давать входа. Соль не нужна — это не
// пароль, который человек придумал и повторил на другом сайте, а наша
// собственная случайная строка достаточной длины.
func fingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
