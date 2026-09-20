package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/progress"
)

// Величины каталога: как они считаются.
//
// # Считаются по попыткам, а не накапливаются счётчиком
//
// Очевидное решение — держать счётчики и увеличивать их по мере прихода
// попыток. Оно неверно, и неверно молча. Приложение работает офлайн и
// досылает попытки пачкой: попытки за понедельник приезжают в среду, уже
// после вторничных. Счётчик, растущий в порядке ПРИХОДА, посчитает серию
// подряд без ошибок и дни занятий по порядку доставки, а не по порядку, в
// котором врач занимался, — и разойдётся с правдой навсегда, потому что
// пересчитать его будет уже нечем.
//
// Пересчёт по таблице попыток от такого порядка не зависит вовсе: попытки
// в ней лежат со своим временем, и сколько бы их ни доехало позже,
// величина получится та же. Стоит это нескольких сводных запросов на
// пачку, а пачка приходит несколько раз в день на врача.
//
// Сохранённые величины — снимок для чтения и для выдачи знаков, а не
// источник правды. Источник правды — попытки.

// Metrics — величины одного врача.
type Metrics map[progress.MetricKey]int64

// computeMetrics пересчитывает величины по попыткам.
//
// Одним запросом, а не пятью: пять запросов по одной и той же таблице
// внутри транзакции — это пять обходов там, где хватает одного, а платит
// за них врач ожиданием после дня офлайна.
func computeMetrics(ctx context.Context, tx pgx.Tx, accountID int64) (Metrics, error) {
	var solved, streak, days, reviews, sources int64
	err := tx.QueryRow(ctx, `
		WITH мои AS (
		    SELECT a.id, a.case_id, a.correct, a.mode, a.happened_at
		      FROM attempts a
		     WHERE a.account_id = $1
		),
		-- Серии подряд: разность двух нумераций постоянна внутри серии и
		-- меняется на каждом переломе. Это дешевле рекурсии и не зависит
		-- от того, сколько попыток доехало позже.
		полосы AS (
		    SELECT correct,
		           row_number() OVER (ORDER BY happened_at, id)
		         - row_number() OVER (PARTITION BY correct ORDER BY happened_at, id) AS полоса
		      FROM мои
		)
		SELECT
		    (SELECT count(DISTINCT case_id) FROM мои WHERE correct),
		    (SELECT coalesce(max(длина), 0) FROM (
		         SELECT count(*) AS длина FROM полосы WHERE correct GROUP BY полоса
		     ) s),
		    -- День считается по UTC: часового пояса врача мы не знаем, а
		    -- выдумывать его значит ошибаться в ту же сторону, только
		    -- молча. Ошибка не больше суток и не накапливается.
		    (SELECT count(DISTINCT (happened_at AT TIME ZONE 'UTC')::date) FROM мои),
		    (SELECT count(*) FROM мои WHERE mode = $2),
		    (SELECT count(DISTINCT c.source_id)
		       FROM мои m JOIN cases c ON c.id = m.case_id)
	`, accountID, ModeReview).Scan(&solved, &streak, &days, &reviews, &sources)
	if err != nil {
		return nil, err
	}
	return Metrics{
		progress.CasesSolved:    solved,
		progress.CorrectStreak:  streak,
		progress.DaysActive:     days,
		progress.ReviewsDone:    reviews,
		progress.SourcesTouched: sources,
	}, nil
}

// saveMetrics кладёт снимок величин рядом с опытом.
func saveMetrics(ctx context.Context, tx pgx.Tx, accountID int64, m Metrics) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO account_progress (account_id, metrics, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (account_id) DO UPDATE SET metrics = $2, updated_at = NOW()`,
		accountID, raw)
	return err
}

// loadMetrics читает снимок.
//
// Врач, ещё ничего не решавший, величин не имеет вовсе, и это исправный
// случай: возвращается каталог с нулями, а не пустая карта. Пустая карта
// заставила бы каждого читателя помнить, что ключа может не быть, — и
// однажды один из них забудет.
func loadMetrics(ctx context.Context, tx pgx.Tx, accountID int64) (Metrics, error) {
	out := Metrics{}
	for _, m := range progress.Metrics() {
		out[m.Key] = 0
	}

	var raw []byte
	err := tx.QueryRow(ctx,
		`SELECT metrics FROM account_progress WHERE account_id = $1`, accountID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}

	var stored map[string]int64
	if err := json.Unmarshal(raw, &stored); err != nil {
		// Испорченный снимок отбрасывается целиком, а не разбирается по
		// кускам: непонятое не применяется. Пересчёт по попыткам вернёт
		// правду при первой же пачке.
		return out, nil
	}
	for key, value := range stored {
		// Величина, которой нет в каталоге, не переносится: словарь
		// закрыт, и строка, пролезшая в снимок из прежнего выпуска, не
		// должна выглядеть как величина.
		if progress.KnownMetric(progress.MetricKey(key)) {
			out[progress.MetricKey(key)] = value
		}
	}
	return out, nil
}
