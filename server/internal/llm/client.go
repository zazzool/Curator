package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client — клиент одного провайдера.
type Client struct {
	cfg ProviderConfig

	// Клиент HTTP собирается ОДИН раз на провайдера, а не на обращение.
	//
	// Собирался он первой строкой каждого do, и каждый новый Transport
	// начинал с пустого пула соединений: TLS-рукопожатие на всякое
	// обращение к модели, а брошенные простаивать соединения прежнего
	// висели до полутора минут. На потоке генерации это сотни лишних
	// рукопожатий в час и сотни висящих сокетов, и увидеть это в журнале
	// нельзя ничем.
	//
	// Отказ разбора адреса прокси запоминается здесь же: собрать клиента
	// в NewClient и промолчать об отказе значило бы ходить мимо прокси —
	// то есть напрямую к провайдеру оттуда, откуда напрямую нельзя.
	http    *http.Client
	httpErr error

	// origin и title уезжают заголовками атрибуции. Даются снаружи, а не
	// берутся здесь из окружения: адрес контура задан в одном месте, и
	// второе место для него разошлось бы с первым молча.
	origin string
	title  string
}

// NewClient собирает клиента провайдера.
func NewClient(cfg ProviderConfig, origin, title string) *Client {
	c := &Client{cfg: cfg, origin: origin, title: title}
	c.http, c.httpErr = newHTTPClient(cfg)
	return c
}

// Timeout — сколько ждать ответа.
//
// Написание задачи — это тысячи токенов и десятки секунд; три минуты
// покрывают его с запасом. Виды работы покороче (пересказ фразы, вычитка)
// назначают своё ожидание контекстом вызова: они идут при человеке,
// смотрящем в экран, и долгое молчание он истолкует как поломку.
const Timeout = 180 * time.Second

func (c *Client) Name() string { return c.cfg.Name }

// EffectiveModel — какой моделью пойдёт обращение.
func (c *Client) EffectiveModel(requested string) string {
	if requested != "" {
		return requested
	}
	return c.cfg.Model
}

// newHTTPClient собирает клиента под канал этого провайдера.
//
// Прокси задаётся только здесь: через него ходит клиент провайдера и
// больше никто.
func newHTTPClient(cfg ProviderConfig) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.ProxyURL != "" {
		u, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("адрес прокси не разобран: %w", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Timeout: Timeout, Transport: transport}, nil
}

// providerRetries — сколько раз повторить обращение к тому же провайдеру.
//
// Два, как в официальных клиентах Anthropic и OpenAI. Больше не значит
// лучше: превышение частоты снимается первой же паузой, а если провайдер
// лежит по-настоящему, ждать его втрое дольше значит задерживать переход к
// соседу, который, может быть, здоров.
const providerRetries = 2

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	if c.httpErr != nil {
		return nil, 0, &ProviderError{Provider: c.cfg.Name, Err: c.httpErr}
	}
	httpClient := c.http

	var rawBody []byte
	var err error
	if body != nil {
		rawBody, err = json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.Kind == KindAnthropic {
		req.Header.Set("x-api-key", c.cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		// OpenRouter просит эти заголовки для атрибуции; остальным
		// провайдерам они не мешают.
		req.Header.Set("HTTP-Referer", c.origin)
		req.Header.Set("X-Title", c.title)
	}

	// Повтор на месте, до перехода к соседу.
	//
	// Провайдер отвечает отказом не только когда «сломан»: 429 значит
	// «слишком часто, подожди», 5xx — «сейчас плохо, попробуй ещё». И то и
	// другое проходит со второй попытки, а у донора до этой заплаты не
	// проходило вовсе: одно превышение частоты роняло задание целиком, и
	// составитель шёл заказывать заново.
	for attempt := 0; ; attempt++ {
		if body != nil {
			// Тело ставится заново на каждой попытке: читатель
			// одноразовый, и повтор без этого ушёл бы с пустым телом.
			req.Body = io.NopCloser(bytes.NewReader(rawBody))
			req.ContentLength = int64(len(rawBody))
		}

		raw, status, err := c.attempt(httpClient, req)
		if err == nil {
			return raw, status, nil
		}
		var pe *ProviderError
		if attempt >= providerRetries || !errors.As(err, &pe) || !pe.worthRepeating() {
			return raw, status, err
		}

		wait := backoff(attempt, pe.RetryAfter)
		log.Printf("провайдер %s: %v — повтор через %s (попытка %d из %d)",
			c.cfg.Name, err, wait.Round(time.Millisecond), attempt+2, providerRetries+1)
		select {
		case <-ctx.Done():
			// Отмена во время ожидания — это уход того, кто ждал, а не
			// отказ провайдера, и ждать дальше незачем.
			return nil, 0, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// attempt — одно обращение к провайдеру.
func (c *Client) attempt(httpClient *http.Client, req *http.Request) ([]byte, int, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		// Статуса нет — значит не дошли: сеть, таймаут, мёртвый прокси.
		// Соседа стоит спросить в любом случае, а вот повторять — не
		// всякое: см. timedOut.
		return nil, 0, &ProviderError{Provider: c.cfg.Name, Err: err, TimedOut: timedOut(err)}
	}
	defer resp.Body.Close()

	// Ограничение на ответ: провайдер внешний, доверять размеру нельзя.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, &ProviderError{Provider: c.cfg.Name, Status: resp.StatusCode, Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		return raw, resp.StatusCode, &ProviderError{
			Provider:   c.cfg.Name,
			Status:     resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			Err:        fmt.Errorf("%d: %s", resp.StatusCode, providerMessage(raw)),
		}
	}
	return raw, resp.StatusCode, nil
}

// backoff — сколько ждать перед повтором.
//
// Удвоение с полным разбросом (full jitter): 0–1 с, 0–2 с, 0–4 с. Разброс
// здесь не украшение — задание отправляет несколько запросов разом, и без
// него все они, получив 429, повторились бы одновременно и получили бы его
// снова.
//
// Заголовок Retry-After главнее расчёта: провайдер знает, когда его можно
// спрашивать, лучше нас. Берётся большее из двух — меньшее означало бы
// спросить раньше, чем нас попросили.
func backoff(attempt int, retryAfter time.Duration) time.Duration {
	base := time.Second << attempt
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	wait := time.Duration(rand.Int64N(int64(base) + 1))
	if retryAfter > wait {
		wait = retryAfter
	}
	if wait > time.Minute {
		// Потолок: ждать дольше минуты внутри одного задания бессмысленно,
		// у самого запроса ожидание втрое меньше.
		wait = time.Minute
	}
	return wait
}

// parseRetryAfter разбирает заголовок: он бывает числом секунд и датой.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if at, err := http.ParseTime(v); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
	}
	return 0
}

// providerMessage вытаскивает человеческое объяснение из ответа об отказе.
func providerMessage(raw []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err == nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return truncate(string(raw), 200)
}

// Generate — одно обращение к модели.
func (c *Client) Generate(ctx context.Context, p Prompt, model string) (string, Usage, error) {
	if c.cfg.APIKey == "" {
		return "", Usage{}, ErrNotConfigured
	}
	model = c.EffectiveModel(model)
	if c.cfg.Kind == KindAnthropic {
		return c.generateAnthropic(ctx, p, model)
	}
	return c.generateOpenAI(ctx, p, model)
}

// cacheMarkedSystem — системная часть с пометкой «кэшируй до сих пор».
//
// Системная часть задания статична: у написания, вычитки и сверки она не
// меняется от запроса к запросу, а это тысячи токенов каждый раз.
// Префиксный кэш отдаёт помеченный кусок за десятую часть цены (запись —
// 1.25×, чтение — 0.1×, срок жизни пять минут): составитель работает
// сессиями, и уже второй запрос за сессию окупает запись. Слишком короткую
// для кэша часть провайдер молча пропускает — хуже не бывает.
func cacheMarkedSystem(system string) []map[string]any {
	return []map[string]any{{
		"type":          "text",
		"text":          system,
		"cache_control": map[string]string{"type": "ephemeral"},
	}}
}

func (c *Client) generateOpenAI(ctx context.Context, p Prompt, model string) (string, Usage, error) {
	// Системная часть — первым сообщением и без примесей: и явный кэш
	// OpenRouter, и самостоятельный кэш OpenAI работают по префиксу, и
	// перестановка сообщений обнуляет его целиком.
	var system any = p.System
	if supportsCacheControl(c.cfg.BaseURL) {
		system = cacheMarkedSystem(p.System)
	}
	body := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{"role": "system", "content": system},
			{"role": "user", "content": p.User},
		},
		"temperature": p.temperature(),
		"max_tokens":  p.tokenBudget(),
	}

	// Формат ответа просим не у всех: часть шлюзов на незнакомом поле
	// отвечает отказом, а JSON и так задан заданием. Схема, если задание её
	// принесло, главнее простого «отвечай JSON»: она держит формат и на
	// моделях, которые заданию следуют нетвёрдо.
	schemaSent := false
	if len(p.Schema) > 0 && jsonModeSafe(c.cfg.BaseURL) {
		schemaSent = true
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   p.SchemaName,
				"strict": true,
				"schema": p.Schema,
			},
		}
	} else if jsonModeSafe(c.cfg.BaseURL) && p.SchemaName != "" {
		body["response_format"] = map[string]string{"type": "json_object"}
	}

	// Точная цена и подробности кэша у OpenRouter выдаются по просьбе: без
	// неё в ответе остаются одни токены, и учёт считал бы по прайсу то, что
	// провайдер готов назвать сам.
	if reportsCost(c.cfg.BaseURL) {
		body["usage"] = map[string]bool{"include": true}
	}

	raw, status, err := c.do(ctx, http.MethodPost, "/chat/completions", body)
	if err != nil && schemaSent && (status == 400 || status == 404 || status == 422) {
		// Схему поняли не все: часть моделей и шлюзов отвечает на неё
		// отказом целиком, и ронять из-за подсобного механизма целое
		// задание нельзя. Но снимается она только тогда, когда провайдер
		// назвал виноватой именно её.
		//
		// Отказ, не назвавший ничего, схемы НЕ лишает. Схема держит формат:
		// без неё ответ приходит свободным текстом, и разбор его либо
		// падает, либо — что хуже — проходит наполовину, то есть задача
		// выходит правдоподобной и неполной. Снять схему по догадке значит
		// испортить сам ответ, а непонятое не применяется.
		if schemaRefused(err) {
			log.Printf("провайдер %s: модель %s не приняла схему ответа (%v) — повтор без неё, "+
				"формат дальше держится только заданием", c.cfg.Name, model, err)
			delete(body, "response_format")
			raw, _, err = c.do(ctx, http.MethodPost, "/chat/completions", body)
		} else {
			log.Printf("провайдер %s: модель %s отказала (%v), не назвав, что именно не приняла — "+
				"схема ответа остаётся, отказ передан как есть", c.cfg.Name, model, err)
		}
	}
	if err != nil {
		return "", Usage{}, err
	}

	var parsed struct {
		Choices []struct {
			Message      struct{ Content string } `json:"message"`
			FinishReason string                   `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`

			// Cost и подробности кэша — расширение OpenRouter поверх
			// формата OpenAI. Прочие шлюзы этих полей не присылают, и
			// нулевой cost означает «цену не назвали», а не «бесплатно»:
			// отсюда CostExact ниже, а не проверка на ноль.
			Cost                float64 `json:"cost"`
			PromptTokensDetails struct {
				CachedTokens     int `json:"cached_tokens"`
				CacheWriteTokens int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Choices) == 0 {
		return "", Usage{}, &ProviderError{Provider: c.cfg.Name,
			Err: fmt.Errorf("ответ не разобран: %s", truncate(string(raw), 200))}
	}

	usage := Usage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		CachedTokens:     parsed.Usage.PromptTokensDetails.CachedTokens,
		CacheWriteTokens: parsed.Usage.PromptTokensDetails.CacheWriteTokens,
	}
	// Цену считаем названной только там, где её действительно называют.
	// Признак — не величина, а шлюз: бесплатная модель отвечает честным
	// нулём, и принять его за «не сказали» значило бы дописать ей цену по
	// прайсу.
	if reportsCost(c.cfg.BaseURL) {
		usage.CostUSD = parsed.Usage.Cost
		usage.CostExact = true
	}

	if parsed.Choices[0].FinishReason == "length" {
		// Обрыв по потолку даёт синтаксически битый JSON. Сказать об этом
		// прямо полезнее, чем показать «ошибку разбора».
		return parsed.Choices[0].Message.Content, usage, ErrTruncated
	}
	return parsed.Choices[0].Message.Content, usage, nil
}

func (c *Client) generateAnthropic(ctx context.Context, p Prompt, model string) (string, Usage, error) {
	body := map[string]any{
		"model": model,
		// Системная часть — блоком с пометкой кэша: она статична между
		// запросами, а чтение из кэша стоит десятую часть цены. Формат
		// блоков — родной для messages-диалекта.
		"system":      cacheMarkedSystem(p.System),
		"max_tokens":  p.tokenBudget(),
		"temperature": p.temperature(),
		"messages":    []map[string]string{{"role": "user", "content": p.User}},
	}

	raw, _, err := c.do(ctx, http.MethodPost, "/messages", body)
	if err != nil {
		return "", Usage{}, err
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`

			// Кэш префикса: прочитано и записано. Ради этих чисел и
			// проставляется пометка кэша — экономию иначе не увидеть.
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Content) == 0 {
		return "", Usage{}, &ProviderError{Provider: c.cfg.Name,
			Err: fmt.Errorf("ответ не разобран: %s", truncate(string(raw), 200))}
	}

	var text string
	for _, part := range parsed.Content {
		if part.Type == "text" {
			text += part.Text
		}
	}
	usage := Usage{
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
		CachedTokens:     parsed.Usage.CacheReadInputTokens,
		CacheWriteTokens: parsed.Usage.CacheCreationInputTokens,
	}
	// Цены в ответе Anthropic нет — её посчитают по прайсу, и посчитанное
	// будет помечено как оценка.
	if parsed.StopReason == "max_tokens" {
		return text, usage, ErrTruncated
	}
	return text, usage, nil
}

// Models — список моделей провайдера.
func (c *Client) Models(ctx context.Context) ([]ModelInfo, error) {
	if c.cfg.APIKey == "" {
		return nil, ErrNotConfigured
	}
	raw, _, err := c.do(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		// Список есть не у всех шлюзов: возвращаем настроенную модель,
		// чтобы выбор не оказался пустым.
		if c.cfg.Model != "" {
			return []ModelInfo{{ID: c.cfg.Model, Name: c.cfg.Model}}, nil
		}
		return nil, err
	}

	// У Anthropic поле зовётся display_name, у остальных — name.
	var parsed struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("список моделей не разобран")
	}

	out := make([]ModelInfo, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		name := m.Name
		if name == "" {
			name = m.DisplayName
		}
		if name == "" {
			name = m.ID
		}
		out = append(out, ModelInfo{ID: m.ID, Name: name})
	}
	return out, nil
}

// jsonModeSafe: поле response_format понимают не все шлюзы, а незнакомое
// поле часть из них отвергает вместе со всем запросом.
func jsonModeSafe(baseURL string) bool {
	return strings.Contains(baseURL, "openrouter.ai") || strings.Contains(baseURL, "api.openai.com")
}

// reportsCost: цену обращения в ответе называет OpenRouter и только он.
//
// Он же единственный, кто может назвать её честно: внутри маршрутизатора
// один и тот же запрос уходит разным провайдерам по разным ценам, и
// вычислить её снаружи по имени модели нельзя даже приблизительно. Прочим
// шлюзам цена считается по прайсу и помечается как оценка.
func reportsCost(baseURL string) bool {
	return strings.Contains(baseURL, "openrouter.ai")
}

// supportsCacheControl: поле cache_control понимает OpenRouter, а часть
// шлюзов на незнакомом поле отвечает отказом — та же осторожность, что и у
// response_format. api.openai.com кэширует префикс сам, без пометок.
func supportsCacheControl(baseURL string) bool {
	return strings.Contains(baseURL, "openrouter.ai")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
