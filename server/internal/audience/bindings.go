package audience

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Связь набора с группами: чем набор открывается помимо линейки и от кого
// он скрыт.

// Bound — группа на карточке набора.
type Bound struct {
	Slug  string
	Title string
	Mode  string
}

// OfPack отдаёт группы, привязанные к набору.
func (s *Store) OfPack(ctx context.Context, slug string) ([]Bound, error) {
	rows, err := s.gate.Query(ctx, `
		SELECT g.slug, g.title, b.mode
		  FROM pack_audiences b
		  JOIN audiences g ON g.id = b.audience_id
		  JOIN packs p ON p.id = b.pack_id
		 WHERE p.slug = $1
		 ORDER BY b.mode, g.title`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Bound{}
	for rows.Next() {
		var one Bound
		if err := rows.Scan(&one.Slug, &one.Title, &one.Mode); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

// SetPack задаёт связи набора целиком.
//
// Целиком, а не по одной: «кому открыт этот набор» — одно решение, и
// правится оно на одном экране. Правка по одной связи превратила бы его в
// череду мелких шагов, из которых итог не виден тому, кто их делает, — а
// итог здесь это состав корпуса у живых врачей.
//
// Одна группа не бывает набору и открывающей, и скрывающей сразу: это не
// «строже», а бессмыслица, и принимать её значило бы решать за
// составителя, что он имел в виду.
func (s *Store) SetPack(ctx context.Context, slug string, bounds []Bound) error {
	for _, one := range bounds {
		if one.Mode != ModeOpen && one.Mode != ModeHidden {
			return fmt.Errorf("группа %s привязана непонятно: набор бывает "+
				"группе открыт или от неё скрыт", one.Slug)
		}
	}
	seen := map[string]string{}
	for _, one := range bounds {
		if was, ok := seen[one.Slug]; ok && was != one.Mode {
			return fmt.Errorf("группа %s названа и открывающей, и скрывающей", one.Slug)
		}
		seen[one.Slug] = one.Mode
	}

	return s.gate.InTx(ctx, func(tx pgx.Tx) error {
		var packID int64
		err := tx.QueryRow(ctx, `SELECT id FROM packs WHERE slug = $1`, slug).Scan(&packID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("набора %s нет", slug)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM pack_audiences WHERE pack_id = $1`, packID); err != nil {
			return err
		}
		for groupSlug, mode := range seen {
			tag, err := tx.Exec(ctx, `
				INSERT INTO pack_audiences (pack_id, audience_id, mode)
				SELECT $1, g.id, $3 FROM audiences g WHERE g.slug = $2`,
				packID, groupSlug, mode)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				// Отказ на всю правку, а не пропуск строки: опечатка в
				// метке иначе молча сняла бы связь, которая была, —
				// «сохранено» при потерянной половине списка.
				return fmt.Errorf("группы %s нет", groupSlug)
			}
		}
		return nil
	})
}
