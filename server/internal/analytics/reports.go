package analytics

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/telemetry"
)

// Floor — сколько попыток нужно, чтобы решаемость вообще о чём-то говорила.
//
// Задача, решённая один раз из одного, имеет решаемость 1.0 и встаёт первой
// в списке «слишком лёгких». Список, который возглавляют задачи, никем
// толком не решавшиеся, читать бесполезно, а объяснять его составителю —
// стыдно. Число выбрано как наименьшее, при котором одна случайная удача не
// двигает долю заметно; перемеряется оно данными, а не рассуждением.
const Floor = 20

// Ranked — отбор задач по решаемости.
type Ranked struct {
	// Easy — слишком лёгкие: решают почти все. Такая задача не различает
	// знающего и незнающего, то есть не работает.
	Easy []Stats
	// Hard — неразрешимые: не решает почти никто. Чаще всего это не
	// сложность, а дефект — испорченное условие или неверная разметка.
	Hard []Stats
}

// Rank отбирает крайние задачи.
//
// Границы задаются доводом, а не зашиты: что считать «слишком лёгким»,
// зависит от того, для кого задачи, и решает это составитель, а не код.
func (r *Rollup) Rank(ctx context.Context, easyAbove, hardBelow float64, limit int) (Ranked, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	easy, err := r.pick(ctx, `solve_rate >= $1`, easyAbove, `solve_rate DESC`, limit)
	if err != nil {
		return Ranked{}, err
	}
	hard, err := r.pick(ctx, `solve_rate <= $1`, hardBelow, `solve_rate ASC`, limit)
	if err != nil {
		return Ranked{}, err
	}
	return Ranked{Easy: easy, Hard: hard}, nil
}

func (r *Rollup) pick(ctx context.Context, where string, bound float64, order string, limit int) ([]Stats, error) {
	// Довод в запросе один — граница. Порядок и сравнение подставляются
	// строкой, и приходят они не снаружи: обе их разновидности написаны
	// выше по месту, в Rank. Довод, который нельзя подставить параметром,
	// обязан быть написан здесь же, а не приехать из запроса.
	rows, err := r.gate.Query(ctx, `
		SELECT case_id, attempts, correct, solve_rate, median_ms, confusion, origin
		  FROM case_stats
		 WHERE attempts >= $2 AND `+where+`
		 ORDER BY `+order+`, case_id
		 LIMIT $3`, bound, Floor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scan(rows)
}

// Funnel — сколько раз случилось каждое событие словаря за срок.
//
// Отдаётся ВЕСЬ словарь, включая события с нулём. Отчёт, показывающий
// только случившееся, не отличает «никто не покупал» от «приложение не шлёт
// это событие», а это разные беды с разным лечением.
type Funnel struct {
	Name     string
	Title    string
	Count    int64
	Accounts int64
}

func (r *Rollup) Funnel(ctx context.Context, since, until time.Time) ([]Funnel, error) {
	rows, err := r.gate.Query(ctx, `
		SELECT name, count(*), count(DISTINCT account_id)
		  FROM telemetry_events
		 WHERE happened_at >= $1 AND happened_at < $2
		 GROUP BY name`, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type pair struct{ count, accounts int64 }
	seen := map[string]pair{}
	for rows.Next() {
		var name string
		var one pair
		if err := rows.Scan(&name, &one.count, &one.accounts); err != nil {
			return nil, err
		}
		seen[name] = one
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Порядок — порядок словаря, а не порядок чисел: в нём читается путь
	// врача, и отсортированный по убыванию отчёт этот путь разрушает.
	out := make([]Funnel, 0, len(telemetry.Catalog()))
	for _, event := range telemetry.Catalog() {
		one := seen[event.Name]
		out = append(out, Funnel{
			Name: event.Name, Title: event.Title,
			Count: one.count, Accounts: one.accounts,
		})
	}
	return out, nil
}

func scan(rows pgx.Rows) ([]Stats, error) {
	// Пустой список — [], а не null: задач без попыток больше, чем с
	// ними, и отсутствие сводки — исправный случай.
	out := []Stats{}
	for rows.Next() {
		var one Stats
		var raw []byte
		if err := rows.Scan(&one.CaseID, &one.Attempts, &one.Correct,
			&one.SolveRate, &one.MedianMs, &raw, &one.Origin); err != nil {
			return nil, err
		}
		one.Confusion = map[string]int64{}
		if len(raw) > 0 {
			// Испорченная путаница отбрасывается целиком, а не подменяется
			// умолчанием по ключам: половина карты хуже её отсутствия —
			// по ней правят варианты.
			if err := json.Unmarshal(raw, &one.Confusion); err != nil {
				one.Confusion = map[string]int64{}
			}
		}
		out = append(out, one)
	}
	return out, rows.Err()
}
