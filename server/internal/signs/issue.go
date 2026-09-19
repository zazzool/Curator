package signs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
	"curator/server/internal/progress"
)

// Issued — только что выданный знак.
type Issued struct {
	Slug   string
	Title  string
	Serial int
	XP     int64
}

// Ensure заводит выпуски знаков по каталогу.
//
// Зовётся при старте. Каталог остаётся единственным местом, где знак
// объявлен: строки в базе — след выдачи, а не второе объявление, и
// расходиться им нельзя.
//
// Тираж не уменьшается задним числом. Уменьшить его ниже уже выданного
// значит объявить часть знаков несуществующими, а выданный знак не
// отбирают; поэтому такая правка — отказ при старте, а не молчаливое
// «оставим как было».
func Ensure(ctx context.Context, gate *dbgate.Gate) error {
	return gate.InTx(ctx, func(tx pgx.Tx) error {
		for _, s := range Catalog() {
			var size any
			if s.EditionSize > 0 {
				size = s.EditionSize
			}
			var issued int
			var current *int
			err := tx.QueryRow(ctx,
				`SELECT issued_count, edition_size FROM sign_editions WHERE slug = $1`,
				s.Slug).Scan(&issued, &current)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
			if err == nil && s.EditionSize > 0 && s.EditionSize < issued {
				return fmt.Errorf(
					"знак %q: тираж %d меньше уже выданных %d — выданный знак не отбирают",
					s.Slug, s.EditionSize, issued)
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO sign_editions (slug, title, edition_size)
				 VALUES ($1, $2, $3)
				 ON CONFLICT (slug) DO UPDATE
				    SET title = $2, edition_size = $3, updated_at = NOW()`,
				s.Slug, s.Title, size); err != nil {
				return err
			}
		}
		return nil
	})
}

// Award выдаёт всё, что врач заслужил и чего у него ещё нет.
//
// Работает внутри чужой транзакции: выдача идёт там же, где обновляются
// величины, и разорвать эти два действия нельзя. Выдай мы знак отдельной
// транзакцией — и обрыв между ними оставил бы знак без величины, которая
// его объясняет, либо величину без знака, который по ней положен.
//
// Порядок обхода — порядок каталога, и он значим: собирательный знак
// стоит в нём ПОСЛЕ своих частей, поэтому орден выдаётся в тот же заход,
// что и последняя его часть, а не через сутки.
func Award(ctx context.Context, tx pgx.Tx, accountID int64, m map[progress.MetricKey]int64) ([]Issued, int64, error) {
	ever, err := everIssued(ctx, tx, accountID)
	if err != nil {
		return nil, 0, err
	}

	out := []Issued{}
	var gained int64

	for _, s := range Catalog() {
		if s.Kind == KindRotating {
			continue
		}
		if ever[s.Slug] {
			continue
		}

		deserved := s.Earned(m)
		if s.Kind == KindCollective {
			deserved = true
			for _, part := range s.Requires {
				if !ever[part] {
					deserved = false
					break
				}
			}
		}
		if !deserved {
			continue
		}

		serial, ok, err := take(ctx, tx, s)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			// Тираж кончился. Знак заслужен и не выдан — и опыта за него
			// нет: он начисляется по выданному.
			continue
		}
		if err := grant(ctx, tx, accountID, s.Slug, serial); err != nil {
			return nil, 0, err
		}
		ever[s.Slug] = true
		out = append(out, Issued{Slug: s.Slug, Title: s.Title, Serial: serial, XP: s.XP})
		gained += s.XP
	}

	moved, xp, err := rotate(ctx, tx, accountID, m)
	if err != nil {
		return nil, 0, err
	}
	out = append(out, moved...)
	gained += xp

	return out, gained, nil
}

// rotate двигает переходящий знак.
//
// Строго больше, а не «не меньше»: на равенстве знак дёргался бы между
// двумя врачами при каждой пачке, и свидетельство о нём ничего бы не
// значило. Прежний обладатель получает отметку об отзыве, а не удаление
// строки: собирательные знаки смотрят на когда-либо выданное.
func rotate(ctx context.Context, tx pgx.Tx, accountID int64, m map[progress.MetricKey]int64) ([]Issued, int64, error) {
	out := []Issued{}
	for _, s := range Catalog() {
		if s.Kind != KindRotating || !s.Earned(m) {
			continue
		}

		var holderID *int64
		var holderSignID *int64
		var raw []byte
		err := tx.QueryRow(ctx, `
			SELECT a.account_id, a.id, p.metrics
			  FROM account_signs a
			  LEFT JOIN account_progress p ON p.account_id = a.account_id
			 WHERE a.sign_slug = $1 AND a.revoked_at IS NULL
			 ORDER BY a.issued_at DESC, a.id DESC
			 LIMIT 1`, s.Slug).Scan(&holderID, &holderSignID, &raw)
		if err != nil && err != pgx.ErrNoRows {
			return nil, 0, err
		}

		if holderID != nil {
			if *holderID == accountID {
				continue
			}
			if m[s.Metric] <= metricOf(raw, s.Metric) {
				continue
			}
			if _, err := tx.Exec(ctx,
				`UPDATE account_signs SET revoked_at = NOW() WHERE id = $1`,
				*holderSignID); err != nil {
				return nil, 0, err
			}
		}

		serial, ok, err := take(ctx, tx, s)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			continue
		}
		if err := grant(ctx, tx, accountID, s.Slug, serial); err != nil {
			return nil, 0, err
		}
		out = append(out, Issued{Slug: s.Slug, Title: s.Title, Serial: serial, XP: s.XP})
	}

	var gained int64
	for _, one := range out {
		gained += one.XP
	}
	return out, gained, nil
}

// take берёт следующий номер выпуска.
//
// Номер выдаёт база под замком строки, а не счётчик в коде: два
// соперника, посчитавшие «следующий номер» по одному и тому же
// состоянию, выдали бы один номер двоим — и поймал бы это только указатель
// уникальности, уже на записи, отказом посреди пачки.
func take(ctx context.Context, tx pgx.Tx, s Sign) (int, bool, error) {
	var issued int
	var size *int
	err := tx.QueryRow(ctx,
		`SELECT issued_count, edition_size FROM sign_editions WHERE slug = $1 FOR UPDATE`,
		s.Slug).Scan(&issued, &size)
	if err == pgx.ErrNoRows {
		// Выпуска нет — знак не заведён. Это не повод ронять пачку
		// разборов: врач потеряет вечер разбора из-за знака, которого он
		// не просил.
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if size != nil && issued >= *size {
		return 0, false, nil
	}
	if _, err := tx.Exec(ctx,
		`UPDATE sign_editions SET issued_count = issued_count + 1, updated_at = NOW()
		  WHERE slug = $1`, s.Slug); err != nil {
		return 0, false, err
	}
	return issued + 1, true, nil
}

func grant(ctx context.Context, tx pgx.Tx, accountID int64, slug string, serial int) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO account_signs (account_id, sign_slug, serial) VALUES ($1, $2, $3)`,
		accountID, slug, serial)
	return err
}

// everIssued — знаки, когда-либо выданные врачу, включая отозванные.
//
// Именно когда-либо: собирательный знак смотрит на это, и считай он
// владение сейчас, орден срывался бы вслед за переходящим.
func everIssued(ctx context.Context, tx pgx.Tx, accountID int64) (map[string]bool, error) {
	rows, err := tx.Query(ctx,
		`SELECT sign_slug FROM account_signs WHERE account_id = $1`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out[slug] = true
	}
	return out, rows.Err()
}

// metricOf достаёт величину из сохранённого снимка.
//
// Разбирается в Go, а не приведением типа в SQL: испорченный снимок при
// приведении уронил бы запрос целиком, а здесь он просто не даёт числа —
// непонятое не применяется, но и не роняет остального.
func metricOf(raw []byte, key progress.MetricKey) int64 {
	if len(raw) == 0 {
		return 0
	}
	var stored map[string]int64
	if err := json.Unmarshal(raw, &stored); err != nil {
		return 0
	}
	return stored[string(key)]
}
