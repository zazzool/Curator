package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Store — хранилище источников.
//
// Всё, что ниже, ходит к базе только через dbgate: своего пула здесь нет и
// быть не может. Разбор при этом остаётся в чистом виде выше (path.go,
// text.go, docx.go) — хранилище его зовёт, а не повторяет. Вторая
// реализация одного правила расходится с первой молча, и ловить это потом
// приходится сверкой.
type Store struct {
	gate *dbgate.Gate
}

// NewStore собирает хранилище на готовой двери.
func NewStore(gate *dbgate.Gate) *Store { return &Store{gate: gate} }

// Document — загруженный документ: то, что принесли на разбор.
type Document struct {
	ID       int64
	SourceID int64
	Filename string
	MIME     string
	SHA256   string
	Body     []byte

	// UploadedBy — кто принёс. Записывается, потому что в споре о том,
	// откуда в источнике взялся пункт, отвечать будет он, а документ —
	// начало этой цепочки.
	UploadedBy string
	ByteSize   int64
}

// CreateSource заводит источник и возвращает его номер.
func (s *Store) CreateSource(ctx context.Context, src Source) (int64, error) {
	if err := validateSource(src); err != nil {
		return 0, err
	}
	var id int64
	err := s.gate.QueryRow(ctx,
		`INSERT INTO sources
		     (slug, kind, title, unit_word, statement_word, purpose, hierarchy, completeness, edition)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id`,
		src.Slug, string(src.Kind), src.Title, src.UnitWord, src.StatementWord,
		string(src.Purpose), string(src.Hierarchy), string(src.Completeness), src.Edition).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("источник %q не заведён: %w", src.Slug, err)
	}
	return id, nil
}

// validateSource проверяет то, что база проверить не может.
//
// Словарь интерфейса пустым не бывает: интерфейс без слова покажет
// «единица» врачу, который ждёт слова «диагноз» или «пункт». База этого не
// стережёт — ограничение там на пустую строку, а пробел пустой строкой не
// считается.
func validateSource(src Source) error {
	switch {
	case trim(src.Slug) == "":
		return errors.New("у источника нет краткого имени")
	case trim(src.Title) == "":
		return errors.New("у источника нет названия")
	case trim(src.UnitWord) == "" || trim(src.StatementWord) == "":
		return errors.New("у источника не назван словарь интерфейса: как звать единицу и как положение")
	case !knownKind(src.Kind):
		return errors.New("у источника не назван вид: классификация это, приказ, рекомендации, стандарт или руководство")
	case !knownPurpose(src.Purpose):
		// Ось обязана быть названа: по ней подбор решает, складываются ли
		// два источника в один список или стоят поперёк друг друга.
		return errors.New("у источника не названа ось: по чему он делит материал")
	case !knownHierarchy(src.Hierarchy):
		return errors.New("у источника не назван смысл вложенности: часть целого это, разновидность или просто группировка")
	case src.Completeness != CompletenessComplete && src.Completeness != CompletenessFragment:
		// Полнота обязана быть названа, и умолчания здесь нет намеренно:
		// разобранный кусок, объявленный полным, даёт ложные доли охвата —
		// то есть врёт ровно там, где на него смотрят.
		return errors.New("у источника не названа полнота: полный справочник это или разобранный кусок")
	}
	return nil
}

// SaveDocument кладёт документ и возвращает его номер.
//
// Один и тот же файл, принесённый дважды в один источник, — один
// документ: иначе разбор пойдёт по обеим копиям и даст две редакции одного
// источника. Повтор узнаётся по отпечатку и возвращает прежний номер, а не
// отказ: человек, загрузивший файл второй раз, хотел работать с ним, а не
// читать сообщение об ошибке.
//
// Отпечаток сверяется ВНУТРИ источника. Сверка по всей базе стоила
// отказа: тот же файл, положенный во второй источник, возвращал документ
// первого вместе с его источником, и дальше всё шло мимо — куски
// переписывались у чужого документа, а приёмка разбора принимала его в
// источник, которого составитель не открывал. Ответ при этом был
// успешным, и заметить подмену было нечем.
func (s *Store) SaveDocument(ctx context.Context, doc Document) (int64, error) {
	if len(doc.Body) == 0 {
		return 0, errors.New("документ пуст")
	}
	var id int64
	err := s.gate.QueryRow(ctx,
		`INSERT INTO source_documents (source_id, filename, mime, byte_size, sha256, body, uploaded_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (COALESCE(source_id, 0), sha256) DO UPDATE SET filename = source_documents.filename
		 RETURNING id`,
		nullable(doc.SourceID), doc.Filename, doc.MIME, len(doc.Body), doc.SHA256,
		doc.Body, doc.UploadedBy).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("документ %q не сохранён: %w", doc.Filename, err)
	}
	return id, nil
}

// SaveFragments кладёт куски документа, заменяя прежние.
//
// Замена, а не добавление: разбор переделывают, и куски прошлого разбора,
// оставшиеся рядом с новыми, — это два ответа на один вопрос.
func (s *Store) SaveFragments(ctx context.Context, docID int64, frags []Fragment) error {
	return s.gate.InTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM source_fragments WHERE document_id = $1`, docID); err != nil {
			return fmt.Errorf("прежние куски не убраны: %w", err)
		}
		for i, f := range frags {
			_, err := tx.Exec(ctx,
				`INSERT INTO source_fragments (document_id, ord, body_md, char_from, char_to)
				 VALUES ($1, $2, $3, $4, $5)`,
				docID, i, fragmentBody(f), f.CharFrom, f.CharTo)
			if err != nil {
				return fmt.Errorf("кусок %d не сохранён: %w", i+1, err)
			}
		}
		return nil
	})
}

// fragmentBody собирает кусок обратно в текст вместе с его заголовком.
//
// Заголовок уходит модели вместе с телом намеренно: без него кусок теряет
// то единственное, что говорит, о чём он, — и модель дописывает это сама,
// то есть выдумывает.
func fragmentBody(f Fragment) string {
	if f.Title == "" {
		return f.Body
	}
	head := f.Title
	if f.Level > 0 {
		head = repeat("#", f.Level) + " " + f.Title
	}
	if f.Body == "" {
		return head
	}
	return head + "\n\n" + f.Body
}

// SaveDraft кладёт черновик разбора: что модель вынула из документа, пока
// человек не принял.
//
// Черновик живёт в своих таблицах, а не полем «принято» у настоящих единиц.
// Флаг забывают в условии запроса, отдельную таблицу забыть нельзя: пока
// разбор не принят, ни один запрос, читающий источник, его не увидит.
func (s *Store) SaveDraft(ctx context.Context, docID int64, units []Unit, statements []Statement) error {
	return s.gate.InTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM source_draft_statements WHERE document_id = $1`, docID); err != nil {
			return fmt.Errorf("прежние положения черновика не убраны: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM source_draft_units WHERE document_id = $1`, docID); err != nil {
			return fmt.Errorf("прежние единицы черновика не убраны: %w", err)
		}
		for i, u := range units {
			_, err := tx.Exec(ctx,
				`INSERT INTO source_draft_units (document_id, label, parent_label, title, ord)
				 VALUES ($1, $2, $3, $4, $5)`,
				docID, u.Label, u.ParentLabel, u.Title, i)
			if err != nil {
				return fmt.Errorf("единица черновика %q не сохранена: %w", u.Label, err)
			}
		}
		for i, st := range statements {
			_, err := tx.Exec(ctx,
				`INSERT INTO source_draft_statements
				     (document_id, unit_label, kind, designation, body_md, place_ref, ord)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				docID, st.UnitLabel, st.Kind, st.Designation, st.Body, st.PlaceRef, i)
			if err != nil {
				return fmt.Errorf("положение черновика %d не сохранено: %w", i+1, err)
			}
		}
		return nil
	})
}

// AcceptDraft переводит черновик разбора в источник.
//
// # Почему путь считается здесь, а не в базе
//
// Путь — правило разбора, и написано оно один раз, на Go (BuildPaths). Тот
// же расчёт на SQL был бы второй реализацией одного правила, а такие пары
// расходятся молча. Поэтому единицы поднимаются в память целиком, путь
// считается разбором и кладётся готовым.
//
// Целиком — это не расточительство: справочник в тысячу единиц весит
// сотни килобайт, а приказ и того меньше.
//
// # Почему приёмка — одна транзакция
//
// Источник с половиной принятой ветки хуже непринятого: у части единиц
// путь есть, у части нет, и срез отдаёт то одно, то другое. Либо приняты
// все, либо ни одна.
func (s *Store) AcceptDraft(ctx context.Context, sourceID, docID int64, decidedBy string) (int, error) {
	var accepted int
	err := s.gate.InTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT label, parent_label, title, ord
			   FROM source_draft_units
			  WHERE document_id = $1
			  ORDER BY ord`, docID)
		if err != nil {
			return fmt.Errorf("черновик не прочитан: %w", err)
		}
		var draft []Unit
		for rows.Next() {
			var u Unit
			if err := rows.Scan(&u.Label, &u.ParentLabel, &u.Title, &u.Ord); err != nil {
				rows.Close()
				return fmt.Errorf("строка черновика не разобрана: %w", err)
			}
			u.Answerable = true
			draft = append(draft, u)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("черновик дочитан не до конца: %w", err)
		}
		if len(draft) == 0 {
			return errors.New("в черновике нет ни одной единицы: принимать нечего")
		}

		withPaths, err := BuildPaths(draft)
		if err != nil {
			// Отказ целиком, а не приёмка годных: источник с потерянной
			// веткой выглядит исправным, а задачи по ней не выпадают в
			// подборе.
			return fmt.Errorf("черновик не принят: %w", err)
		}

		for _, u := range withPaths {
			_, err := tx.Exec(ctx,
				`INSERT INTO source_units
				     (source_id, kind, label, parent_label, title, path, depth, answerable, ord)
				 VALUES ($1, 'entry', $2, $3, $4, $5, $6, $7, $8)
				 ON CONFLICT (source_id, kind, label) DO UPDATE
				    SET parent_label = EXCLUDED.parent_label,
				        title        = EXCLUDED.title,
				        path         = EXCLUDED.path,
				        depth        = EXCLUDED.depth,
				        ord          = EXCLUDED.ord`,
				sourceID, u.Label, u.ParentLabel, u.Title, u.Path, u.Depth, u.Answerable, u.Ord)
			if err != nil {
				return fmt.Errorf("единица %q не принята: %w", u.Label, err)
			}
			_, err = tx.Exec(ctx,
				`INSERT INTO source_item_acceptances (document_id, unit_label, decision, decided_by)
				 VALUES ($1, $2, 'accept', $3)`,
				docID, u.Label, decidedBy)
			if err != nil {
				return fmt.Errorf("решение по единице %q не записано: %w", u.Label, err)
			}
		}

		rows, err = tx.Query(ctx,
			`SELECT unit_label, kind, designation, body_md, place_ref, ord
			   FROM source_draft_statements
			  WHERE document_id = $1
			  ORDER BY ord`, docID)
		if err != nil {
			return fmt.Errorf("положения черновика не прочитаны: %w", err)
		}
		var statements []Statement
		for rows.Next() {
			var st Statement
			if err := rows.Scan(&st.UnitLabel, &st.Kind, &st.Designation, &st.Body, &st.PlaceRef, &st.Ord); err != nil {
				rows.Close()
				return fmt.Errorf("строка положения не разобрана: %w", err)
			}
			statements = append(statements, st)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("положения дочитаны не до конца: %w", err)
		}

		// Прежние положения принятых единиц убираются перед вставкой.
		//
		// Приёмку повторяют: разбор переделали, нашли пропущенный пункт,
		// нажали «принять» второй раз. Без этой уборки положения легли бы
		// вторым слоем поверх первого, и врач увидел бы каждое дважды —
		// причём у единиц ничего подобного не случилось бы, там UPSERT.
		// Убираются только у тех меток, что есть в черновике: положение
		// единицы, которой этот документ не касается, чужое.
		cleaned := map[string]bool{}
		for _, st := range statements {
			if cleaned[st.UnitLabel] {
				continue
			}

			// Спрашиваем заранее, не ссылается ли на эти положения
			// разметка уже выпущенных задач.
			//
			// База такую уборку и так не даст — на case_chunks стоит
			// внешний ключ, — но её отказ приезжает составителю кодом
			// нарушения и именем ограничения. Человек читает его как
			// поломку студии, хотя случилось ровно то, что должно:
			// положение, на которое показывает выпущенная задача, не
			// заменяется молча, иначе разметка у врача повисла бы в
			// никуда. Спросив сами, мы называем и единицу, и число задач.
			var linked int
			err := tx.QueryRow(ctx,
				`SELECT COUNT(DISTINCT ch.case_id)
				   FROM case_chunks ch
				   JOIN source_unit_statements s ON s.id = ch.statement_id
				  WHERE s.source_id = $1 AND s.unit_label = $2`,
				sourceID, st.UnitLabel).Scan(&linked)
			if err != nil {
				return fmt.Errorf("не проверено, связаны ли положения единицы %q с задачами: %w", st.UnitLabel, err)
			}
			if linked > 0 {
				return fmt.Errorf(
					"на положения единицы %q ссылается разметка уже выпущенных задач (%d) — "+
						"замена оборвала бы её, и врач увидел бы задачу без обоснования. "+
						"Заведите новую редакцию источника: старые задачи останутся при своих положениях",
					st.UnitLabel, linked)
			}

			_, err = tx.Exec(ctx,
				`DELETE FROM source_unit_statements WHERE source_id = $1 AND unit_label = $2`,
				sourceID, st.UnitLabel)
			if err != nil {
				return fmt.Errorf("прежние положения единицы %q не убраны: %w", st.UnitLabel, err)
			}
			cleaned[st.UnitLabel] = true
		}

		for _, st := range statements {
			_, err := tx.Exec(ctx,
				`INSERT INTO source_unit_statements
				     (source_id, unit_label, kind, designation, body_md, place_ref, ord)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				sourceID, st.UnitLabel, st.Kind, st.Designation, st.Body, st.PlaceRef, st.Ord)
			if err != nil {
				return fmt.Errorf("положение единицы %q не принято: %w", st.UnitLabel, err)
			}
		}

		accepted = len(withPaths)
		return nil
	})
	return accepted, err
}

// Units — все единицы источника по порядку.
func (s *Store) Units(ctx context.Context, sourceID int64) ([]Unit, error) {
	return s.units(ctx,
		`SELECT label, parent_label, title, path, depth, answerable, ord
		   FROM source_units
		  WHERE source_id = $1
		  ORDER BY ord, label`, sourceID)
}

// SliceUnits — единицы под указанным путём, вместе с самой единицей этого
// пути.
//
// Это вторая реализация того же правила, что Slice в path.go, и она здесь
// не по недосмотру: срез по тысяче единиц нужен запросом, а не подъёмом
// всего источника в память. Пары «Go и SQL» расходятся молча, поэтому их
// сверяет проверка на живой базе, поле за полем.
//
// Сверяется не просто начало строки: путь «F3» иначе захватил бы «F30».
// Метка экранируется, потому что в пути встречается точка — для LIKE это
// обычный знак, но подчёркивание и процент в метке приказа вполне возможны,
// а они для LIKE значимы.
func (s *Store) SliceUnits(ctx context.Context, sourceID int64, path string) ([]Unit, error) {
	if path == "" {
		return s.Units(ctx, sourceID)
	}
	return s.units(ctx,
		`SELECT label, parent_label, title, path, depth, answerable, ord
		   FROM source_units
		  WHERE source_id = $1
		    AND (path = $2 OR path LIKE $3 ESCAPE '\')
		  ORDER BY ord, label`,
		sourceID, path, escapeLike(path)+PathSeparator+"%")
}

func (s *Store) units(ctx context.Context, sql string, args ...any) ([]Unit, error) {
	rows, err := s.gate.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("единицы не прочитаны: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: он уедет в ответ ручки, и null вместо
	// списка роняет студию именно на исправных случаях — у источника без
	// единиц под этим путём.
	out := []Unit{}
	for rows.Next() {
		var u Unit
		if err := rows.Scan(&u.Label, &u.ParentLabel, &u.Title, &u.Path, &u.Depth, &u.Answerable, &u.Ord); err != nil {
			return nil, fmt.Errorf("строка единицы не разобрана: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("единицы дочитаны не до конца: %w", err)
	}
	return out, nil
}

// nullable отдаёт NULL вместо нуля: ноль — это не номер источника, а его
// отсутствие, и внешний ключ на ноль отказал бы.
func nullable(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// knownKind, knownPurpose, knownHierarchy — сторожа закрытых словарей.
//
// Проверка стоит здесь, а не только ограничением в базе, ради текста
// отказа: база скажет «нарушено ограничение sources_kind_check», и человек
// пойдёт читать схему, а не исправлять поле.
func knownKind(k Kind) bool {
	switch k {
	case KindClassification, KindDecree, KindGuidelines, KindStandard, KindHandbook, KindOther:
		return true
	}
	return false
}

func knownPurpose(p Purpose) bool {
	switch p {
	case PurposeTopic, PurposeSystem, PurposeDiscipline, PurposeTask,
		PurposeLevel, PurposeLegal, PurposeOther:
		return true
	}
	return false
}

func knownHierarchy(h Hierarchy) bool {
	switch h {
	case HierarchyIsA, HierarchyPartOf, HierarchyGrouped:
		return true
	}
	return false
}

// Sources — все источники, новые сверху.
func (s *Store) Sources(ctx context.Context) ([]Source, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT id, slug, kind, title, unit_word, statement_word,
		        purpose, hierarchy, completeness, edition, status
		   FROM sources
		  ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("источники не прочитаны: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: он уедет в ответ ручки, и null вместо
	// списка роняет студию на исправном случае — на пустой установке.
	out := []Source{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("источники дочитаны не до конца: %w", err)
	}
	return out, nil
}

// SourceByID — паспорт одного источника.
func (s *Store) SourceByID(ctx context.Context, id int64) (Source, error) {
	row := s.gate.QueryRow(ctx,
		`SELECT id, slug, kind, title, unit_word, statement_word,
		        purpose, hierarchy, completeness, edition, status
		   FROM sources
		  WHERE id = $1`, id)
	src, err := scanSource(row)
	if err != nil {
		return Source{}, fmt.Errorf("источник %d не найден: %w", id, err)
	}
	return src, nil
}

// scanRow — то общее у строки и у набора строк, что нужно чтению паспорта.
type scanRow interface {
	Scan(dest ...any) error
}

func scanSource(row scanRow) (Source, error) {
	var src Source
	err := row.Scan(&src.ID, &src.Slug, &src.Kind, &src.Title, &src.UnitWord,
		&src.StatementWord, &src.Purpose, &src.Hierarchy, &src.Completeness,
		&src.Edition, &src.Status)
	if err != nil {
		return Source{}, fmt.Errorf("строка источника не разобрана: %w", err)
	}
	return src, nil
}

// DocumentByID — паспорт документа без тела.
//
// Без тела намеренно: тело весит мегабайты, а ручке списка и ручке приёмки
// нужен только номер источника, к которому документ принесли. Тело
// поднимается отдельно и только тем, кому оно нужно.
func (s *Store) DocumentByID(ctx context.Context, id int64) (Document, error) {
	var doc Document
	var sourceID *int64
	err := s.gate.QueryRow(ctx,
		`SELECT id, source_id, filename, mime, byte_size, sha256, uploaded_by
		   FROM source_documents
		  WHERE id = $1`, id).
		Scan(&doc.ID, &sourceID, &doc.Filename, &doc.MIME, &doc.ByteSize,
			&doc.SHA256, &doc.UploadedBy)
	if err != nil {
		return Document{}, fmt.Errorf("документ %d не найден: %w", id, err)
	}
	if sourceID != nil {
		doc.SourceID = *sourceID
	}
	return doc, nil
}

// Documents — документы источника, новые сверху.
func (s *Store) Documents(ctx context.Context, sourceID int64) ([]Document, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT id, filename, mime, byte_size, sha256, uploaded_by
		   FROM source_documents
		  WHERE source_id = $1
		  ORDER BY id DESC`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("документы не прочитаны: %w", err)
	}
	defer rows.Close()

	out := []Document{}
	for rows.Next() {
		doc := Document{SourceID: sourceID}
		if err := rows.Scan(&doc.ID, &doc.Filename, &doc.MIME, &doc.ByteSize,
			&doc.SHA256, &doc.UploadedBy); err != nil {
			return nil, fmt.Errorf("строка документа не разобрана: %w", err)
		}
		out = append(out, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("документы дочитаны не до конца: %w", err)
	}
	return out, nil
}

// Fragments — куски документа по порядку.
//
// Заголовок в теле куска уже есть (его дописал fragmentBody при сохранении),
// и обратно в Level с Title он не разбирается: разбор собранного текста
// назад — вторая реализация того же правила, а такие пары расходятся молча.
func (s *Store) Fragments(ctx context.Context, docID int64) ([]Fragment, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT body_md, char_from, char_to
		   FROM source_fragments
		  WHERE document_id = $1
		  ORDER BY ord`, docID)
	if err != nil {
		return nil, fmt.Errorf("куски не прочитаны: %w", err)
	}
	defer rows.Close()

	out := []Fragment{}
	for rows.Next() {
		var f Fragment
		if err := rows.Scan(&f.Body, &f.CharFrom, &f.CharTo); err != nil {
			return nil, fmt.Errorf("строка куска не разобрана: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("куски дочитаны не до конца: %w", err)
	}
	return out, nil
}

// DraftUnits — единицы черновика разбора, в порядке разбора.
func (s *Store) DraftUnits(ctx context.Context, docID int64) ([]Unit, error) {
	return s.units(ctx,
		`SELECT label, parent_label, title, '' AS path, 0 AS depth,
		        TRUE AS answerable, ord
		   FROM source_draft_units
		  WHERE document_id = $1
		  ORDER BY ord`, docID)
}

// DraftStatements — положения черновика разбора.
func (s *Store) DraftStatements(ctx context.Context, docID int64) ([]Statement, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT unit_label, kind, designation, body_md, place_ref, ord
		   FROM source_draft_statements
		  WHERE document_id = $1
		  ORDER BY ord`, docID)
	if err != nil {
		return nil, fmt.Errorf("положения черновика не прочитаны: %w", err)
	}
	defer rows.Close()

	out := []Statement{}
	for rows.Next() {
		var st Statement
		if err := rows.Scan(&st.UnitLabel, &st.Kind, &st.Designation, &st.Body,
			&st.PlaceRef, &st.Ord); err != nil {
			return nil, fmt.Errorf("строка положения не разобрана: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("положения дочитаны не до конца: %w", err)
	}
	return out, nil
}

// UnitStatements — положения единицы, принятые в источник.
//
// Пустая метка означает «все положения источника»: студия показывает их
// списком, когда человек смотрит источник целиком.
func (s *Store) UnitStatements(ctx context.Context, sourceID int64, unitLabel string) ([]Statement, error) {
	sql := `SELECT unit_label, kind, designation, body_md, place_ref, ord
		      FROM source_unit_statements
		     WHERE source_id = $1 AND ($2 = '' OR unit_label = $2)
		     ORDER BY unit_label, ord`
	rows, err := s.gate.Query(ctx, sql, sourceID, unitLabel)
	if err != nil {
		return nil, fmt.Errorf("положения не прочитаны: %w", err)
	}
	defer rows.Close()

	out := []Statement{}
	for rows.Next() {
		var st Statement
		if err := rows.Scan(&st.UnitLabel, &st.Kind, &st.Designation, &st.Body,
			&st.PlaceRef, &st.Ord); err != nil {
			return nil, fmt.Errorf("строка положения не разобрана: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("положения дочитаны не до конца: %w", err)
	}
	return out, nil
}
