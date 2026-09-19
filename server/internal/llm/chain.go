package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Chain — перебор провайдеров по порядку списка.
//
// Порядок значим: первый в списке — основной, остальные запасные. Перебор
// идёт только по тем отказам, которые соседа не касаются; наш собственный
// кривой запрос сосед отвергнет точно так же, и перебирать его значит
// потратить все ключи на одну и ту же ошибку (см. retryable).
type Chain struct {
	// providers — откуда берётся список. Функцией, а не полем: список
	// живёт в настройках и меняется без перезапуска, а снимок, взятый при
	// сборке, устарел бы к первому же изменению.
	providers func() []ProviderConfig

	origin string
	title  string

	// Record — куда записать состоявшееся обращение. Пусто — никуда.
	//
	// Записывается КАЖДОЕ обращение, а не только неудачное: сбой
	// объясняется соседями по заданию — тем, что перебор дошёл до
	// третьего провайдера, что вычитка ушла на другую модель. Задание —
	// единица разбора, и половина задания не объясняет ничего.
	Record func(Attempt)
}

// Attempt — одно состоявшееся обращение, каким его видит учёт.
type Attempt struct {
	Provider string
	Model    string
	Usage    Usage
	Started  time.Time
	Took     time.Duration
	Err      error

	// Ordinal — какой по счёту провайдер отвечал: первый, второй. По нему
	// видно, что основной лёг, хотя задание в итоге собралось.
	Ordinal int
}

// NewChain собирает перебор.
func NewChain(providers func() []ProviderConfig, origin, title string) *Chain {
	return &Chain{providers: providers, origin: origin, title: title}
}

// active — включённые провайдеры с ключом, по порядку списка.
//
// Провайдер без ключа пропускается молча: он объявлен, но не настроен, и
// отказ по нему рассказал бы про устройство настроек тому, кто спрашивал
// задачу.
func (c *Chain) active() []ProviderConfig {
	var out []ProviderConfig
	for _, cfg := range c.providers() {
		if cfg.Enabled && cfg.APIKey != "" && cfg.BaseURL != "" {
			out = append(out, cfg)
		}
	}
	return out
}

// Generate ведёт обращение через первого ответившего провайдера.
func (c *Chain) Generate(ctx context.Context, p Prompt, model string) (string, Usage, error) {
	active := c.active()
	if len(active) == 0 {
		return "", Usage{}, ErrNotConfigured
	}

	var failures []string
	broke := false
	for i, cfg := range active {
		client := NewClient(cfg, c.origin, c.title)
		started := time.Now()
		text, usage, err := client.Generate(ctx, p, model)

		// Запись до всех выходов из цикла: обращение состоялось независимо
		// от того, что мы решим делать дальше. Ровно та ошибка, которую
		// легко сделать иначе, — снятое в одной ветке и забытое в трёх
		// остальных.
		c.record(Attempt{
			Provider: cfg.Name, Model: client.EffectiveModel(model), Usage: usage,
			Started: started, Took: time.Since(started), Err: err, Ordinal: i + 1,
		})

		if err == nil {
			return text, usage, nil
		}

		// Обрыв по потолку соседа спрашивать незачем.
		//
		// Потолок ответа назначаем мы сами и один на всех, а значит второй
		// провайдер упрётся в него ровно так же. Разница с прочими
		// отказами в том, что этот оплачен: заплачено полностью, получен
		// обрывок, — и у донора счёт выставлялся столько раз, сколько
		// провайдеров в списке. Лечится обрыв потолком, а не сменой шлюза.
		if errors.Is(err, ErrTruncated) {
			return text, usage, err
		}

		var pe *ProviderError
		if errors.As(err, &pe) {
			if pe.Status == http.StatusPaymentRequired {
				broke = true
			}
			if !pe.retryable() {
				// Наша ошибка, а не провайдера: перебирать бессмысленно.
				return "", Usage{}, err
			}
		}
		log.Printf("обращение к модели: %s не ответил (%v), пробую следующего", cfg.Name, err)
		failures = append(failures, fmt.Sprintf("%s: %v", cfg.Name, err))

		// Отмена — это уход того, кто ждал, а не отказ провайдера.
		if ctx.Err() != nil {
			return "", Usage{}, ctx.Err()
		}
	}

	if broke {
		// Склейка отказов сюда не подклеивается намеренно: в ней лежит
		// ответ провайдера дословно — «402: This request would exceed your
		// available credits» — и уехал бы он человеку, который английского
		// кода отказа не заказывал. Разбирается это по журналу обращений, а
		// не по строке отказа.
		return "", Usage{}, ErrOutOfCredits
	}
	return "", Usage{}, fmt.Errorf("ни один провайдер не ответил — %s", strings.Join(failures, "; "))
}

func (c *Chain) record(a Attempt) {
	if c.Record != nil {
		c.Record(a)
	}
}
