package app

import (
	"context"
	"fmt"

	"curator/server/internal/dbgate"
)

// Справочник для устройства: то, что врач читает без сети.
//
// # Зачем он в приложении
//
// Задача проверяет знание, а справочник его даёт: врач, не узнавший
// рубрику, идёт читать её критерии — и идёт туда же, когда рубрику надо
// просто вспомнить у постели больного. Сеть в отделении есть не всегда,
// и справочник, живущий только на сервере, в этот момент не существует.
// Поэтому он качается на устройство целиком.
//
// # Почему это тот же источник, а не вторая копия материала
//
// Единицы и положения, по которым пишутся задачи, и есть справочник:
// рубрика с её критериями. Заведи мы под справочник отдельные таблицы —
// получили бы два места для одного текста, и расходились бы они молча,
// причём в ту сторону, где ошибка опаснее всего: задача спрашивала бы по
// одному тексту, а врач читал бы другой.
//
// # Отдаётся выпусками, а не построчными изменениями
//
// У источника есть номер выпуска (sources.reference_version), и он
// меняется всем, что правит единицы или положения. Устройство сверяет
// своё число с нашим на равенство и, если они разошлись, качает источник
// заново страницами. Построчная досылка была бы дешевле по трафику и
// дороже по правде: удалённую строку «досылать» нечем, и копия
// накапливала бы то, чего в источнике уже нет.

// Reference — справочник.
type Reference struct {
	gate *dbgate.Gate
}

func NewReference(gate *dbgate.Gate) *Reference { return &Reference{gate: gate} }

// RefSource — источник в списке доступного офлайн.
type RefSource struct {
	Slug          string
	Title         string
	UnitWord      string
	StatementWord string
	Edition       string
	Units         int
	Statements    int
	Version       int
}

// RefUnit — единица справочника.
type RefUnit struct {
	Label       string
	ParentLabel string
	Title       string
	Path        string
	Depth       int
	Kind        string
	Answerable  bool
	Statements  int
	Ord         int
}

// RefStatement — положение единицы: критерий, пункт, абзац.
type RefStatement struct {
	ID          int64
	UnitLabel   string
	Kind        string
	Designation string
	PlaceRef    string
	Body        string
	Ord         int
}

// Sources — источники, пригодные к чтению офлайн.
//
// Только те, что объявлены действующими: черновик источника это работа
// составителя, а не материал для врача. Числа единиц и положений уезжают
// вместе со списком, чтобы устройство показало размер закачки до её
// начала, а не после.
func (r *Reference) Sources(ctx context.Context) ([]RefSource, error) {
	rows, err := r.gate.Query(ctx,
		`SELECT s.slug, s.title, s.unit_word, s.statement_word, s.edition,
		        s.reference_version,
		        (SELECT COUNT(*) FROM source_units u WHERE u.source_id = s.id),
		        (SELECT COUNT(*) FROM source_unit_statements st WHERE st.source_id = s.id)
		   FROM sources s
		  WHERE s.status = 'active'
		  ORDER BY s.slug`)
	if err != nil {
		return nil, fmt.Errorf("список справочников не прочитан: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: устройство ходит по нему циклом, и
	// установка без единого действующего источника — исправный случай.
	out := []RefSource{}
	for rows.Next() {
		var one RefSource
		if err := rows.Scan(&one.Slug, &one.Title, &one.UnitWord, &one.StatementWord,
			&one.Edition, &one.Version, &one.Units, &one.Statements); err != nil {
			return nil, fmt.Errorf("строка списка справочников не разобрана: %w", err)
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("список справочников не дочитан: %w", err)
	}
	return out, nil
}

// RefPage — страница справочника.
type RefPage struct {
	Units      []RefUnit
	Statements []RefStatement

	// Next — с какой метки продолжать. Пусто — страниц больше нет.
	Next string

	// Version — выпуск источника на момент выдачи страницы. Уезжает с
	// каждой страницей: изменись он посреди обхода — и копия собралась бы
	// из двух разных выпусков, причём выглядела бы целой.
	Version int
}

// Units отдаёт страницу единиц источника.
//
// Порядок по метке, а не по ord: курсор обязан быть устойчивым, а ord у
// двух единиц совпадает сплошь и рядом — на нём страницы зацикливались бы.
// Метка же в источнике уникальна, этого требует схема.
func (r *Reference) Units(ctx context.Context, slug, after string, limit int) (RefPage, error) {
	limit = pageLimit(limit)
	id, version, err := r.source(ctx, slug)
	if err != nil {
		return RefPage{}, err
	}

	rows, err := r.gate.Query(ctx,
		`SELECT u.label, u.parent_label, u.title, u.path, u.depth, u.kind, u.answerable, u.ord,
		        (SELECT COUNT(*) FROM source_unit_statements st
		          WHERE st.source_id = u.source_id AND st.unit_label = u.label)
		   FROM source_units u
		  WHERE u.source_id = $1 AND ($2 = '' OR u.label > $2)
		  ORDER BY u.label
		  LIMIT $3`, id, after, limit+1)
	if err != nil {
		return RefPage{}, fmt.Errorf("единицы справочника не прочитаны: %w", err)
	}
	defer rows.Close()

	page := RefPage{Units: []RefUnit{}, Statements: []RefStatement{}, Version: version}
	for rows.Next() {
		var one RefUnit
		if err := rows.Scan(&one.Label, &one.ParentLabel, &one.Title, &one.Path,
			&one.Depth, &one.Kind, &one.Answerable, &one.Ord, &one.Statements); err != nil {
			return RefPage{}, fmt.Errorf("единица справочника не разобрана: %w", err)
		}
		page.Units = append(page.Units, one)
	}
	if err := rows.Err(); err != nil {
		return RefPage{}, fmt.Errorf("единицы справочника не дочитаны: %w", err)
	}

	// Лишняя единица запрошена нарочно: она отвечает на вопрос «есть ли
	// ещё», не требуя второго запроса со счётом.
	if len(page.Units) > limit {
		page.Units = page.Units[:limit]
		page.Next = page.Units[limit-1].Label
	}
	return page, nil
}

// Statements отдаёт страницу положений источника.
//
// Курсор по номеру строки: у положений одной единицы совпадает и метка, и
// порядок в пределах единицы, а номер уникален и растёт.
func (r *Reference) Statements(ctx context.Context, slug string, after int64, limit int) (RefPage, error) {
	limit = pageLimit(limit)
	id, version, err := r.source(ctx, slug)
	if err != nil {
		return RefPage{}, err
	}

	rows, err := r.gate.Query(ctx,
		`SELECT id, unit_label, kind, designation, place_ref, body_md, ord
		   FROM source_unit_statements
		  WHERE source_id = $1 AND ($2 = 0 OR id > $2)
		  ORDER BY id
		  LIMIT $3`, id, after, limit+1)
	if err != nil {
		return RefPage{}, fmt.Errorf("положения справочника не прочитаны: %w", err)
	}
	defer rows.Close()

	page := RefPage{Units: []RefUnit{}, Statements: []RefStatement{}, Version: version}
	for rows.Next() {
		var one RefStatement
		if err := rows.Scan(&one.ID, &one.UnitLabel, &one.Kind, &one.Designation,
			&one.PlaceRef, &one.Body, &one.Ord); err != nil {
			return RefPage{}, fmt.Errorf("положение справочника не разобрано: %w", err)
		}
		page.Statements = append(page.Statements, one)
	}
	if err := rows.Err(); err != nil {
		return RefPage{}, fmt.Errorf("положения справочника не дочитаны: %w", err)
	}

	if len(page.Statements) > limit {
		page.Statements = page.Statements[:limit]
		page.Next = fmt.Sprintf("%d", page.Statements[limit-1].ID)
	}
	return page, nil
}

// source находит действующий источник по краткому имени.
//
// Недействующий не отдаётся вовсе, и отличить «нет такого» от «есть, но
// черновик» устройству нельзя: о черновиках составителя врачу знать
// незачем.
func (r *Reference) source(ctx context.Context, slug string) (int64, int, error) {
	var id int64
	var version int
	err := r.gate.QueryRow(ctx,
		`SELECT id, reference_version FROM sources WHERE slug = $1 AND status = 'active'`,
		slug).Scan(&id, &version)
	if err != nil {
		return 0, 0, fmt.Errorf("такого справочника нет: %w", err)
	}
	return id, version, nil
}

// pageLimit держит страницу в разумных границах.
//
// Двести — не украшение: справочник качается целиком, и страница в
// тысячи строк означает ответ в мегабайты, который на слабой сети рвётся
// и начинается заново.
func pageLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return 200
	}
	return limit
}
