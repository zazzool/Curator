// Учёт обращений к моделям: во что обошлась работа.
//
// Пакет стоит между дверью к моделям и базой: дверь про базу не знает
// намеренно, а числа надо где-то хранить. Записывается КАЖДОЕ обращение, а
// не только неудачное: сбой объясняется соседями по заданию — тем, что
// перебор дошёл до третьего поставщика, что вычитка ушла на другую модель.
// Задание — единица разбора, и половина задания не объясняет ничего.
//
// Сводку можно посчитать из строк, строки из сводки — нет. Поэтому строки
// и хранятся.
package llmusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
	"curator/server/internal/llm"
)

// Store — учёт и прайс.
type Store struct {
	gate *dbgate.Gate
}

func NewStore(gate *dbgate.Gate) *Store { return &Store{gate: gate} }

// TextMax — потолок одного сохраняемого тела в БАЙТАХ.
//
// Тела нужны, чтобы разбирать сбой: «разбор ответа модели не удался» без
// самого ответа не разберёт никто. Но задание с указаниями — это тысяч
// десять знаков, то есть вдвое больше байт на кириллице; двадцать тысяч
// байт вмещают его целиком, а всё, что длиннее, — это хвост, для разбора
// сбоя не нужный.
const TextMax = 20000

// Call — одно обращение, как его записывает учёт.
type Call struct {
	JobID    int64
	Node     string
	Provider string
	Model    string

	Usage   llm.Usage
	Latency time.Duration
	Err     error

	// Request и Answer — что отправили и что пришло дословно. Пусто —
	// значит хранение тел выключено.
	Request string
	Answer  string
}

// Record записывает обращение.
//
// Цена берётся из прайса только тогда, когда поставщик её не назвал:
// названная учитывает и скидку кэша, и наценку шлюза, а прайс не знает ни
// того, ни другого.
func (s *Store) Record(ctx context.Context, call Call, prices llm.Prices) error {
	cost, exact := prices.Estimate(call.Usage, call.Model)

	_, err := s.gate.Exec(ctx,
		`INSERT INTO llm_calls
		     (job_id, node, provider, model, status,
		      prompt_tokens, output_tokens, cached_tokens, cache_write_tokens,
		      cost_nano_usd, cost_exact, latency_ms, request, response, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		nullable(call.JobID), call.Node, call.Provider, call.Model, status(call.Err),
		call.Usage.PromptTokens, call.Usage.CompletionTokens,
		call.Usage.CachedTokens, call.Usage.CacheWriteTokens,
		cost, exact, call.Latency.Milliseconds(),
		body(call.Request), body(call.Answer), errText(call.Err))
	if err != nil {
		return fmt.Errorf("обращение не записано: %w", err)
	}
	return nil
}

// status — род исхода одним словом.
//
// Словарь закрыт и короток намеренно: по нему считают сводку, а свободный
// текст отказа для счёта непригоден — двух одинаковых отказов у моделей не
// бывает.
func status(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, llm.ErrTruncated):
		// Обрыв стоит особняком: заплачено полностью, получено ничто.
		// Сложить его с прочими отказами (те не стоят ничего) значит
		// потерять единственный род отказа, который виден в счёте.
		return "truncated"
	case llm.OutOfCredits(err):
		return "no_credits"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "cancelled"
	default:
		return "failed"
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return trim(err.Error(), 500)
}

// body готовит тело к хранению: обрезает и заворачивает в JSON.
//
// JSONB, а не текстом, потому что рядом уже лежит разобранный ответ, и
// колонка одного вида избавляет от двух путей чтения. Строка — это тоже
// годный JSON, и заворачивать её в объект незачем.
func body(text string) any {
	if text == "" {
		return nil
	}
	raw, err := json.Marshal(trim(text, TextMax))
	if err != nil {
		return nil
	}
	return raw
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Режем по границе рун: обрезанная посередине кириллическая буква
	// делает строку недействительным UTF-8, и такую строку база не примет
	// вовсе — запись упала бы из-за длины тела, а не из-за работы.
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// utf8Start — начинается ли с этого байта руна.
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// Line — строка сводки: во что обошёлся один день одной модели.
type Line struct {
	Day      time.Time
	Provider string
	Model    string
	Calls    int
	Failed   int

	PromptTokens int
	OutputTokens int

	// CostNanoUSD — сумма за день. Exact считает те обращения, чью цену
	// назвал поставщик: сводка, в которой оценка неотличима от факта, —
	// это сводка, которой нельзя пользоваться.
	CostNanoUSD    int64
	ExactNanoUSD   int64
	EstimatedCalls int
}

// Summary — сводка расхода по дням, моделям и поставщикам.
func (s *Store) Summary(ctx context.Context, since, until time.Time) ([]Line, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT date_trunc('day', created_at) AS day, provider, model,
		        COUNT(*), COUNT(*) FILTER (WHERE status <> 'ok'),
		        COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(output_tokens), 0),
		        COALESCE(SUM(cost_nano_usd), 0),
		        COALESCE(SUM(cost_nano_usd) FILTER (WHERE cost_exact), 0),
		        COUNT(*) FILTER (WHERE NOT cost_exact)
		   FROM llm_calls
		  WHERE created_at >= $1 AND created_at < $2
		  GROUP BY day, provider, model
		  ORDER BY day DESC, provider, model`, since, until)
	if err != nil {
		return nil, fmt.Errorf("сводка расхода не прочитана: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: он уедет в ответ ручки, и null вместо
	// списка роняет студию на исправном случае — в день, когда не
	// обращались ни разу.
	out := []Line{}
	for rows.Next() {
		var l Line
		if err := rows.Scan(&l.Day, &l.Provider, &l.Model, &l.Calls, &l.Failed,
			&l.PromptTokens, &l.OutputTokens, &l.CostNanoUSD, &l.ExactNanoUSD,
			&l.EstimatedCalls); err != nil {
			return nil, fmt.Errorf("строка сводки не разобрана: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("сводка дочитана не до конца: %w", err)
	}
	return out, nil
}

// SavePrices кладёт прайс, заменяя прежние цены тех же моделей.
//
// Заменяя, а не добавляя: цена модели одна, и две строки о ней — это два
// ответа на один вопрос, из которых покажут случайный.
func (s *Store) SavePrices(ctx context.Context, provider string, prices llm.Prices) error {
	if provider == "" {
		return errors.New("прайс не приписан поставщику")
	}
	return s.gate.InTx(ctx, func(tx pgx.Tx) error {
		for model, price := range prices {
			_, err := tx.Exec(ctx,
				`INSERT INTO model_prices
				     (provider, model, prompt_nano_usd, completion_nano_usd,
				      cache_read_nano_usd, cache_write_nano_usd, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, NOW())
				 ON CONFLICT (provider, model) DO UPDATE
				    SET prompt_nano_usd      = EXCLUDED.prompt_nano_usd,
				        completion_nano_usd  = EXCLUDED.completion_nano_usd,
				        cache_read_nano_usd  = EXCLUDED.cache_read_nano_usd,
				        cache_write_nano_usd = EXCLUDED.cache_write_nano_usd,
				        updated_at           = NOW()`,
				provider, model, price.Prompt, price.Completion,
				price.CacheRead, price.CacheWrite)
			if err != nil {
				return fmt.Errorf("цена модели %q не сохранена: %w", model, err)
			}
		}
		return nil
	})
}

// Prices — прайс из базы.
func (s *Store) Prices(ctx context.Context) (llm.Prices, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT model, prompt_nano_usd, completion_nano_usd,
		        cache_read_nano_usd, cache_write_nano_usd
		   FROM model_prices`)
	if err != nil {
		return nil, fmt.Errorf("прайс не прочитан: %w", err)
	}
	defer rows.Close()

	out := llm.Prices{}
	for rows.Next() {
		var model string
		var price llm.Price
		if err := rows.Scan(&model, &price.Prompt, &price.Completion,
			&price.CacheRead, &price.CacheWrite); err != nil {
			return nil, fmt.Errorf("строка прайса не разобрана: %w", err)
		}
		out[strings.ToLower(model)] = price
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("прайс дочитан не до конца: %w", err)
	}
	return out, nil
}

// Sweep убирает тела обращений, которым вышел срок.
//
// Тела стареют быстро: журнал нужен, пока сбой разбирают, а не вечно.
// Числа учёта при этом остаются навсегда — сводка расходов за всё время
// должна оставаться правдой, а место занимают именно тела.
func (s *Store) Sweep(ctx context.Context, keep time.Duration, now time.Time) (int64, error) {
	if keep <= 0 {
		return 0, errors.New("срок хранения тел не назван")
	}
	tag, err := s.gate.Exec(ctx,
		`UPDATE llm_calls
		    SET request = NULL, response = NULL
		  WHERE created_at < $1 AND (request IS NOT NULL OR response IS NOT NULL)`,
		now.Add(-keep))
	if err != nil {
		return 0, fmt.Errorf("старые тела не убраны: %w", err)
	}
	return tag.RowsAffected(), nil
}

// nullable отдаёт NULL вместо нуля: ноль — это не номер задания, а его
// отсутствие, и внешний ключ на ноль отказал бы.
func nullable(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// ModelPrice — одна строка прайса моделей.
type ModelPrice struct {
	Provider string
	Model    string

	// PromptNanoUSD и CompletionNanoUSD — цена ОДНОГО токена в
	// нанодолларах. За токен, а не за тысячу: тысяча — привычная единица
	// прайс-листов, но она заставляет делить при каждом расчёте, и
	// однажды разделят не там.
	PromptNanoUSD     int64
	CompletionNanoUSD int64
}

// Catalog — модели, о которых что-то известно, для выбора в студии.
//
// Берётся из прайса, а не спрашивается у поставщика при каждом открытии
// экрана: список у шлюза бывает в сотни строк, ходит он секунды, а
// выбирают модель раз в месяц. Прайс же обновляется своим чередом и несёт
// вдобавок цену — то, по чему модель для узла и выбирают.
//
// Список НЕ закрытый, и это важно для того, кто его читает: новая модель
// появляется у поставщика раньше, чем в нашем прайсе, и отсутствие в
// этом списке ничего не запрещает.
func (s *Store) Catalog(ctx context.Context) ([]ModelPrice, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT provider, model, prompt_nano_usd, completion_nano_usd
		   FROM model_prices
		  ORDER BY provider, model`)
	if err != nil {
		return nil, fmt.Errorf("прайс моделей не прочитан: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: он уедет в ответ ручки, и null вместо
	// списка роняет студию на исправном случае — на свежей установке, где
	// прайса ещё нет.
	out := []ModelPrice{}
	for rows.Next() {
		var one ModelPrice
		if err := rows.Scan(&one.Provider, &one.Model,
			&one.PromptNanoUSD, &one.CompletionNanoUSD); err != nil {
			return nil, fmt.Errorf("строка прайса моделей не разобрана: %w", err)
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("прайс моделей дочитан не до конца: %w", err)
	}
	return out, nil
}

// NodeStat — что известно про один узел конвейера за срок.
type NodeStat struct {
	Node   string
	Calls  int64
	Failed int64

	// MedianNanoUSD — медианная цена ОДНОГО состоявшегося обращения.
	//
	// Медиана, а не среднее: одно обращение разбора документа стоит
	// десятка обращений сверки, и среднее по узлу, куда попал один
	// длинный документ, показало бы дорогим узел, который дорог не был.
	// Решают по этому числу, где менять модель, — и решили бы не там.
	//
	// По состоявшимся: отказ чаще всего не стоит ничего, и посчитанный
	// вместе с работой он тянет медиану к нулю. Узел, отказывающий
	// половину раз, выглядел бы вдвое дешевле исправного.
	MedianNanoUSD int64

	// MedianMs — медианное время одного состоявшегося обращения.
	MedianMs int64

	// Estimated — сколько обращений посчитано по прайсу, а не по цене,
	// названной поставщиком. Оценка не выдаётся за факт: расчёт не знает
	// ни скидки префиксного кэша, ни наценки шлюза и ошибается в разы
	// там, где кэш работает.
	Estimated int64
}

// ByNode — расход и отказы по узлам конвейера за срок.
func (s *Store) ByNode(ctx context.Context, since, until time.Time) ([]NodeStat, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT node,
		        COUNT(*),
		        COUNT(*) FILTER (WHERE status <> 'ok'),
		        COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (
		            ORDER BY cost_nano_usd) FILTER (WHERE status = 'ok'), 0),
		        COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (
		            ORDER BY latency_ms) FILTER (WHERE status = 'ok'), 0),
		        COUNT(*) FILTER (WHERE status = 'ok' AND NOT cost_exact)
		   FROM llm_calls
		  WHERE created_at >= $1 AND created_at < $2
		  GROUP BY node
		  ORDER BY node`, since, until)
	if err != nil {
		return nil, fmt.Errorf("расход по узлам не прочитан: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: он уедет в ответ ручки, и null вместо
	// списка роняет студию на исправном случае — на свежей установке, где
	// не писали ещё ни одной задачи.
	out := []NodeStat{}
	for rows.Next() {
		var one NodeStat
		// Медиана приезжает дробной — PERCENTILE_CONT усредняет два
		// средних значения. Нанодоллар и миллисекунда дробными не
		// бывают, и округление здесь честнее, чем дробь в ответе.
		var cost, ms float64
		if err := rows.Scan(&one.Node, &one.Calls, &one.Failed,
			&cost, &ms, &one.Estimated); err != nil {
			return nil, fmt.Errorf("строка расхода по узлам не разобрана: %w", err)
		}
		one.MedianNanoUSD = int64(math.Round(cost))
		one.MedianMs = int64(math.Round(ms))
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("расход по узлам дочитан не до конца: %w", err)
	}
	return out, nil
}
