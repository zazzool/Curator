package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Проверки идут против подставного провайдера на httptest: сети здесь нет
// и быть не должно — набор, ходящий к настоящей модели, стоит денег,
// зависит от чужой доступности и краснеет не от своей работы.

// answer — что подставной провайдер ответит и с каким кодом.
type answer struct {
	status  int
	body    string
	headers map[string]string
}

// provider поднимает подставного провайдера, отвечающего по очереди, и
// собирает клиента к нему. Возвращает ещё и счётчик обращений: без него
// повтор не отличить от его отсутствия.
func provider(t *testing.T, kind Kind, answers ...answer) (*Client, *atomic.Int32, *[]map[string]any) {
	t.Helper()
	var calls atomic.Int32
	bodies := &[]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		raw, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &parsed)
		}
		*bodies = append(*bodies, parsed)

		a := answers[len(answers)-1]
		if n < len(answers) {
			a = answers[n]
		}
		for k, v := range a.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.body)
	}))
	t.Cleanup(srv.Close)

	return NewClient(ProviderConfig{
		Name: "подставной", Kind: kind, BaseURL: srv.URL,
		APIKey: "ключ", Model: "модель", Enabled: true,
	}, "https://curator.example", "Куратор"), &calls, bodies
}

const okOpenAI = `{"choices":[{"message":{"content":"ответ"},"finish_reason":"stop"}],
	"usage":{"prompt_tokens":10,"completion_tokens":5}}`

func TestОбращениеВозвращаетТекстИТокены(t *testing.T) {
	client, _, _ := provider(t, KindOpenAI, answer{status: 200, body: okOpenAI})
	text, usage, err := client.Generate(context.Background(), Prompt{System: "с", User: "у"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "ответ" || usage.PromptTokens != 10 || usage.CompletionTokens != 5 {
		t.Fatalf("ответ разобран не тем: %q %+v", text, usage)
	}
}

func TestПревышениеЧастотыПовторяется(t *testing.T) {
	// 429 значит «слишком часто, подожди», и со второй попытки проходит. У
	// донора до этой заплаты одно превышение частоты роняло задание
	// целиком, и составитель шёл заказывать заново.
	client, calls, _ := provider(t, KindOpenAI,
		answer{status: 429, body: `{"error":{"message":"rate limit"}}`,
			headers: map[string]string{"Retry-After": "0"}},
		answer{status: 200, body: okOpenAI},
	)
	if _, _, err := client.Generate(context.Background(), Prompt{}, ""); err != nil {
		t.Fatalf("повтор не выручил: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("обращений было %d, а должно быть два", got)
	}
}

func TestОтвалившийсяКлючНеПовторяется(t *testing.T) {
	// 401 у того же провайдера через секунду не заработает: повтор
	// потратит время и ничего не изменит. Соседа спросить стоит — это
	// другой вопрос и другая проверка.
	client, calls, _ := provider(t, KindOpenAI,
		answer{status: 401, body: `{"error":{"message":"no key"}}`})
	if _, _, err := client.Generate(context.Background(), Prompt{}, ""); err == nil {
		t.Fatal("отказ по ключу прошёл")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("обращений было %d: отвалившийся ключ повторять незачем", got)
	}
}

func TestОбрывПоПотолкуНазываетсяСвоимИменем(t *testing.T) {
	// Обрыв даёт синтаксически битый JSON. Сказать об этом прямо полезнее,
	// чем показать «ошибку разбора»: лечится он потолком ответа, а не
	// сменой провайдера.
	client, _, _ := provider(t, KindOpenAI, answer{status: 200,
		body: `{"choices":[{"message":{"content":"обрыв"},"finish_reason":"length"}],"usage":{}}`})
	text, _, err := client.Generate(context.Background(), Prompt{}, "")
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("обрыв не опознан: %v", err)
	}
	if text != "обрыв" {
		// Оплаченный обрывок отдаётся вместе с отказом: заплачено за него
		// полностью, и выбрасывать его молча незачем.
		t.Fatalf("обрывок потерян: %q", text)
	}
}

func TestСхемаСнимаетсяТолькоКогдаЕёНазвалиВиноватой(t *testing.T) {
	// Схема держит формат ответа. Отказ, не назвавший ничего, схемы не
	// лишает: снять её по догадке значит получить свободный текст, разбор
	// которого либо падает, либо — что хуже — проходит наполовину.
	schema := json.RawMessage(`{"type":"object"}`)

	// Провайдер, назвавший схему: повтор без неё и успех.
	client, calls, bodies := provider(t, KindOpenAI,
		answer{status: 400, body: `{"error":{"message":"response_format is not supported"}}`},
		answer{status: 200, body: okOpenAI},
	)
	client.cfg.BaseURL = openrouterLike(client.cfg.BaseURL)
	if _, _, err := client.Generate(context.Background(),
		Prompt{Schema: schema, SchemaName: "разбор"}, ""); err != nil {
		t.Fatalf("повтор без схемы не выручил: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("обращений %d: названная виноватой схема должна сниматься", calls.Load())
	}
	if _, ok := (*bodies)[1]["response_format"]; ok {
		t.Fatal("во втором заходе схема осталась")
	}

	// Провайдер, не назвавший ничего: второго захода нет вовсе.
	mute, muteCalls, _ := provider(t, KindOpenAI,
		answer{status: 400, body: `{"error":{"message":"bad request"}}`})
	mute.cfg.BaseURL = openrouterLike(mute.cfg.BaseURL)
	if _, _, err := mute.Generate(context.Background(),
		Prompt{Schema: schema, SchemaName: "разбор"}, ""); err == nil {
		t.Fatal("безымянный отказ прошёл")
	}
	if muteCalls.Load() != 1 {
		t.Fatalf("обращений %d: отказ, не назвавший поля, схемы не лишает", muteCalls.Load())
	}
}

func TestЦенаСчитаетсяНазваннойТолькоТам_ГдеЕёНазывают(t *testing.T) {
	// Бесплатная модель отвечает честным нулём, и принять его за «не
	// сказали» значило бы дописать ей цену по прайсу. Признак — не
	// величина, а шлюз.
	body := `{"choices":[{"message":{"content":"ответ"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"cost":0}}`

	обычный, _, _ := provider(t, KindOpenAI, answer{status: 200, body: body})
	_, usage, err := обычный.Generate(context.Background(), Prompt{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if usage.CostExact {
		t.Fatal("цена посчитана названной у шлюза, который её не называет")
	}

	маршрутизатор, _, _ := provider(t, KindOpenAI, answer{status: 200, body: body})
	маршрутизатор.cfg.BaseURL = openrouterLike(маршрутизатор.cfg.BaseURL)
	_, usage, err = маршрутизатор.Generate(context.Background(), Prompt{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !usage.CostExact {
		t.Fatal("честный ноль от называющего цену шлюза принят за «не сказали»")
	}
}

func TestДиалектAnthropicИдётСвоимПутём(t *testing.T) {
	client, _, bodies := provider(t, KindAnthropic, answer{status: 200,
		body: `{"content":[{"type":"text","text":"ответ"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":7,"output_tokens":3,"cache_read_input_tokens":4}}`})
	text, usage, err := client.Generate(context.Background(), Prompt{System: "с", User: "у"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "ответ" || usage.PromptTokens != 7 || usage.CachedTokens != 4 {
		t.Fatalf("ответ messages-диалекта разобран не тем: %q %+v", text, usage)
	}
	// Системная часть уходит блоком с пометкой кэша: она статична между
	// запросами, а чтение из кэша стоит десятую часть цены.
	if _, ok := (*bodies)[0]["system"].([]any); !ok {
		t.Fatalf("системная часть ушла без пометки кэша: %v", (*bodies)[0]["system"])
	}
}

func TestОжиданиеПеребиваетсяОтменой(t *testing.T) {
	// Отмена во время ожидания — это уход того, кто ждал, а не отказ
	// провайдера, и ждать дальше незачем.
	client, _, _ := provider(t, KindOpenAI,
		answer{status: 429, body: `{}`, headers: map[string]string{"Retry-After": "60"}})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, _, err := client.Generate(ctx, Prompt{}, ""); err == nil {
		t.Fatal("отмена не прервала ожидание")
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("ожидание тянулось %s: отмена не услышана", took)
	}
}

func TestЗаголовокRetryAfterЧитаетсяЧисломИДатой(t *testing.T) {
	// Провайдер знает, когда его можно спрашивать, лучше нас, и говорит
	// это двумя способами.
	if got := parseRetryAfter("3"); got != 3*time.Second {
		t.Fatalf("число секунд разобрано как %s", got)
	}
	at := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(at); got <= 0 || got > 3*time.Second {
		t.Fatalf("дата разобрана как %s", got)
	}
	if got := parseRetryAfter("вчера"); got != 0 {
		t.Fatalf("непонятое применено: %s", got)
	}
}

// openrouterLike делает адрес подставного провайдера похожим на
// маршрутизатор.
//
// Признаки шлюза (точная цена, схема ответа, пометка кэша) выводятся из
// адреса, и проверке нужен адрес, а не настоящий OpenRouter. Имя
// дописывается путём, а не в имя узла: узел должен по-прежнему
// разрешаться в подставного провайдера, иначе проверка пойдёт в сеть — а
// в сеть она ходить не должна.
func openrouterLike(base string) string {
	return base + "/openrouter.ai"
}
