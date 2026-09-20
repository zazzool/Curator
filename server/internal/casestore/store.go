package casestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Store — задачи в базе.
type Store struct {
	gate *dbgate.Gate
}

func NewStore(gate *dbgate.Gate) *Store { return &Store{gate: gate} }

// FromDraft заводит задачу из черновика, написанного конвейером.
//
// Черновик при этом остаётся на месте: он — след того, что ответила
// модель, и нужен, когда задача вышла плохой, а понять почему уже не по
// чему. Задача из него — отдельная запись, которую правят руками.
//
// Метка единицы и источник берутся у черновика, а не у того, кто заводит:
// черновик написан по конкретной единице, и подменить её при заведении
// значило бы получить задачу, проверяющую не то, что написано.
func (s *Store) FromDraft(ctx context.Context, draftID int64, origin string) (Case, bool, error) {
	var out Case
	var repeated bool
	err := s.gate.InTx(ctx, func(tx pgx.Tx) error {
		var raw []byte
		err := tx.QueryRow(ctx,
			`SELECT source_id, unit_label, body FROM case_drafts WHERE id = $1`,
			draftID).Scan(&out.SourceID, &out.UnitLabel, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("черновика %d нет", draftID)
		}
		if err != nil {
			return fmt.Errorf("черновик %d не прочитан: %w", draftID, err)
		}
		if err := json.Unmarshal(raw, &out.Body); err != nil {
			// Непонятое не применяется: черновик, записанный прежней
			// выкаткой и не разобравшийся нынешней, — не черновик с
			// пробелом. Завести из него задачу наполовину значит показать
			// составителю задачу, которой никто не писал.
			return fmt.Errorf("черновик %d не разобран: %w", draftID, err)
		}

		id, err := NewID()
		if err != nil {
			return err
		}
		out.ID, out.Status, out.Origin, out.Revision = id, StatusDraft, origin, 1

		заведена, err := s.insert(ctx, tx, &out, draftID)
		if err != nil {
			return err
		}
		if заведена {
			return nil
		}

		// Этот черновик уже принят. Отдаём прежнюю задачу, а не вторую и
		// не отказ: составитель нажал «Принять» дважды — при обрыве связи
		// или просто дважды, — а принял черновик один раз. Завести вторую
		// задачу с тем же условием значит выдать обучающемуся одну задачу
		// дважды, и заметит это он, а не составитель.
		prior, err := scanCase(tx.QueryRow(ctx, selectCase+` WHERE draft_id = $1`, draftID))
		if err != nil {
			return fmt.Errorf("задача по черновику %d не найдена: %w", draftID, err)
		}
		out, repeated = prior, true
		return nil
	})
	if err != nil {
		return Case{}, false, err
	}
	return out, repeated, nil
}

// insert кладёт новую задачу вместе с первой строкой истории.
//
// Путь единицы читается здесь же, а не принимается доводом: он обязан
// сойтись с тем, что лежит в источнике СЕЙЧАС, и единственный способ это
// обеспечить — прочитать его в той же транзакции.
// Отдаёт false, когда задача по этому черновику уже заведена: проверкой и
// вставкой это не делается — между ними успевает вклиниться второй
// соперник, и проверка «сперва прочитали — не было» пропустит ровно тот
// случай, ради которого заведена. Держит это частичный указатель базы.
//
// draftID нулевой у задач, написанных руками и пришедших ввозом: у них
// черновика нет, и указатель их не считает — на то он и частичный.
func (s *Store) insert(ctx context.Context, tx pgx.Tx, c *Case, draftID int64) (bool, error) {
	err := tx.QueryRow(ctx,
		`SELECT path FROM source_units WHERE source_id = $1 AND label = $2`,
		c.SourceID, c.UnitLabel).Scan(&c.UnitPath)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("единицы «%s» в источнике нет: задача повиснет вне подбора", c.UnitLabel)
	}
	if err != nil {
		return false, fmt.Errorf("путь единицы «%s» не прочитан: %w", c.UnitLabel, err)
	}

	body, err := json.Marshal(c.Body)
	if err != nil {
		return false, fmt.Errorf("содержание задачи не записано: %w", err)
	}
	var draft *int64
	if draftID != 0 {
		draft = &draftID
	}
	err = tx.QueryRow(ctx,
		`INSERT INTO cases (id, source_id, unit_label, unit_path, status, revision,
		                    origin, draft_id, body)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (draft_id) WHERE draft_id IS NOT NULL DO NOTHING
		 RETURNING created_at, updated_at`,
		c.ID, c.SourceID, c.UnitLabel, c.UnitPath, c.Status, c.Revision, c.Origin, draft, body).
		Scan(&c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("задача не заведена: %w", err)
	}
	return true, keepRevision(ctx, tx, c.ID, c.Revision, string(c.Status), body, c.Origin)
}

// Save правит содержание задачи со сверкой редакции.
//
// Сверяется условием самого UPDATE, а не чтением перед записью: между
// чтением и записью успевает вклиниться второй составитель, и проверка
// «сперва прочитали — совпало» пропустит ровно тот случай, ради которого
// заведена.
func (s *Store) Save(ctx context.Context, id string, body Body, revision int, login string) (Case, error) {
	var out Case
	err := s.gate.InTx(ctx, func(tx pgx.Tx) error {
		current, err := scanCase(tx.QueryRow(ctx, selectCase+` WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if current.Status == StatusPublished {
			// Правка раздаваемой задачи молча меняет то, что уже видят на
			// устройствах, и расходится с попытками, записанными по
			// прежнему тексту. Снять с раздачи — решение составителя, и
			// оно принимается отдельно.
			return errors.New("задача раздаётся: снимите её с раздачи, потом правьте")
		}
		if revision != current.Revision {
			return fmt.Errorf(
				"задачу правили, пока вы её открывали: у вас редакция %d, в студии уже %d — перечитайте и внесите правку заново",
				revision, current.Revision)
		}

		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("содержание задачи не записано: %w", err)
		}
		out, err = scanCase(tx.QueryRow(ctx,
			`UPDATE cases SET body = $2, revision = revision + 1, updated_at = NOW()
			  WHERE id = $1 AND revision = $3
			 RETURNING `+caseColumns, id, raw, current.Revision))
		if err != nil {
			return fmt.Errorf("задача %q не сохранена: %w", id, err)
		}
		return keepRevision(ctx, tx, out.ID, out.Revision, string(out.Status), raw, login)
	})
	if err != nil {
		return Case{}, err
	}
	return out, nil
}

// Publish выпускает задачу в раздачу.
//
// Всё в одной транзакции: проверка, смена состояния, разметка по номерам
// положений и версия содержания. Разорви их — и появится задача,
// раздаваемая без разметки, либо версия, выросшая без задачи; и то и
// другое устройство примет за правду.
func (s *Store) Publish(ctx context.Context, id, login string) (Case, error) {
	var out Case
	err := s.gate.InTx(ctx, func(tx pgx.Tx) error {
		c, err := scanCase(tx.QueryRow(ctx, selectCase+` WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if c.Status == StatusPublished {
			// Не отказ: составитель нажал дважды, и он хотел раздачи.
			out = c
			return nil
		}

		known, byDesignation, err := statements(ctx, tx, c.SourceID, c.UnitLabel)
		if err != nil {
			return err
		}
		if faults := CheckPublishable(c, known); len(faults) > 0 {
			return faults
		}

		// Разметка раскладывается по номерам положений здесь, при
		// публикации, а не при написании: обозначение осмысленно всегда, а
		// номер — только пока положение живо. Разложить его заранее значит
		// получить ссылку в никуда у задачи, лежавшей в черновиках месяц.
		if _, err := tx.Exec(ctx, `DELETE FROM case_chunks WHERE case_id = $1`, id); err != nil {
			return fmt.Errorf("прежняя разметка задачи %q не убрана: %w", id, err)
		}
		// Порядковый номер считается отдельно от номера фрагмента:
		// фрагмент, подтверждающий два положения, занимает две строки, и
		// база не примет вторую под тем же ord (UNIQUE). Взять номер из
		// range было бы тихой потерей второго положения — строка не легла
		// бы, а ошибку дал бы UNIQUE, и объяснял бы её потом не тот, кто
		// писал.
		ord := 0
		for _, seg := range c.Body.Segments {
			if len(seg.Statements) == 0 {
				if err := insertChunk(ctx, tx, id, ord, seg.Text, nil); err != nil {
					return err
				}
				ord++
				continue
			}
			for _, designation := range seg.Statements {
				statementID := byDesignation[designation]
				if err := insertChunk(ctx, tx, id, ord, seg.Text, &statementID); err != nil {
					return err
				}
				ord++
			}
		}

		out, err = scanCase(tx.QueryRow(ctx,
			`UPDATE cases
			    SET status = 'published', published_at = NOW(),
			        revision = revision + 1, updated_at = NOW()
			  WHERE id = $1
			 RETURNING `+caseColumns, id))
		if err != nil {
			return fmt.Errorf("задача %q не выпущена: %w", id, err)
		}

		raw, err := json.Marshal(out.Body)
		if err != nil {
			return fmt.Errorf("содержание задачи не записано: %w", err)
		}
		if err := keepRevision(ctx, tx, out.ID, out.Revision, string(out.Status), raw, login); err != nil {
			return err
		}
		return bumpVersion(ctx, tx)
	})
	if err != nil {
		return Case{}, err
	}
	return out, nil
}

// Withdraw снимает задачу с раздачи.
//
// Строка не удаляется никогда: на устройствах задача уже стоит, попытки по
// ней уже записаны, и задача, исчезнувшая из базы, оставила бы попытки,
// ссылающиеся в никуда, — то есть испортила бы отчёты задним числом.
func (s *Store) Withdraw(ctx context.Context, id, login string) (Case, error) {
	var out Case
	err := s.gate.InTx(ctx, func(tx pgx.Tx) error {
		c, err := scanCase(tx.QueryRow(ctx, selectCase+` WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if c.Status != StatusPublished {
			out = c
			return nil
		}
		out, err = scanCase(tx.QueryRow(ctx,
			`UPDATE cases SET status = 'archived', revision = revision + 1, updated_at = NOW()
			  WHERE id = $1 RETURNING `+caseColumns, id))
		if err != nil {
			return fmt.Errorf("задача %q не снята: %w", id, err)
		}
		raw, err := json.Marshal(out.Body)
		if err != nil {
			return fmt.Errorf("содержание задачи не записано: %w", err)
		}
		if err := keepRevision(ctx, tx, out.ID, out.Revision, string(out.Status), raw, login); err != nil {
			return err
		}
		// Версия растёт и на снятии: устройство, не узнавшее о снятии,
		// продолжит показывать задачу, которую составитель уже счёл
		// негодной.
		return bumpVersion(ctx, tx)
	})
	if err != nil {
		return Case{}, err
	}
	return out, nil
}

// Case читает задачу по номеру.
func (s *Store) Case(ctx context.Context, id string) (Case, error) {
	return scanCase(s.gate.QueryRow(ctx, selectCase+` WHERE id = $1`, id))
}

// Filter — по чему отбирают задачи в студии.
//
// Срез по пути, а не по форме метки: «всё, что под 3» — это данные
// источника, а не догадка о том, как устроен чужой справочник.
type Filter struct {
	SourceID int64
	Path     string
	Status   Status
	Limit    int
}

// CasesShown приводит спрошенный предел к тому, сколько задач ручка
// отдаст на самом деле.
//
// Отдельной функцией по той же причине, что и у клиентов: ручке нужно
// знать тот же предел, чтобы спросить на одну больше и отличить «их
// ровно столько» от «их больше». Приводить его второй раз внутри Cases
// нельзя — предел в самый потолок с прибавленной единицей приводился бы
// обратно к пятидесяти.
func CasesShown(asked int) int {
	const byDefault, most = 50, 200
	if asked <= 0 || asked > most {
		return byDefault
	}
	return asked
}

// Cases отдаёт задачи по отбору.
func (s *Store) Cases(ctx context.Context, f Filter) ([]Case, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = CasesShown(0)
	}
	rows, err := s.gate.Query(ctx, selectCase+`
		 WHERE ($1 = 0 OR source_id = $1)
		   AND ($2 = '' OR status = $2)
		   AND ($3 = '' OR unit_path = $3 OR unit_path LIKE $4 ESCAPE '\')
		 ORDER BY updated_at DESC
		 LIMIT $5`,
		f.SourceID, string(f.Status), f.Path, escapeLike(f.Path)+`/%`, limit)
	if err != nil {
		return nil, fmt.Errorf("задачи не прочитаны: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: студия ходит по нему циклом, и на
	// источнике без задач nil уехал бы наружу как null.
	out := []Case{}
	for rows.Next() {
		one, err := scanCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("задачи не дочитаны: %w", err)
	}
	return out, nil
}

// Version — версия опубликованного содержания.
//
// Приложение сравнивает её со своей и качает только при расхождении.
func (s *Store) Version(ctx context.Context) (int64, error) {
	var version int64
	if err := s.gate.QueryRow(ctx, `SELECT version FROM content_version WHERE id`).Scan(&version); err != nil {
		return 0, fmt.Errorf("версия содержания не прочитана: %w", err)
	}
	return version, nil
}

const caseColumns = `id, source_id, unit_label, unit_path, status, revision, origin,
	body, created_at, updated_at, published_at`

const selectCase = `SELECT ` + caseColumns + ` FROM cases`

type row interface {
	Scan(dest ...any) error
}

func scanCase(r row) (Case, error) {
	var c Case
	var raw []byte
	err := r.Scan(&c.ID, &c.SourceID, &c.UnitLabel, &c.UnitPath, &c.Status, &c.Revision,
		&c.Origin, &raw, &c.CreatedAt, &c.UpdatedAt, &c.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Case{}, errors.New("такой задачи нет")
	}
	if err != nil {
		return Case{}, fmt.Errorf("задача не прочитана: %w", err)
	}
	if err := json.Unmarshal(raw, &c.Body); err != nil {
		return Case{}, fmt.Errorf("содержание задачи %q не разобрано: %w", c.ID, err)
	}
	return c, nil
}

func keepRevision(ctx context.Context, tx pgx.Tx, id string, revision int, status string, body []byte, by string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO case_revisions (case_id, revision, status, body, saved_by)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (case_id, revision) DO NOTHING`,
		id, revision, status, body, by)
	if err != nil {
		return fmt.Errorf("редакция задачи %q не сохранена: %w", id, err)
	}
	return nil
}

func insertChunk(ctx context.Context, tx pgx.Tx, id string, ord int, text string, statementID *int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO case_chunks (case_id, ord, text, statement_id) VALUES ($1, $2, $3, $4)`,
		id, ord, text, statementID)
	if err != nil {
		return fmt.Errorf("разметка задачи %q не записана: %w", id, err)
	}
	return nil
}

// statements читает положения единицы: какие есть и под какими номерами.
func statements(ctx context.Context, tx pgx.Tx, sourceID int64, unitLabel string) (map[string]bool, map[string]int64, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, designation FROM source_unit_statements
		  WHERE source_id = $1 AND unit_label = $2`, sourceID, unitLabel)
	if err != nil {
		return nil, nil, fmt.Errorf("положения единицы «%s» не прочитаны: %w", unitLabel, err)
	}
	defer rows.Close()

	known, byDesignation := map[string]bool{}, map[string]int64{}
	for rows.Next() {
		var id int64
		var designation string
		if err := rows.Scan(&id, &designation); err != nil {
			return nil, nil, fmt.Errorf("положение не прочитано: %w", err)
		}
		known[designation], byDesignation[designation] = true, id
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("положения не дочитаны: %w", err)
	}
	return known, byDesignation, nil
}

// bumpVersion двигает версию содержания.
func bumpVersion(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE content_version SET version = version + 1 WHERE id`)
	if err != nil {
		return fmt.Errorf("версия содержания не сдвинута: %w", err)
	}
	return nil
}

// escapeLike обезвреживает знаки, значимые для LIKE.
//
// В метке приказа подчёркивание и процент вполне возможны, а для LIKE это
// «любой знак» и «любая строка». Без экранирования срез по пути «п_1»
// захватил бы «п-1» и «п.1» — то есть отдал бы чужие задачи, и молча.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
