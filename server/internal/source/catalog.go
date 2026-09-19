package source

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Разбор выгрузки каталога: единицы и положения одним файлом.
//
// # Зачем отдельный разбор рядом с разбором текста
//
// Остальные форматы (text.go, docx.go) — это текст, из которого разбор
// достаёт структуру догадкой по заголовкам. Здесь структура уже названа:
// выгрузка прежней системы несёт разделы, записи, критерии и
// дифференциальный диагноз отдельными списками со связями между ними.
// Пытаться достать это из текста значило бы выбросить связи и угадывать их
// заново — и угадывать по виду метки, то есть ровно тем способом, который
// здесь запрещён.
//
// # Это разбор ОДНОГО формата, а не ветка «если это МКБ»
//
// Имена полей ниже — имена конкретной выгрузки, как «слово docx» в
// docx.go. Источник от этого особым не становится: на выходе получаются
// обычные Unit и Statement, и всё, что ниже по пути, не знает, каким
// разбором они получены. Свойства источника (слова интерфейса, вид,
// полнота) в файле не лежат и берутся у паспорта, а не из формата меток.
//
// # Непонятое не применяется
//
// Чужая версия схемы — отказ целиком, а не разбор по тем полям, которые
// узнались: наполовину понятый каталог кладёт наполовину верный
// справочник, и заметить это можно только открыв нужную рубрику.

// CatalogSchema — версия формата выгрузки, которую разбор понимает.
const CatalogSchema = 1

// catalogFile — выгрузка, как она лежит в файле.
type catalogFile struct {
	SchemaVersion int    `json:"schemaVersion"`
	ExportedAt    string `json:"exportedAt"`
	Source        string `json:"source"`

	Groups []struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		SortOrder int    `json:"sort_order"`
	} `json:"groups"`

	Diagnoses []struct {
		Code      string `json:"code"`
		Title     string `json:"title"`
		Detail    string `json:"detail"`
		GroupCode string `json:"group_code"`
	} `json:"diagnoses"`

	Criteria []struct {
		Code     string `json:"code"`
		Type     string `json:"criteria_type"`
		Content  string `json:"content_md"`
		Citation string `json:"source_citation"`
	} `json:"criteria"`

	Differential []struct {
		Code     string `json:"code"`
		With     string `json:"differential_with"`
		Features string `json:"distinguishing_features_md"`
	} `json:"differential"`
}

// CatalogReport — чем кончился разбор.
//
// Отброшенное считается и называется. Молча выброшенная сотня критериев
// выглядит как «столько в выгрузке и было», и объяснить это потом нечем:
// в базе лежит ровно то, что легло.
type CatalogReport struct {
	Groups    int
	Entries   int
	Criteria  int
	Different int

	// Dropped — что не легло и почему. Ключ — метка, значение — причина.
	Dropped map[string]string
}

// Differential — пара «путают с» и текст различий.
//
// Отдельно от положения, хотя текст у них общего происхождения, и это не
// задвоение: положение — то, что ЧИТАЕТ врач, а пара — типизированная
// связь, по которой генерация подбирает неверные варианты. Текст лежит в
// положении, здесь — только связь: два места для одного текста расходятся
// молча, а место для связи в схеме ровно одно.
type Differential struct {
	UnitLabel   string
	Counterpart string
	Ord         int
}

// Catalog — что получилось из выгрузки.
type Catalog struct {
	Units         []Unit
	Statements    []Statement
	Differentials []Differential
	Report        CatalogReport
}

// ParseCatalog разбирает выгрузку каталога в единицы и положения.
//
// Родитель записи ищется по САМОЙ ДЛИННОЙ чужой метке, которая является
// началом этой. Это не разбор кода МКБ: правило сформулировано через сами
// метки выгрузки и одинаково работает для «F32.1» внутри «F32» и для
// пункта «3.2.1» внутри «3.2». Не нашлось ничего — родителем становится
// раздел, названный самой выгрузкой.
func ParseCatalog(raw []byte) (Catalog, error) {
	var file catalogFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return Catalog{}, fmt.Errorf("каталог не разобран: %w", err)
	}
	if file.SchemaVersion != CatalogSchema {
		return Catalog{}, fmt.Errorf(
			"каталог версии %d, а разбор понимает %d: чужая версия не применяется целиком",
			file.SchemaVersion, CatalogSchema)
	}

	report := CatalogReport{Dropped: map[string]string{}}
	units := make([]Unit, 0, len(file.Groups)+len(file.Diagnoses))
	known := map[string]bool{}

	for i, g := range file.Groups {
		code := strings.TrimSpace(g.Code)
		name := strings.TrimSpace(g.Name)
		if code == "" || name == "" {
			report.Dropped[fmt.Sprintf("раздел %d", i+1)] = "без метки или без названия"
			continue
		}
		if known[code] {
			report.Dropped[code] = "метка встречается дважды"
			continue
		}
		known[code] = true
		units = append(units, Unit{
			Label: code, Title: name, Kind: KindGroup,
			// Отвечаемость следует из рода, а не задаётся рядом с ним:
			// два поля об одном расходятся молча. По разделу задачу
			// сгенерировать нельзя — отгадывать в нём нечего.
			Answerable: false,
			Ord:        g.SortOrder,
		})
	}

	// Метки записей нужны целиком до того, как ищется родитель: родителем
	// бывает запись, стоящая в выгрузке ниже.
	entryLabels := make([]string, 0, len(file.Diagnoses))
	for _, d := range file.Diagnoses {
		if code := strings.TrimSpace(d.Code); code != "" {
			entryLabels = append(entryLabels, code)
		}
	}
	// От длинных к коротким: первая подошедшая и есть самая длинная.
	sort.Slice(entryLabels, func(a, b int) bool {
		return len(entryLabels[a]) > len(entryLabels[b])
	})

	for i, d := range file.Diagnoses {
		code := strings.TrimSpace(d.Code)
		title := strings.TrimSpace(d.Title)
		if code == "" || title == "" {
			report.Dropped[fmt.Sprintf("запись %d", i+1)] = "без метки или без названия"
			continue
		}
		if known[code] {
			report.Dropped[code] = "метка встречается дважды"
			continue
		}
		known[code] = true
		if detail := strings.TrimSpace(d.Detail); detail != "" {
			title = title + ". " + detail
		}
		units = append(units, Unit{
			Label:       code,
			ParentLabel: catalogParent(code, entryLabels, strings.TrimSpace(d.GroupCode)),
			Title:       title,
			Kind:        KindEntry,
			Answerable:  true,
			Ord:         i,
		})
	}

	for i := range units {
		if units[i].Kind == KindGroup {
			report.Groups++
		} else {
			report.Entries++
		}
	}

	statements := make([]Statement, 0, len(file.Criteria)+len(file.Differential))
	ord := map[string]int{}
	for i, c := range file.Criteria {
		code := strings.TrimSpace(c.Code)
		body := strings.TrimSpace(c.Content)
		if code == "" || body == "" {
			report.Dropped[fmt.Sprintf("критерий %d", i+1)] = "без метки или без текста"
			continue
		}
		if !known[code] {
			report.Dropped[code] = "критерий ссылается на метку, которой нет в каталоге"
			continue
		}
		statements = append(statements, Statement{
			UnitLabel: code,
			Kind:      catalogKind(c.Type),
			Body:      body,
			PlaceRef:  strings.TrimSpace(c.Citation),
			Ord:       ord[code],
		})
		ord[code]++
		report.Criteria++
	}

	differentials := make([]Differential, 0, len(file.Differential))
	for i, d := range file.Differential {
		code := strings.TrimSpace(d.Code)
		body := strings.TrimSpace(d.Features)
		with := strings.TrimSpace(d.With)
		if code == "" || body == "" {
			report.Dropped[fmt.Sprintf("отличие %d", i+1)] = "без метки или без текста"
			continue
		}
		if !known[code] {
			report.Dropped[code] = "отличие ссылается на метку, которой нет в каталоге"
			continue
		}
		statements = append(statements, Statement{
			UnitLabel: code,
			Kind:      catalogDifferential,
			// С чем именно путают — это и есть обозначение положения: по
			// нему врач ищет нужное отличие глазами, а в теле оно
			// потерялось бы среди строк таблицы.
			Designation: with,
			Body:        body,
			Ord:         ord[code],
		})
		// Связь пишется только тогда, когда обе стороны — единицы этого
		// же каталога. Пара, ссылающаяся в пустоту, увела бы подбор
		// неверных вариантов на метку, которой нет, и заметить это можно
		// было бы только по странным вариантам в готовой задаче.
		if with != "" && known[with] {
			differentials = append(differentials, Differential{
				UnitLabel:   code,
				Counterpart: with,
				Ord:         i,
			})
		} else if with != "" {
			report.Dropped[code+" ↔ "+with] = "пара ссылается на метку, которой нет в каталоге"
		}
		ord[code]++
		report.Different++
	}

	return Catalog{
		Units:         units,
		Statements:    statements,
		Differentials: differentials,
		Report:        report,
	}, nil
}

// catalogParent ищет родителя записи среди чужих меток.
func catalogParent(code string, labels []string, group string) string {
	for _, other := range labels {
		if other == code || len(other) >= len(code) {
			continue
		}
		if strings.HasPrefix(code, other) {
			return other
		}
	}
	return group
}

// Слова родов положений.
//
// Слово рода показывается врачу как есть, и «cddg» на экране сказало бы
// ему, что приложение не знает, что показывает. Перевод здесь, а не в
// приложении: приложение показывает слово источника, каким бы источник ни
// был, и знать сокращения чужих выгрузок ему незачем.
const catalogDifferential = "дифференциальный диагноз"

func catalogKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "cddg":
		return "клинические описания"
	case "dcr10":
		return "диагностические критерии"
	case "":
		return ""
	default:
		// Незнакомое слово рода не теряется и не подменяется общим: показ
		// даст ему общий знак и свою группу, и ничего от этого не сломается.
		return raw
	}
}
