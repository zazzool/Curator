package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Store — хранилище свода.
type Store struct {
	gate *dbgate.Gate
	now  func() time.Time
}

// NewStore заводит хранилище. Часы берутся полем, а не пакетом: свод
// считает затухание и кворум по времени, и проверить это без
// подставленных часов нельзя.
func NewStore(gate *dbgate.Gate) *Store {
	return &Store{gate: gate, now: time.Now}
}

// All читает свод целиком, включая погашенное и закрытое.
//
// Целиком, а не только действующее: свод показывают составителю, и
// погашенное правило в списке — это ответ на вопрос «а почему система
// перестала этого требовать». Отбор действующих делает Book.
func (s *Store) All(ctx context.Context) ([]Rule, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT id, title, text, why, kind, source, status, pinned, scope,
		        confirmations, seen_jobs, valid_from, valid_to,
		        last_seen_at, created_at, updated_at
		   FROM rulebook
		  ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("свод правил не прочитан: %w", err)
	}
	defer rows.Close()

	out := []Rule{}
	for rows.Next() {
		var r Rule
		var scope []byte
		var lastSeen *time.Time
		if err := rows.Scan(&r.ID, &r.Title, &r.Text, &r.Why, &r.Kind, &r.Source,
			&r.Status, &r.Pinned, &scope, &r.Confirmations, &r.SeenJobs,
			&r.ValidFrom, &r.ValidTo, &lastSeen, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("правило не прочитано: %w", err)
		}
		if len(scope) > 0 {
			// Непонятое не применяется: правило с испорченной областью
			// отбрасывается целиком, а не считается действующим всегда.
			// Область «всегда» у правила про один источник — это правило
			// в каждом запросе по каждому документу.
			if err := json.Unmarshal(scope, &r.Scope); err != nil {
				return nil, fmt.Errorf("область правила %q не разобрана: %w", r.ID, err)
			}
		}
		if lastSeen != nil {
			r.LastSeenAt = *lastSeen
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("свод правил не дочитан: %w", err)
	}
	return out, nil
}

// Book читает свод и делает из него снимок.
//
// Одно чтение на заход — см. довод у самого снимка: два независимых
// чтения разъезжаются на дозревающем правиле, и задача получает
// замечание о правиле, которого в её задании не было.
func (s *Store) Book(ctx context.Context) (Book, error) {
	list, err := s.All(ctx)
	if err != nil {
		return Book{}, err
	}
	return NewBook(list, s.now()), nil
}

// Save записывает правило, выверив его.
//
// Выверка стоит ЗДЕСЬ, а не у вызывающего: правило приезжает и из
// студии, и из накопления по замечаниям, и проверка в одном из двух мест
// пропустила бы второе.
func (s *Store) Save(ctx context.Context, r Rule) (Rule, error) {
	now := s.now()
	if err := r.Normalize(now); err != nil {
		return Rule{}, err
	}
	if r.ID == "" {
		return Rule{}, fmt.Errorf("у правила нет опознавателя")
	}
	scope, err := json.Marshal(r.Scope)
	if err != nil {
		return Rule{}, fmt.Errorf("область правила %q не записана: %w", r.ID, err)
	}
	// Пустой срез Go уезжает в JSON как null, а в колонку BIGINT[] —
	// как NULL, и NOT NULL это отвергает. Ловушка одна, мест два.
	seen := r.SeenJobs
	if seen == nil {
		seen = []int64{}
	}
	var lastSeen *time.Time
	if !r.LastSeenAt.IsZero() {
		lastSeen = &r.LastSeenAt
	}

	_, err = s.gate.Exec(ctx,
		`INSERT INTO rulebook (id, title, text, why, kind, source, status, pinned,
		                    scope, confirmations, seen_jobs, valid_from, valid_to,
		                    last_seen_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		 ON CONFLICT (id) DO UPDATE SET
		     title = EXCLUDED.title, text = EXCLUDED.text, why = EXCLUDED.why,
		     kind = EXCLUDED.kind, source = EXCLUDED.source,
		     status = EXCLUDED.status, pinned = EXCLUDED.pinned,
		     scope = EXCLUDED.scope, confirmations = EXCLUDED.confirmations,
		     seen_jobs = EXCLUDED.seen_jobs, valid_to = EXCLUDED.valid_to,
		     last_seen_at = EXCLUDED.last_seen_at, updated_at = EXCLUDED.updated_at`,
		r.ID, r.Title, r.Text, r.Why, r.Kind, r.Source, r.Status, r.Pinned,
		scope, r.Confirmations, seen, r.ValidFrom, r.ValidTo, lastSeen, r.UpdatedAt)
	if err != nil {
		return Rule{}, fmt.Errorf("правило %q не записано: %w", r.ID, err)
	}
	return r, nil
}

// Seed кладёт встроенные правила, которых ещё нет.
//
// Только недостающие: правленное составителем правило затирать накатом
// нельзя — правка свода это его работа, и потерять её значит потерять
// неделю настройки. Поэтому ON CONFLICT DO NOTHING, а не UPSERT.
//
// valid_from у встроенного правила — время первой укладки, а не время
// наката: иначе каждый запуск объявлял бы, что правило появилось
// сегодня, и ответ на «чего мы требовали в марте» был бы «того же, что
// и всегда».
func (s *Store) Seed(ctx context.Context) error {
	now := s.now()
	return s.gate.InTx(ctx, func(tx pgx.Tx) error {
		for _, r := range Builtins(now) {
			if err := r.Normalize(now); err != nil {
				return fmt.Errorf("встроенное правило %q негодно: %w", r.ID, err)
			}
			scope, err := json.Marshal(r.Scope)
			if err != nil {
				return fmt.Errorf("область встроенного правила %q не записана: %w", r.ID, err)
			}
			_, err = tx.Exec(ctx,
				`INSERT INTO rulebook (id, title, text, why, kind, source, status,
				                    scope, valid_from, created_at, updated_at)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,$9)
				 ON CONFLICT (id) DO NOTHING`,
				r.ID, r.Title, r.Text, r.Why, r.Kind, r.Source, r.Status, scope, now)
			if err != nil {
				return fmt.Errorf("встроенное правило %q не положено: %w", r.ID, err)
			}
		}
		return nil
	})
}
