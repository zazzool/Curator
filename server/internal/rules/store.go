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
		        check_json, confirmations, seen_jobs, valid_from, valid_to,
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
		var scope, check []byte
		var lastSeen *time.Time
		if err := rows.Scan(&r.ID, &r.Title, &r.Text, &r.Why, &r.Kind, &r.Source,
			&r.Status, &r.Pinned, &scope, &check, &r.Confirmations, &r.SeenJobs,
			&r.ValidFrom, &r.ValidTo, &lastSeen, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("правило не прочитано: %w", err)
		}
		if err := readCheck(&r, check); err != nil {
			return nil, err
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
	check, err := writeCheck(r)
	if err != nil {
		return Rule{}, err
	}

	_, err = s.gate.Exec(ctx,
		`INSERT INTO rulebook (id, title, text, why, kind, source, status, pinned,
		                    scope, check_json, confirmations, seen_jobs, valid_from,
		                    valid_to, last_seen_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		 ON CONFLICT (id) DO UPDATE SET
		     title = EXCLUDED.title, text = EXCLUDED.text, why = EXCLUDED.why,
		     kind = EXCLUDED.kind, source = EXCLUDED.source,
		     status = EXCLUDED.status, pinned = EXCLUDED.pinned,
		     scope = EXCLUDED.scope, check_json = EXCLUDED.check_json,
		     confirmations = EXCLUDED.confirmations,
		     seen_jobs = EXCLUDED.seen_jobs, valid_to = EXCLUDED.valid_to,
		     last_seen_at = EXCLUDED.last_seen_at, updated_at = EXCLUDED.updated_at`,
		r.ID, r.Title, r.Text, r.Why, r.Kind, r.Source, r.Status, r.Pinned,
		scope, check, r.Confirmations, seen, r.ValidFrom, r.ValidTo, lastSeen, r.UpdatedAt)
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
			check, err := writeCheck(r)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx,
				`INSERT INTO rulebook (id, title, text, why, kind, source, status,
				                    scope, check_json, valid_from, created_at, updated_at)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10,$10)
				 ON CONFLICT (id) DO NOTHING`,
				r.ID, r.Title, r.Text, r.Why, r.Kind, r.Source, r.Status, scope, check, now)
			if err != nil {
				return fmt.Errorf("встроенное правило %q не положено: %w", r.ID, err)
			}
		}
		return nil
	})
}

// Confirm — задание подтвердило правило: оно нарушило его или показало,
// что оно нужно.
//
// Подтверждение идёт ОДНОЙ транзакцией с блокировкой строки, а не
// чтением и записью подряд. Два исполнителя очереди подтверждают правило
// одновременно, читают одно число, прибавляют по единице и записывают
// одно и то же: кворум набирался бы вдвое дольше, а выглядело бы это
// исправной работой. Повтор при конфликте тут не спасает — соперники
// видят одно состояние и сталкиваются снова.
//
// Одно задание считается один раз, сколько бы замечаний оно ни принесло:
// задача, где признак назван трижды, — всё равно одна задача, и считать
// её за три значило бы выдать кворум в одиночку. Держит это seen_jobs, а
// не совесть вызывающего.
//
// Возвращает правило после подтверждения и признак того, что оно
// зачлось: повторное подтверждение тем же заданием — не ошибка, а
// обычный случай.
func (s *Store) Confirm(ctx context.Context, id string, jobID int64) (Rule, bool, error) {
	var out Rule
	var counted bool
	now := s.now()

	err := s.gate.InTx(ctx, func(tx pgx.Tx) error {
		var r Rule
		var scope, check []byte
		var lastSeen *time.Time
		err := tx.QueryRow(ctx,
			`SELECT id, title, text, why, kind, source, status, pinned, scope, check_json,
			        confirmations, seen_jobs, valid_from, valid_to,
			        last_seen_at, created_at, updated_at
			   FROM rulebook WHERE id = $1 FOR UPDATE`, id).
			Scan(&r.ID, &r.Title, &r.Text, &r.Why, &r.Kind, &r.Source, &r.Status,
				&r.Pinned, &scope, &check, &r.Confirmations, &r.SeenJobs, &r.ValidFrom,
				&r.ValidTo, &lastSeen, &r.CreatedAt, &r.UpdatedAt)
		if err != nil {
			return fmt.Errorf("правило %q не прочитано для подтверждения: %w", id, err)
		}
		if len(scope) > 0 {
			if err := json.Unmarshal(scope, &r.Scope); err != nil {
				return fmt.Errorf("область правила %q не разобрана: %w", id, err)
			}
		}
		if err := readCheck(&r, check); err != nil {
			return err
		}
		if lastSeen != nil {
			r.LastSeenAt = *lastSeen
		}

		if hasInt(r.SeenJobs, jobID) {
			// Это задание правило уже подтверждало. Ни счётчик, ни срок
			// не трогаются: иначе повторный проход по тем же замечаниям
			// — а он бывает при перезапуске задания — выдал бы кворум
			// одной задачей.
			out = r
			return nil
		}

		r.SeenJobs = append(r.SeenJobs, jobID)
		r.Confirmations++
		r.LastSeenAt = now
		// Состояние пересчитывается ТЕМ ЖЕ Normalize, что и при записи:
		// кворум, назначенное человеком и погашенное описаны в одном
		// месте, и второе их описание здесь разошлось бы молча.
		if err := r.Normalize(now); err != nil {
			return fmt.Errorf("правило %q после подтверждения негодно: %w", id, err)
		}

		_, err = tx.Exec(ctx,
			`UPDATE rulebook
			    SET confirmations = $2, seen_jobs = $3, last_seen_at = $4,
			        status = $5, updated_at = $6
			  WHERE id = $1`,
			r.ID, r.Confirmations, r.SeenJobs, r.LastSeenAt, r.Status, r.UpdatedAt)
		if err != nil {
			return fmt.Errorf("подтверждение правила %q не записано: %w", id, err)
		}
		out, counted = r, true
		return nil
	})
	return out, counted, err
}

// Propose заводит выведенное правило, если его ещё нет, и подтверждает
// его этим заданием.
//
// Заводится КАНДИДАТОМ и ждёт кворума — даже когда правило очевидно.
// Заведи его действующим, и первое же случайное замечание стало бы
// требованием к модели на все следующие задачи.
//
// Существующее правило не переписывается: составитель мог поправить его
// текст или погасить, и накат обучения затёр бы его работу.
func (s *Store) Propose(ctx context.Context, r Rule, jobID int64) (Rule, bool, error) {
	now := s.now()
	if err := r.Normalize(now); err != nil {
		return Rule{}, false, err
	}
	scope, err := json.Marshal(r.Scope)
	if err != nil {
		return Rule{}, false, fmt.Errorf("область правила %q не записана: %w", r.ID, err)
	}
	_, err = s.gate.Exec(ctx,
		`INSERT INTO rulebook (id, title, text, why, kind, source, status,
		                       scope, valid_from, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,'candidate',$7,$8,$8,$8)
		 ON CONFLICT (id) DO NOTHING`,
		r.ID, r.Title, r.Text, r.Why, r.Kind, r.Source, scope, now)
	if err != nil {
		return Rule{}, false, fmt.Errorf("правило %q не заведено: %w", r.ID, err)
	}
	return s.Confirm(ctx, r.ID, jobID)
}

// readCheck разбирает записанную проверку.
//
// Непонятое не применяется: правило с испорченной проверкой отбрасывается
// целиком, а не считается непроверяемым. Проверка, молча ставшая пустой,
// хуже отсутствующей — на неё полагаются, а она не срабатывает никогда.
func readCheck(r *Rule, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	var check Check
	if err := json.Unmarshal(raw, &check); err != nil {
		return fmt.Errorf("проверка правила %q не разобрана: %w", r.ID, err)
	}
	r.Check = &check
	return nil
}

// writeCheck готовит проверку к записи.
//
// Отсутствие проверки уезжает в базу как NULL, а не как «{}»: пустой
// объект прочитался бы обратно предикатом без рода, то есть проверкой,
// которая есть и ничего не делает.
func writeCheck(r Rule) ([]byte, error) {
	if r.Check == nil {
		return nil, nil
	}
	raw, err := json.Marshal(r.Check)
	if err != nil {
		return nil, fmt.Errorf("проверка правила %q не записана: %w", r.ID, err)
	}
	return raw, nil
}
