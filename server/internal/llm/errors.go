package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured — обращаться некуда: ключ не задан.
var ErrNotConfigured = errors.New("генерация недоступна: не задан ключ провайдера")

// ErrTruncated — модель не уложилась в потолок ответа.
//
// Отдельной ошибкой, а не текстом на месте, потому что род у неё особый:
// заплачено полностью, получено ничто. Учёт обязан отличать её от прочих
// отказов (те не стоят ничего), а лечится она потолком ответа, а не сменой
// провайдера — перебор в этом случае платит за обрыв столько раз, сколько
// провайдеров в списке.
var ErrTruncated = errors.New("модель не уложилась в потолок ответа — ответ оборван")

// ErrOutOfCredits — у платформы кончились средства у поставщика моделей.
//
// Отличается от прочих отказов тем, что относится не к обращению, а ко
// всей работе: следующее обращение упрётся в то же самое, и задание,
// продолжающее работу после него, тратит минуты на то, чтобы записать одну
// и ту же строку столько раз, сколько пунктов было в заказе.
//
// Текст обращён к человеку, потому что читать его будет он. У донора сюда
// уезжал ответ провайдера как есть — «402: This request would exceed your
// available credits given your current in-flight requests», — и строка про
// in-flight requests не говорит составителю ни что случилось, ни что
// делать. Делать ему и нечего: счёт не его.
var ErrOutOfCredits = errors.New(
	"заказать сейчас нельзя: у платформы кончились средства у поставщика моделей. " +
		"Это чинится не вами: счёт пополняем мы, а написанное остаётся на месте")

// OutOfCredits опознаёт пустой счёт в чём угодно, во что его успели
// завернуть по дороге.
func OutOfCredits(err error) bool {
	if errors.Is(err, ErrOutOfCredits) {
		return true
	}
	var pe *ProviderError
	return errors.As(err, &pe) && pe.Status == http.StatusPaymentRequired
}

// ProviderError — отказ одного провайдера.
type ProviderError struct {
	Provider string
	Status   int
	Err      error

	// RetryAfter — сколько провайдер попросил подождать (заголовок
	// Retry-After). Ноль означает «не сказал».
	RetryAfter time.Duration

	// TimedOut — истекло наше собственное ожидание, а не пришёл отказ.
	// Повторять такое тому же провайдеру дорого (см. worthRepeating).
	TimedOut bool
}

func (e *ProviderError) Error() string { return fmt.Sprintf("%s: %v", e.Provider, e.Err) }

func (e *ProviderError) Unwrap() error { return e.Err }

// retryable — стоит ли спросить соседа.
//
// Отказ отказу рознь. Кончился баланс, отвалился ключ, лёг сервис — это
// беда конкретного провайдера, и сосед справится. А вот наш собственный
// кривой запрос сосед отвергнет точно так же: перебирать в этом случае
// значит потратить все ключи на одну и ту же ошибку.
func (e *ProviderError) retryable() bool {
	switch {
	case e.Status == 0: // сеть, таймаут, прокси недоступен
		return true
	case e.Status == 400 || e.Status == 422:
		return false // запрос неверен — у соседа будет то же самое
	default:
		return true
	}
}

// worthRepeating — стоит ли повторить тому же провайдеру.
//
// Условие строже, чем у retryable, и это разные вопросы. Отвалившийся ключ
// (401) у соседа сработает, а у этого же провайдера через секунду — нет:
// повтор потратит время и ничего не изменит. Повторять стоит только то,
// что проходит само: превышение частоты, перегрузку, сбой на их стороне и
// обрыв связи.
//
// Список сверен с описанием ошибок Claude API: 429 — частота, 500 —
// внутренняя ошибка, 504 — их таймаут, 529 — перегрузка. Прочие 4xx
// (401 ключ, 402 оплата, 403 доступ, 404 адрес, 413 размер) сами не
// проходят.
func (e *ProviderError) worthRepeating() bool {
	switch {
	case e.TimedOut:
		// Ожидание истекло. Повтор стоит ещё трёх минут, а модель, не
		// уложившаяся в срок, обычно не укладывается и со второго раза.
		// Соседа спросить стоит, повторять этому же — нет.
		return false
	case e.Status == 0: // отказ в соединении, обрыв, мёртвый прокси
		return true
	case e.Status == http.StatusTooManyRequests:
		return true
	case e.Status >= 500:
		return true
	default:
		return false
	}
}

// timedOut — истекло ли наше собственное ожидание.
//
// Отличать это от прочих сетевых сбоев приходится потому, что цена повтора
// у них разная. Отказ в соединении или сброс мгновенны, и повторить их
// дёшево. А наш таймаут — это уже три минуты ожидания, и повторить его
// значит ждать девять; человек всё это время смотрит на «модель пишет».
func timedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// --- Признаки: в чём именно провайдер обвинил запрос ---

// refusalSigns — слова, которыми провайдеры отказывают в поле запроса.
//
// Формулировок много: шлюз пересказывает ответ вышестоящего, а пишут они
// по-своему. Общее у них — «не понял», «не поддерживаю», «не принимаю»,
// сказанное про названное поле.
var refusalSigns = []string{
	"not supported",
	"unsupported",
	"not support",  // «does not support streaming»
	"unrecognized", // «unrecognized request argument supplied»
	"unknown",
	"not allowed",
	"not available",
	"invalid",
}

// refusalNames — назвал ли провайдер в отказе одно из этих полей и обвинил
// ли его. Упоминание поля само по себе не обвинение: провайдер называет
// поля и в пояснениях, и в описании ограничений.
func refusalNames(err error, fields ...string) bool {
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Err == nil {
		return false
	}
	msg := strings.ToLower(pe.Err.Error())
	named := false
	for _, field := range fields {
		if strings.Contains(msg, field) {
			named = true
			break
		}
	}
	if !named {
		return false
	}
	for _, sign := range refusalSigns {
		if strings.Contains(msg, sign) {
			return true
		}
	}
	return false
}

// schemaRefused — обвинил ли провайдер схему ответа.
func schemaRefused(err error) bool {
	return refusalNames(err, "response_format", "json_schema", "structured output")
}
