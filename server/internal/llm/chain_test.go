package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// stub поднимает подставного провайдера с постоянным ответом и отдаёт его
// настройку.
func stub(t *testing.T, name string, status int, body string) (ProviderConfig, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return ProviderConfig{
		Name: name, Kind: KindOpenAI, BaseURL: srv.URL,
		APIKey: "ключ", Model: "модель", Enabled: true,
	}, &calls
}

func chain(t *testing.T, cfgs ...ProviderConfig) *Chain {
	t.Helper()
	return NewChain(func() []ProviderConfig { return cfgs }, "https://curator.example", "Куратор")
}

func TestПереборДоходитДоЗдоровогоСоседа(t *testing.T) {
	// Кончился баланс, отвалился ключ, лёг сервис — это беда конкретного
	// провайдера, и сосед справится.
	лежит, лежитCalls := stub(t, "лежит", 503, `{"error":{"message":"down"}}`)
	здоров, здоровCalls := stub(t, "здоров", 200, okOpenAI)

	text, _, err := chain(t, лежит, здоров).Generate(context.Background(), Prompt{}, "")
	if err != nil {
		t.Fatalf("перебор не дошёл до здорового: %v", err)
	}
	if text != "ответ" {
		t.Fatalf("ответ не тот: %q", text)
	}
	if здоровCalls.Load() != 1 {
		t.Fatal("здорового не спросили")
	}
	// Лежачего спросили трижды: 5xx проходит сам, и повтор на месте дешевле
	// перехода к соседу.
	if лежитCalls.Load() != 3 {
		t.Fatalf("лежачего спросили %d раз вместо трёх", лежитCalls.Load())
	}
}

func TestКривойЗапросНеПеребираетПровайдеров(t *testing.T) {
	// Наш собственный кривой запрос сосед отвергнет точно так же:
	// перебирать значит потратить все ключи на одну и ту же ошибку.
	первый, _ := stub(t, "первый", 400, `{"error":{"message":"bad request"}}`)
	второй, второйCalls := stub(t, "второй", 200, okOpenAI)

	if _, _, err := chain(t, первый, второй).Generate(context.Background(), Prompt{}, ""); err == nil {
		t.Fatal("кривой запрос прошёл")
	}
	if второйCalls.Load() != 0 {
		t.Fatal("на кривом запросе потратили второго провайдера")
	}
}

func TestОбрывПоПотолкуНеПеребираетПровайдеров(t *testing.T) {
	// Потолок ответа назначаем мы сами и один на всех, а значит второй
	// провайдер упрётся в него ровно так же. Разница с прочими отказами в
	// том, что этот оплачен: у донора счёт выставлялся столько раз,
	// сколько провайдеров в списке.
	первый, _ := stub(t, "первый", 200,
		`{"choices":[{"message":{"content":"обрыв"},"finish_reason":"length"}],"usage":{}}`)
	второй, второйCalls := stub(t, "второй", 200, okOpenAI)

	_, _, err := chain(t, первый, второй).Generate(context.Background(), Prompt{}, "")
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("обрыв не опознан: %v", err)
	}
	if второйCalls.Load() != 0 {
		t.Fatal("за обрыв заплатили дважды: спросили и соседа")
	}
}

func TestПустойСчётНазываетсяЧеловеческимиСловами(t *testing.T) {
	// Врач английского кода отказа не заказывал, а делать ему и нечего:
	// счёт не его.
	пустой, _ := stub(t, "пустой", 402,
		`{"error":{"message":"This request would exceed your available credits"}}`)
	_, _, err := chain(t, пустой).Generate(context.Background(), Prompt{}, "")
	if !errors.Is(err, ErrOutOfCredits) {
		t.Fatalf("пустой счёт не опознан: %v", err)
	}
	if !OutOfCredits(err) {
		t.Fatal("пустой счёт не опознаётся снаружи")
	}
}

func TestЗаписываетсяКаждоеОбращениеАНеТолькоНеудачное(t *testing.T) {
	// Сбой объясняется соседями по заданию: тем, что перебор дошёл до
	// второго провайдера, хотя задание в итоге собралось. Половина задания
	// не объясняет ничего.
	лежит, _ := stub(t, "лежит", 503, `{}`)
	здоров, _ := stub(t, "здоров", 200, okOpenAI)

	var written []Attempt
	c := chain(t, лежит, здоров)
	c.Record = func(a Attempt) { written = append(written, a) }
	if _, _, err := c.Generate(context.Background(), Prompt{}, ""); err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("записано %d обращений вместо двух", len(written))
	}
	if written[0].Err == nil || written[1].Err != nil {
		t.Fatal("записано не то: первый отказал, второй ответил")
	}
	if written[1].Ordinal != 2 {
		t.Fatalf("порядковый номер отвечавшего — %d: по нему видно, что основной лёг",
			written[1].Ordinal)
	}
}

func TestПровайдерБезКлючаПропускается(t *testing.T) {
	// Он объявлен, но не настроен. Отказ по нему рассказал бы про
	// устройство настроек тому, кто спрашивал задачу.
	безКлюча, безКлючаCalls := stub(t, "без ключа", 200, okOpenAI)
	безКлюча.APIKey = ""
	здоров, _ := stub(t, "здоров", 200, okOpenAI)

	if _, _, err := chain(t, безКлюча, здоров).Generate(context.Background(), Prompt{}, ""); err != nil {
		t.Fatal(err)
	}
	if безКлючаCalls.Load() != 0 {
		t.Fatal("провайдера без ключа всё-таки спросили")
	}
}

func TestБезПровайдеровОтказ_АНеПустота(t *testing.T) {
	// Пустота здесь была бы хуже отказа: её работа приняла бы за правду и
	// записала бы как результат.
	_, _, err := chain(t).Generate(context.Background(), Prompt{}, "")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("без провайдеров получили не отказ: %v", err)
	}
}
