package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
	"curator/server/internal/progress"
	"curator/server/internal/signs"
)

// Попытки: что врач ответил и что из этого следует.
//
// # Попытки досылаются пачкой и повторно
//
// Приложение работает офлайн: врач разбирает задачи в метро, а посылает их
// вечером, и посылку повторяет при обрыве. Значит, одна и та же попытка
// придёт дважды, и лечь обязана один раз — иначе она испортит и прогресс,
// и решаемость, причём незаметно: числа останутся правдоподобными.
//
// Держит это ключ повторности, который выдаёт САМО УСТРОЙСТВО. Сервер его
// не придумывает: придуманный сервером ключ не пережил бы обрыва ровно в
// тот момент, ради которого заведён.

// Attempts — попытки в базе.
type Attempts struct {
	gate  *dbgate.Gate
	rules progress.Rules
}

func NewAttempts(gate *dbgate.Gate, rules progress.Rules) *Attempts {
	return &Attempts{gate: gate, rules: rules}
}

// Attempt — одна попытка, как её присылает устройство.
type Attempt struct {
	CaseID  string `json:"caseId"`
	Correct bool   `json:"correct"`
	Answer  string `json:"answer"`
	Mode    string `json:"mode"`
	SpentMs int64  `json:"spentMs"`

	// IdemKey — ключ повторности, выданный устройством.
	IdemKey string `json:"idemKey"`

	// HappenedAt — когда это было на устройстве, а не когда доехало.
	// Время доставки к обучению отношения не имеет: врач разбирал задачу
	// в метро, а посылка ушла вечером, и «занимался вечером» — неправда.
	HappenedAt time.Time `json:"happenedAt"`
}

// Outcome — что стало после пачки попыток.
type Outcome struct {
	Accepted int
	Repeated int
	XP       int64
	Level    int
	Due      int

	// Metrics — величины каталога после этой пачки. Уезжают тем же
	// ответом: иначе приложение спрашивало бы их вторым обращением сразу
	// после первого, из того же метро, где второго может и не случиться.
	Metrics Metrics

	// Signs — знаки, выданные ЭТОЙ пачкой, и только они. Приложение
	// показывает их врачу как событие: знак, о котором он узнал бы при
	// следующем заходе в раздел наград, не награда, а находка.
	Signs []signs.Issued
}

// ModeReview — разбор по расписанию повторения.
const ModeReview = "review"

// Record принимает пачку попыток.
//
// Вся пачка в одной транзакции: прогресс, посчитанный по половине пачки,
// это неверный уровень, показанный врачу, — и починить его потом нечем,
// потому что вторая половина уже легла.
func (a *Attempts) Record(ctx context.Context, accountID int64, batch []Attempt, now time.Time) (Outcome, error) {
	var out Outcome
	err := a.gate.InTx(ctx, func(tx pgx.Tx) error {
		xp, level, err := loadProgress(ctx, tx, accountID, a.rules)
		if err != nil {
			return err
		}

		for _, one := range batch {
			if one.CaseID == "" {
				// Попытка без задачи не попытка. Отбрасываем её, а не
				// роняем пачку: одна кривая строка не должна стоить врачу
				// вечера разбора.
				continue
			}
			happened := one.HappenedAt
			if happened.IsZero() {
				happened = now
			}

			fresh, err := insertAttempt(ctx, tx, accountID, one, happened)
			if err != nil {
				return err
			}
			if !fresh {
				// Та же попытка уже лежит. Ни опыта, ни сдвига интервала:
				// повтор посылки — это доставка, а не работа.
				out.Repeated++
				continue
			}
			out.Accepted++

			state, seen, err := loadState(ctx, tx, accountID, one.CaseID, a.rules)
			if err != nil {
				return err
			}
			xp += a.rules.Award(one.Correct, seen, one.Mode == ModeReview)

			next := a.rules.Next(state, one.Correct, happened)
			if err := saveState(ctx, tx, accountID, one.CaseID, next); err != nil {
				return err
			}
		}

		level = a.rules.Level(xp)
		if err := saveProgress(ctx, tx, accountID, xp, level); err != nil {
			return err
		}
		out.XP, out.Level = xp, level

		// Величины пересчитываются по попыткам, а не наращиваются: пачка
		// за понедельник приезжает в среду, и счётчик, растущий в порядке
		// прихода, посчитал бы серию и дни занятий по порядку доставки.
		// Пересчёт — один раз на пачку, а не на попытку.
		metrics, err := computeMetrics(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if err := saveMetrics(ctx, tx, accountID, metrics); err != nil {
			return err
		}
		out.Metrics = metrics

		// Знаки выдаются здесь же, в той же транзакции: разорви эти два
		// действия — и обрыв между ними оставит либо знак без величины,
		// которая его объясняет, либо величину без знака, который по ней
		// положен.
		issued, bonus, err := signs.Award(ctx, tx, accountID, metrics)
		if err != nil {
			return err
		}
		if bonus > 0 {
			// Опыт за знак начисляется по ВЫДАННОМУ: знак с тиражом можно
			// заслужить и не получить, и расчёт «по заслугам» показал бы
			// врачу опыт за знак, которого у него нет.
			xp += bonus
			level = a.rules.Level(xp)
			if err := saveProgress(ctx, tx, accountID, xp, level); err != nil {
				return err
			}
			out.XP, out.Level = xp, level
		}
		out.Signs = issued

		out.Due, err = countDue(ctx, tx, accountID, now)
		return err
	})
	if err != nil {
		return Outcome{}, err
	}
	return out, nil
}

// insertAttempt кладёт попытку и отвечает, легла ли она впервые.
//
// Повторность ловится указателем базы (UNIQUE по паре запись+ключ), а не
// чтением перед записью: между чтением и записью успевает лечь вторая
// посылка той же пачки, и проверка «сперва посмотрели — нет такой»
// пропустит ровно тот случай, ради которого заведена.
func insertAttempt(ctx context.Context, tx pgx.Tx, accountID int64, one Attempt, happened time.Time) (bool, error) {
	tag, err := tx.Exec(ctx,
		`INSERT INTO attempts
		     (account_id, case_id, correct, answer, mode, spent_ms, idem_key, happened_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (account_id, idem_key) WHERE idem_key <> '' DO NOTHING`,
		accountID, one.CaseID, one.Correct, trim(one.Answer, 200),
		trim(one.Mode, 32), one.SpentMs, trim(one.IdemKey, 128), happened)
	if err != nil {
		return false, fmt.Errorf("попытка не записана: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// loadState читает состояние повторения и отвечает, разбирали ли задачу.
func loadState(ctx context.Context, tx pgx.Tx, accountID int64, caseID string, rules progress.Rules) (progress.State, bool, error) {
	var out progress.State
	err := tx.QueryRow(ctx,
		`SELECT ease, interval_days, repetitions FROM review_states
		  WHERE account_id = $1 AND case_id = $2`,
		accountID, caseID).Scan(&out.Ease, &out.IntervalDays, &out.Repetitions)
	if errors.Is(err, pgx.ErrNoRows) {
		return rules.Fresh(), false, nil
	}
	if err != nil {
		return progress.State{}, false, fmt.Errorf("состояние повторения не прочитано: %w", err)
	}
	return out, true, nil
}

func saveState(ctx context.Context, tx pgx.Tx, accountID int64, caseID string, s progress.State) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO review_states (account_id, case_id, ease, interval_days, repetitions, due_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 ON CONFLICT (account_id, case_id) DO UPDATE
		    SET ease = EXCLUDED.ease, interval_days = EXCLUDED.interval_days,
		        repetitions = EXCLUDED.repetitions, due_at = EXCLUDED.due_at,
		        updated_at = NOW()`,
		accountID, caseID, s.Ease, s.IntervalDays, s.Repetitions, s.DueAt)
	if err != nil {
		return fmt.Errorf("состояние повторения не сохранено: %w", err)
	}
	return nil
}

func loadProgress(ctx context.Context, tx pgx.Tx, accountID int64, rules progress.Rules) (int64, int, error) {
	var xp int64
	var level int
	err := tx.QueryRow(ctx,
		`SELECT xp, level FROM account_progress WHERE account_id = $1`, accountID).Scan(&xp, &level)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, rules.Level(0), nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("прогресс не прочитан: %w", err)
	}
	return xp, level, nil
}

func saveProgress(ctx context.Context, tx pgx.Tx, accountID int64, xp int64, level int) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO account_progress (account_id, xp, level, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (account_id) DO UPDATE
		    SET xp = EXCLUDED.xp, level = EXCLUDED.level, updated_at = NOW()`,
		accountID, xp, level)
	if err != nil {
		return fmt.Errorf("прогресс не сохранён: %w", err)
	}
	return nil
}

// countDue считает, сколько задач ждут повторения.
func countDue(ctx context.Context, tx pgx.Tx, accountID int64, now time.Time) (int, error) {
	var due int
	err := tx.QueryRow(ctx,
		`SELECT count(*) FROM review_states r
		  WHERE r.account_id = $1 AND r.due_at IS NOT NULL AND r.due_at <= $2
		    AND EXISTS (SELECT 1 FROM cases c WHERE c.id = r.case_id AND c.status = 'published')`,
		accountID, now).Scan(&due)
	if err != nil {
		return 0, fmt.Errorf("счёт задач к повторению не сделан: %w", err)
	}
	return due, nil
}

// Progress — что врач видит о своём продвижении.
type Progress struct {
	XP      int64
	Level   int
	Due     int
	Metrics Metrics
}

// Progress читает прогресс.
//
// Величины берутся снимком, а не пересчитываются: чтение прогресса — самое
// частое обращение приложения, и сводить по таблице попыток на каждый
// показ экрана значит платить за правду, которая не изменилась с прошлой
// пачки. Снимок обновляется там, где величины и могут измениться, — при
// приёме разборов.
func (a *Attempts) Progress(ctx context.Context, accountID int64, now time.Time) (Progress, error) {
	var out Progress
	err := a.gate.InTx(ctx, func(tx pgx.Tx) error {
		xp, _, err := loadProgress(ctx, tx, accountID, a.rules)
		if err != nil {
			return err
		}
		out.XP, out.Level = xp, a.rules.Level(xp)
		if out.Metrics, err = loadMetrics(ctx, tx, accountID); err != nil {
			return err
		}
		out.Due, err = countDue(ctx, tx, accountID, now)
		return err
	})
	if err != nil {
		return Progress{}, err
	}
	return out, nil
}

// DueCase — задача, которую пора повторить.
type DueCase struct {
	ID    string
	Body  json.RawMessage
	DueAt time.Time
}

// Due отдаёт задачи, которым пришёл срок.
//
// Только опубликованные: у снятой с раздачи состояние повторения
// остаётся — оно принадлежит врачу, а не задаче, — но показывать её
// нельзя, составитель уже счёл её негодной.
func (a *Attempts) Due(ctx context.Context, accountID int64, now time.Time, limit int) ([]DueCase, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := a.gate.Query(ctx,
		`SELECT c.id, c.body, r.due_at
		   FROM review_states r
		   JOIN cases c ON c.id = r.case_id AND c.status = 'published'
		  WHERE r.account_id = $1 AND r.due_at IS NOT NULL AND r.due_at <= $2
		  ORDER BY r.due_at, c.id
		  LIMIT $3`, accountID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("задачи к повторению не прочитаны: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: приложение ходит по нему циклом, и
	// «сегодня нечего повторять» — исправный случай, самый частый из всех.
	out := []DueCase{}
	for rows.Next() {
		var one DueCase
		if err := rows.Scan(&one.ID, &one.Body, &one.DueAt); err != nil {
			return nil, fmt.Errorf("задача к повторению не прочитана: %w", err)
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("задачи к повторению не дочитаны: %w", err)
	}
	return out, nil
}

// Signs отдаёт каталог знаков глазами врача.
//
// Каталог целиком, вместе с невыданными: знак, о котором врач не знает, не
// мотивирует никого. Величины берутся снимком — тем же, что показывает
// прогресс.
func (a *Attempts) Signs(ctx context.Context, accountID int64) ([]signs.Held, error) {
	var out []signs.Held
	err := a.gate.InTx(ctx, func(tx pgx.Tx) error {
		metrics, err := loadMetrics(ctx, tx, accountID)
		if err != nil {
			return err
		}
		out, err = signs.List(ctx, tx, accountID, metrics)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
