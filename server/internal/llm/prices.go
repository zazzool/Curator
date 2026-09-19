package llm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// Прайс моделей — запасной путь для тех, кто цену не называет.
//
// Цену обращения называет маршрутизатор, и только он может назвать её
// честно: внутри него один и тот же запрос уходит разным поставщикам по
// разным ценам. Все прочие шлюзы отвечают одними токенами, и посчитать
// деньги можно лишь умножением на прайс.
//
// Посчитанное так помечается как оценка и в сводке показывается отдельно.
// Это не педантизм: расчёт не знает ни скидки префиксного кэша, ни наценки
// шлюза, и ошибается в разы там, где кэш работает. Число, которое выглядит
// точным и таковым не является, хуже отсутствия числа.

// Nano — сколько нанодолларов в долларе. Деньги дробным числом не хранятся
// никогда, а цена одного токена — это миллионные доли доллара: в копейках
// она округлилась бы в ноль.
const Nano = 1_000_000_000

// Price — цена одного токена в нанодолларах.
type Price struct {
	Prompt     int64
	Completion int64

	// CacheRead и CacheWrite — цена токена, прочитанного из префиксного
	// кэша и записанного в него. Ноль означает «поставщик не сказал»:
	// тогда кэшированный токен считается по обычной цене входа, то есть
	// расчёт завышает — и это правильная сторона для ошибки в оценке.
	CacheRead  int64
	CacheWrite int64
}

// Prices — прайс по именам моделей в нижнем регистре.
type Prices map[string]Price

// Estimate — во что обошлось обращение, в нанодолларах.
//
// Второй ответ — названа ли цена поставщиком. Названную не пересчитываем
// никогда: она учитывает и скидку кэша, и наценку шлюза, а прайс не знает
// ни того, ни другого.
func (p Prices) Estimate(usage Usage, model string) (int64, bool) {
	if usage.CostExact {
		return int64(math.Round(usage.CostUSD * Nano)), true
	}
	price, ok := p[strings.ToLower(strings.TrimSpace(model))]
	if !ok {
		// Модели нет в прайсе — считать нечем. Ноль здесь означает «не
		// знаем», и помечен он как оценка: приписать обращению нулевую
		// стоимость значило бы показать бесплатной работу, за которую
		// заплачено.
		return 0, false
	}

	// Кэшированные токены входят в общее число входных, и цена у них своя.
	// Вычитаем их из обычных, а не складываем сверху: иначе один и тот же
	// токен посчитается дважды.
	plain := usage.PromptTokens - usage.CachedTokens - usage.CacheWriteTokens
	if plain < 0 {
		plain = 0
	}
	cacheRead := price.CacheRead
	if cacheRead == 0 {
		cacheRead = price.Prompt
	}
	cacheWrite := price.CacheWrite
	if cacheWrite == 0 {
		cacheWrite = price.Prompt
	}

	total := int64(plain)*price.Prompt +
		int64(usage.CachedTokens)*cacheRead +
		int64(usage.CacheWriteTokens)*cacheWrite +
		int64(usage.CompletionTokens)*price.Completion
	return total, false
}

// FetchPrices берёт прайс у того поставщика, который его публикует.
//
// Берётся он у маршрутизатора: прайс там публичный, покрывает модели всех
// перечисленных вендоров и обновляется без нашего участия.
func (c *Client) FetchPrices(ctx context.Context) (Prices, error) {
	if c.cfg.APIKey == "" {
		return nil, ErrNotConfigured
	}
	raw, _, err := c.do(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}

	// Цены в ответе — строки, а не числа, и это не каприз формата: доли
	// цента за токен в double превращаются в 0.0000029999999999999997, и
	// вендоры присылают их текстом, чтобы не спорить о последнем знаке.
	var parsed struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
				CacheRead  string `json:"input_cache_read"`
				CacheWrite string `json:"input_cache_write"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}

	out := make(Prices, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID == "" {
			continue
		}
		price := Price{
			Prompt:     parsePrice(m.Pricing.Prompt),
			Completion: parsePrice(m.Pricing.Completion),
			CacheRead:  parsePrice(m.Pricing.CacheRead),
			CacheWrite: parsePrice(m.Pricing.CacheWrite),
		}
		// Модель без цены входа и выхода в прайсе бесполезна: она даст
		// оценку в ноль, неотличимую от бесплатной.
		if price.Prompt == 0 && price.Completion == 0 {
			continue
		}
		out[strings.ToLower(m.ID)] = price
	}
	return out, nil
}

// parsePrice переводит цену за токен из строки в нанодоллары.
func parsePrice(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" || v == "-1" {
		// «-1» означает «цена не фиксирована»: считать по ней нельзя, и
		// приписывать модели отрицательную цену тем более.
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0
	}
	return int64(math.Round(f * Nano))
}
