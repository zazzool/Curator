package app

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Ручки почты: привязка и возврат доступа.
//
// # Почему возврат доступа стоит за ключом программы, а не за токеном
//
// Врач приходит сюда ровно тогда, когда токена у него уже нет: телефон
// сменился, приложение поставлено заново. Закрой мы эти две ручки токеном
// устройства — они открывались бы только тому, кому и не нужны.
//
// # Почему ответ всегда один и тот же
//
// Ручка, отвечающая «такой почты у нас нет», — это способ проверить,
// заведён ли у нас конкретный человек. Почта врача обычно рабочая и легко
// угадывается, так что перебор по списку кафедры занял бы вечер. Поэтому
// ответ здесь не зависит ни от того, знаем ли мы адрес, ни от того,
// отправили ли мы письмо.

// Postman — кто уносит письмо.
//
// Интерфейсом, а не mail.Sender: проверке нужно посмотреть, что именно
// ушло врачу, а живого почтового ящика у неё нет. Доводов у него два, и
// оба нужны: Ready отличает «почта не настроена» от «письмо не дошло», и
// сказать это врачу надо по-разному.
type Postman interface {
	Ready() bool
	Send(to, subject, body string) error
}

// EmailRoutes объявляет ручки почты.
func EmailRoutes(door *Door, emails *Emails, post Postman, now func() time.Time) {
	r := &emailRoutes{emails: emails, post: post, now: now}

	door.Device("POST /v1/me/email", r.startBind)
	door.Device("POST /v1/me/email/confirm", r.confirmBind)

	door.Keyed("POST /v1/recovery", r.startRecovery)
	door.Keyed("POST /v1/recovery/confirm", r.confirmRecovery)
}

type emailRoutes struct {
	emails *Emails
	post   Postman
	now    func() time.Time
}

type emailRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`

	// About приезжает при возврате доступа: устройство заводится тут же, и
	// рассказать о себе ему больше негде.
	About
}

// startBind отправляет код на указанный адрес.
func (r *emailRoutes) startBind(w http.ResponseWriter, req *http.Request, caller Caller) {
	var body emailRequest
	_ = DecodeBody(req, &body)

	if !r.post.Ready() {
		// Прямо и сразу: обещать письмо там, где отправлять его нечем, —
		// значит отправить врача ждать кода, которого не будет.
		WriteError(w, http.StatusServiceUnavailable,
			"Отправка писем пока не настроена. Напишите нам, и мы привяжем почту руками")
		return
	}

	code, err := r.emails.StartBind(req.Context(), caller.AccountID, body.Email, r.now())
	switch {
	case errors.Is(err, ErrTooOften):
		WriteError(w, http.StatusTooManyRequests, "Письмо уже отправлено. Подождите минуту")
		return
	case err != nil:
		WriteError(w, http.StatusBadRequest, "Это не похоже на адрес почты")
		return
	}

	// Пустой код означает, что адрес занят другой записью. Ответ тот же:
	// иначе ручка рассказывает, заведён ли у нас такой врач.
	if code != "" {
		if err := r.post.Send(Normalize(body.Email),
			"Куратор: подтверждение почты", bindLetter(code)); err != nil {
			WriteError(w, http.StatusBadGateway,
				"Письмо не ушло. Проверьте адрес и попробуйте ещё раз")
			return
		}
	}
	WriteJSON(w, http.StatusAccepted, map[string]any{
		"sent": true,
		"hint": "Код придёт письмом и будет годен 15 минут",
	})
}

// confirmBind сверяет код и привязывает почту.
func (r *emailRoutes) confirmBind(w http.ResponseWriter, req *http.Request, caller Caller) {
	var body emailRequest
	_ = DecodeBody(req, &body)

	email, err := r.emails.ConfirmBind(req.Context(), caller.AccountID, body.Code, r.now())
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Код не подошёл или устарел. Запросите новый")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"email": email})
}

// startRecovery отправляет код на привязанный адрес.
func (r *emailRoutes) startRecovery(w http.ResponseWriter, req *http.Request, _ string) {
	var body emailRequest
	_ = DecodeBody(req, &body)

	if !r.post.Ready() {
		WriteError(w, http.StatusServiceUnavailable,
			"Отправка писем пока не настроена. Напишите нам, и мы вернём доступ руками")
		return
	}

	// Отказ базы сюда не просачивается намеренно: и он, и «такой почты нет»
	// отвечают одинаково. Иначе отличить одно от другого снаружи было бы
	// можно — а значит, можно было бы и перебирать адреса.
	code, err := r.emails.StartRecovery(req.Context(), body.Email, r.now())
	if err == nil && code != "" {
		_ = r.post.Send(Normalize(body.Email),
			"Куратор: возврат доступа", recoveryLetter(code))
	}

	WriteJSON(w, http.StatusAccepted, map[string]any{
		"sent": true,
		"hint": "Если эта почта привязана, письмо с кодом уже отправлено",
	})
}

// confirmRecovery сверяет код и заводит устройство на найденной записи.
func (r *emailRoutes) confirmRecovery(w http.ResponseWriter, req *http.Request, _ string) {
	var body emailRequest
	_ = DecodeBody(req, &body)

	device, err := r.emails.ConfirmRecovery(req.Context(),
		body.Email, body.Code, body.About, r.now())
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Код не подошёл или устарел. Запросите новый")
		return
	}
	// Тот же вид, что у заведения устройства: приложение разбирает оба
	// ответа одним кодом, и расхождение здесь стоило бы отдельной ветки
	// разбора ради одного и того же токена.
	WriteJSON(w, http.StatusCreated, map[string]any{
		"token":     device.Token,
		"accountId": device.AccountID,
		"deviceId":  device.DeviceID,
	})
}

// bindLetter — письмо с кодом привязки.
//
// Письма пишутся здесь, а не в mail: там перевозка, здесь смысл. И пишутся
// так, будто их читает врач: что за код, куда его вводить и сколько он
// годен. Письмо без этих трёх строк выглядит как рассылка и отправляется
// в спам руками получателя.
func bindLetter(code string) string {
	return fmt.Sprintf(`Здравствуйте!

Код подтверждения почты: %s

Введите его в приложении «Куратор», в разделе «Профиль». Код годен
15 минут и работает один раз.

Если почту подтверждали не вы — просто не вводите код, ничего не
произойдёт.`, code)
}

// recoveryLetter — письмо с кодом возврата доступа.
//
// Тон другой намеренно: это письмо получает и тот, у кого пытаются увести
// запись, и сказать ему надо прямо, что делать.
func recoveryLetter(code string) string {
	return fmt.Sprintf(`Здравствуйте!

Код для возврата доступа: %s

Введите его в приложении «Куратор» на новом устройстве. Код годен
15 минут и работает один раз. Ваши задачи, знаки и купленные наборы
останутся при вас.

Если доступ восстанавливали не вы — не вводите код и напишите нам:
кто-то знает вашу почту и пробует войти.`, code)
}
