// Анализ поведения: решаемость задач и отчёты по событиям.
//
// # Сводка считается из попыток, но хранится отдельно
//
// Отчёт, который на каждый показ перебирает все попытки, перестаёт
// открываться ровно тогда, когда данных становится достаточно, чтобы он был
// интересен. Поэтому решаемость сводится заранее и лежит строкой на задачу.
package analytics

import (
	"context"
	"time"

	"curator/server/internal/dbgate"
)

// Stats — сводка по задаче.
type Stats struct {
	CaseID    string
	Attempts  int64
	Correct   int64
	SolveRate float64
	MedianMs  int64

	// Confusion — на что ловятся: неверный вариант и сколько раз его
	// выбрали. Это и есть материал для правки вариантов: вариант, который
	// не выбирает никто, не различает ничего.
	Confusion map[string]int64

	// Origin — 'live' (наши попытки) или 'imported' (ввезено из прежней
	// базы). Различать обязательно: ввезённая решаемость мерила другую
	// аудиторию.
	Origin string
}

// Rollup — сведение решаемости.
type Rollup struct {
	gate *dbgate.Gate
}

func NewRollup(gate *dbgate.Gate) *Rollup { return &Rollup{gate: gate} }

// Overlap — нахлёст при выборе задач к пересчёту.
//
// # Зачем нахлёст
//
// Попытка получает время начала своей транзакции, а видимой становится
// после её конца. Значит, попытка может лечь «в прошлое» — уже после того,
// как мы записали время сведения. Без нахлёста она не была бы учтена
// никогда: задача пересчитывается только тогда, когда у неё появляется
// попытка новее сведения.
//
// Нахлёст стоит лишней работы и не стоит ничего больше: пересчёт задачи
// идёт целиком из попыток и идемпотентен — посчитай его дважды, выйдет то
// же самое.
const Overlap = 5 * time.Minute

// Run пересчитывает сводку у задач, которых коснулись новые попытки.
//
// Целиком, а не приращением. Приращением считаются только счётчики, а
// медиана и путаница вариантов — нет: медиану из «было столько-то, стало на
// одну больше» не вывести, и попытка подобрать её приращением даёт число,
// похожее на правду и ею не являющееся.
func (r *Rollup) Run(ctx context.Context, now time.Time) (int, error) {
	tag, err := r.gate.Exec(ctx, `
		WITH задетые AS (
		    SELECT DISTINCT a.case_id
		      FROM attempts a
		      LEFT JOIN case_stats s ON s.case_id = a.case_id
		     WHERE s.case_id IS NULL
		        OR a.created_at > s.updated_at - $1::interval
		),
		свои AS (
		    SELECT a.case_id, a.correct, a.answer, a.spent_ms
		      FROM attempts a
		      JOIN задетые z ON z.case_id = a.case_id
		),
		путаница AS (
		    SELECT case_id, jsonb_object_agg(answer, n) AS confusion
		      FROM (SELECT case_id, answer, count(*) AS n
		              FROM свои
		             WHERE NOT correct AND answer <> ''
		             GROUP BY case_id, answer) t
		     GROUP BY case_id
		),
		сводка AS (
		    SELECT s.case_id,
		           count(*)                                   AS attempts,
		           count(*) FILTER (WHERE s.correct)          AS correct,
		           -- Медиана считается только по попыткам с временем:
		           -- ноль здесь значит «сборка времени не прислала», и
		           -- сложить его с настоящими секундами значит объявить,
		           -- что задача решается мгновенно.
		           coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY s.spent_ms)
		                    FILTER (WHERE s.spent_ms > 0), 0) AS median_ms
		      FROM свои s
		     GROUP BY s.case_id
		)
		INSERT INTO case_stats
		       (case_id, attempts, correct, solve_rate, median_ms, confusion, origin, updated_at)
		SELECT c.case_id, c.attempts, c.correct,
		       c.correct::real / c.attempts,
		       round(c.median_ms)::bigint,
		       coalesce(p.confusion, '{}'::jsonb),
		       'live', $2
		  FROM сводка c
		  LEFT JOIN путаница p ON p.case_id = c.case_id
		ON CONFLICT (case_id) DO UPDATE
		   SET attempts   = EXCLUDED.attempts,
		       correct    = EXCLUDED.correct,
		       solve_rate = EXCLUDED.solve_rate,
		       median_ms  = EXCLUDED.median_ms,
		       confusion  = EXCLUDED.confusion,
		       -- Ввезённая сводка уступает место своей на первой же нашей
		       -- попытке, а не смешивается с ней: ввезённое число мерило
		       -- другую аудиторию, и среднее двух аудиторий не описывает
		       -- ни одной. Цена названа вслух — ввезённое число теряется;
		       -- оно и заводилось затем, чтобы система не была слепой,
		       -- пока своих попыток нет.
		       origin     = 'live',
		       updated_at = EXCLUDED.updated_at`,
		Overlap.String(), now)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// Get отдаёт сводку по задаче.
func (r *Rollup) Get(ctx context.Context, caseID string) (Stats, bool, error) {
	rows, err := r.gate.Query(ctx, `
		SELECT case_id, attempts, correct, solve_rate, median_ms, confusion, origin
		  FROM case_stats WHERE case_id = $1`, caseID)
	if err != nil {
		return Stats{}, false, err
	}
	defer rows.Close()
	list, err := scan(rows)
	if err != nil || len(list) == 0 {
		return Stats{}, false, err
	}
	return list[0], true, nil
}
