package signs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/progress"
)

// Held — знак, каким его видит врач.
//
// Каталог отдаётся целиком, вместе с невыданными: знак, о котором врач не
// знает, не мотивирует никого. Невыданный несёт долю пути, выданный —
// номер и дату.
type Held struct {
	Slug  string
	Title string
	Kind  Kind

	Issued   bool
	Serial   int
	IssuedAt time.Time
	Revoked  bool

	// Progress — доля пути, от 0 до 1. У выданного единица.
	Progress float64

	// IssuedCount и EditionSize — сколько роздано и сколько всего.
	// EditionSize ноль значит «тираж не ограничен».
	IssuedCount int
	EditionSize int

	// HoldersShare — доля обладателей среди всех врачей.
	//
	// Считает её сервер, и только он: из собственной истории врача она не
	// выводится вовсе, а без неё знак не говорит, каким он пришёл среди
	// всех.
	HoldersShare float64
}

// List отдаёт каталог глазами одного врача.
func List(ctx context.Context, tx pgx.Tx, accountID int64, m map[progress.MetricKey]int64) ([]Held, error) {
	mine, err := held(ctx, tx, accountID)
	if err != nil {
		return nil, err
	}
	editions, err := editionCounts(ctx, tx)
	if err != nil {
		return nil, err
	}

	var accounts int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&accounts); err != nil {
		return nil, err
	}

	// Пустой список — [], а не null: у нового врача не выдано ничего, и
	// это исправный случай, самый частый из всех.
	out := make([]Held, 0, len(Catalog()))
	for _, s := range Catalog() {
		one := Held{Slug: s.Slug, Title: s.Title, Kind: s.Kind}
		if e, ok := editions[s.Slug]; ok {
			one.IssuedCount, one.EditionSize = e.issued, e.size
			if accounts > 0 {
				one.HoldersShare = float64(e.issued) / float64(accounts)
			}
		}
		if got, ok := mine[s.Slug]; ok {
			one.Issued, one.Serial, one.IssuedAt, one.Revoked = true, got.serial, got.at, got.revoked
			one.Progress = 1
		} else if s.Kind == KindCollective {
			one.Progress = collectiveProgress(s, mine)
		} else {
			one.Progress = s.Progress(m)
		}
		out = append(out, one)
	}
	return out, nil
}

// collectiveProgress — доля собранного ордена.
//
// Считается по КОГДА-ЛИБО выданным частям, как и сама выдача: иначе доля
// падала бы вслед за отозванным переходящим знаком, и врач видел бы, как
// собранное разбирается обратно.
func collectiveProgress(s Sign, mine map[string]mark) float64 {
	if len(s.Requires) == 0 {
		return 0
	}
	var have int
	for _, part := range s.Requires {
		if _, ok := mine[part]; ok {
			have++
		}
	}
	return float64(have) / float64(len(s.Requires))
}

type mark struct {
	serial  int
	at      time.Time
	revoked bool
}

func held(ctx context.Context, tx pgx.Tx, accountID int64) (map[string]mark, error) {
	rows, err := tx.Query(ctx,
		`SELECT sign_slug, serial, issued_at, revoked_at IS NOT NULL
		   FROM account_signs WHERE account_id = $1 ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]mark{}
	for rows.Next() {
		var slug string
		var one mark
		if err := rows.Scan(&slug, &one.serial, &one.at, &one.revoked); err != nil {
			return nil, err
		}
		// Последняя выдача перекрывает прежнюю: переходящий знак можно
		// получить дважды, и показывать надо нынешний номер, а не первый.
		out[slug] = one
	}
	return out, rows.Err()
}

type edition struct {
	issued int
	size   int
}

func editionCounts(ctx context.Context, tx pgx.Tx) (map[string]edition, error) {
	rows, err := tx.Query(ctx, `SELECT slug, issued_count, edition_size FROM sign_editions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]edition{}
	for rows.Next() {
		var slug string
		var one edition
		var size *int
		if err := rows.Scan(&slug, &one.issued, &size); err != nil {
			return nil, err
		}
		if size != nil {
			one.size = *size
		}
		out[slug] = one
	}
	return out, rows.Err()
}
