package source

import (
	"strings"
	"testing"
)

const catalogSample = `{
  "schemaVersion": 1,
  "groups": [
    {"code": "F30-F39", "name": "Расстройства настроения", "sort_order": 530}
  ],
  "diagnoses": [
    {"code": "F32.1", "title": "Умеренный эпизод", "detail": null, "group_code": "F30-F39"},
    {"code": "F32", "title": "Депрессивный эпизод", "detail": null, "group_code": "F30-F39"},
    {"code": "F3", "title": "Расстройства настроения (обобщённо)", "detail": null, "group_code": "F30-F39"},
    {"code": "F41.2", "title": "Смешанное тревожное расстройство", "detail": null, "group_code": "F30-F39"}
  ],
  "criteria": [
    {"code": "F32", "criteria_type": "cddg", "content_md": "Сниженное настроение…",
     "source_citation": "ВОЗ, CDDG 1992, раздел F32."},
    {"code": "F32", "criteria_type": "dcr10", "content_md": "Не менее двух недель.",
     "source_citation": ""}
  ],
  "differential": [
    {"code": "F32", "differential_with": "F41.2",
     "distinguishing_features_md": "| Признак | F32 | F41.2 |"}
  ]
}`

func parsed(t *testing.T, raw string) Catalog {
	t.Helper()
	catalog, err := ParseCatalog([]byte(raw))
	if err != nil {
		t.Fatalf("разбор отказал: %v", err)
	}
	return catalog
}

func TestКаталогРазложенНаРодыИСчитан(t *testing.T) {
	catalog := parsed(t, catalogSample)
	units, statements, report := catalog.Units, catalog.Statements, catalog.Report

	if report.Groups != 1 || report.Entries != 4 {
		t.Errorf("разделов %d, записей %d; ожидалось 1 и 4", report.Groups, report.Entries)
	}
	if report.Criteria != 2 || report.Different != 1 {
		t.Errorf("критериев %d, отличий %d; ожидалось 2 и 1", report.Criteria, report.Different)
	}
	if len(units) != 5 || len(statements) != 3 {
		t.Fatalf("единиц %d, положений %d", len(units), len(statements))
	}
	if len(report.Dropped) != 0 {
		t.Errorf("отброшено лишнее: %v", report.Dropped)
	}
	for _, u := range units {
		if u.Kind == KindGroup && u.Answerable {
			t.Errorf("раздел %q объявлен отвечаемым: по нему будет сгенерирована задача", u.Label)
		}
		if u.Kind == KindEntry && !u.Answerable {
			t.Errorf("запись %q объявлена неотвечаемой: по ней не будет задач", u.Label)
		}
	}
}

func TestРодительБерётсяСамойДлиннойМеткой(t *testing.T) {
	// Главное правило разбора. Возьми первую подошедшую, и «F32.1» встало
	// бы под «F3» — на уровень выше своего места, — а в срезе по «F32»
	// его бы не было вовсе. Заметить это можно только открыв F32.
	units := parsed(t, catalogSample).Units
	byLabel := map[string]Unit{}
	for _, u := range units {
		byLabel[u.Label] = u
	}
	if got := byLabel["F32.1"].ParentLabel; got != "F32" {
		t.Errorf("родитель F32.1 — %q, ожидался F32", got)
	}
	if got := byLabel["F32"].ParentLabel; got != "F3" {
		t.Errorf("родитель F32 — %q, ожидался F3", got)
	}
	// Записи, над которой нет другой записи, родителем становится раздел,
	// названный самой выгрузкой, — а не угаданный по началу метки.
	if got := byLabel["F3"].ParentLabel; got != "F30-F39" {
		t.Errorf("родитель F3 — %q, ожидался раздел F30-F39", got)
	}
}

func TestРазобранныйКаталогСобираетсяВПути(t *testing.T) {
	// Разбор и построение путей — соседние шаги, и первый обязан отдавать
	// то, что второй примет: оборванная цепочка родителей роняет приёмку
	// целиком, и узнать об этом на разборе дешевле, чем на приёмке.
	units := parsed(t, catalogSample).Units
	withPaths, err := BuildPaths(units)
	if err != nil {
		t.Fatalf("пути не посчитались: %v", err)
	}
	for _, u := range withPaths {
		if u.Label == "F32.1" && u.Path != "F30-F39/F3/F32/F32.1" {
			t.Errorf("путь F32.1 — %q", u.Path)
		}
	}
}

func TestСловоРодаПереводитсяВРазборе(t *testing.T) {
	// «cddg» на экране сказало бы врачу, что приложение не знает, что
	// показывает. Перевод стоит здесь, а не в приложении: приложение
	// показывает слово источника, каким бы источник ни был.
	statements := parsed(t, catalogSample).Statements
	kinds := map[string]bool{}
	for _, s := range statements {
		kinds[s.Kind] = true
	}
	for _, want := range []string{"клинические описания", "диагностические критерии", catalogDifferential} {
		if !kinds[want] {
			t.Errorf("рода %q нет среди %v", want, kinds)
		}
	}
	if kinds["cddg"] || kinds["dcr10"] {
		t.Error("сокращение выгрузки доехало до показа")
	}
}

func TestОтличиеНесётСЧемПутают(t *testing.T) {
	statements := parsed(t, catalogSample).Statements
	for _, s := range statements {
		if s.Kind != catalogDifferential {
			continue
		}
		if s.Designation != "F41.2" {
			t.Errorf("обозначение отличия %q, ожидалось F41.2", s.Designation)
		}
		return
	}
	t.Fatal("отличие не разобрано")
}

func TestПоложенияОдногоКодаНеДелятОдинНомер(t *testing.T) {
	// Порядок положений внутри рубрики задаётся номером, и два положения
	// с нулём показались бы в порядке, зависящем от базы, — то есть в
	// разном при каждом чтении.
	statements := parsed(t, catalogSample).Statements
	seen := map[string]map[int]bool{}
	for _, s := range statements {
		if seen[s.UnitLabel] == nil {
			seen[s.UnitLabel] = map[int]bool{}
		}
		if seen[s.UnitLabel][s.Ord] {
			t.Fatalf("у %q два положения с номером %d", s.UnitLabel, s.Ord)
		}
		seen[s.UnitLabel][s.Ord] = true
	}
}

func TestЧужаяВерсияКаталогаОтбрасываетсяЦеликом(t *testing.T) {
	// Наполовину понятый каталог кладёт наполовину верный справочник, и
	// заметить это можно только открыв нужную рубрику.
	_, err := ParseCatalog([]byte(`{"schemaVersion": 2, "groups": []}`))
	if err == nil {
		t.Fatal("каталог чужой версии разобран")
	}
	if !strings.Contains(err.Error(), "версии") {
		t.Errorf("отказ не называет причину: %v", err)
	}
}

func TestСиротаСчитаетсяИНазывается(t *testing.T) {
	// Молча выброшенный критерий выглядит как «столько в выгрузке и
	// было», и объяснить это потом нечем.
	catalog := parsed(t, `{
	  "schemaVersion": 1,
	  "groups": [], "diagnoses": [],
	  "criteria": [{"code": "F99", "criteria_type": "cddg", "content_md": "Текст", "source_citation": ""}],
	  "differential": []
	}`)
	statements, report := catalog.Statements, catalog.Report
	if len(statements) != 0 {
		t.Error("критерий без своей рубрики всё-таки лёг")
	}
	if report.Criteria != 0 {
		t.Errorf("отброшенный критерий посчитан принятым: %d", report.Criteria)
	}
	if _, named := report.Dropped["F99"]; !named {
		t.Errorf("отброшенное не названо: %v", report.Dropped)
	}
}

func TestПараПутаютСДоезжаетДоПодбора(t *testing.T) {
	// Подбор неверных вариантов опирается на пары: без них он берёт
	// соседей по родителю, то есть случайных. Текста различий в паре нет
	// намеренно — он лежит положением, которое читает врач.
	catalog := parsed(t, catalogSample)
	if len(catalog.Differentials) != 1 {
		t.Fatalf("пар %d, ожидалась одна: %v", len(catalog.Differentials), catalog.Differentials)
	}
	pair := catalog.Differentials[0]
	if pair.UnitLabel != "F32" || pair.Counterpart != "F41.2" {
		t.Errorf("пара %q ↔ %q", pair.UnitLabel, pair.Counterpart)
	}
}

func TestПараВПустотуНеПишетсяИНазывается(t *testing.T) {
	// Пара, ссылающаяся на метку, которой нет, увела бы подбор на
	// несуществующую единицу, и заметить это можно было бы только по
	// странным вариантам в готовой задаче.
	catalog := parsed(t, `{
	  "schemaVersion": 1,
	  "groups": [],
	  "diagnoses": [{"code": "F32", "title": "Эпизод", "detail": null, "group_code": ""}],
	  "criteria": [],
	  "differential": [{"code": "F32", "differential_with": "F99.9",
	                    "distinguishing_features_md": "Текст"}]
	}`)
	if len(catalog.Differentials) != 0 {
		t.Error("пара в пустоту всё-таки записана")
	}
	// Само положение при этом остаётся: врач читает различия и тогда,
	// когда второй рубрики в этом каталоге нет.
	if len(catalog.Statements) != 1 {
		t.Errorf("положений %d, ожидалось одно", len(catalog.Statements))
	}
	if _, named := catalog.Report.Dropped["F32 ↔ F99.9"]; !named {
		t.Errorf("отброшенная пара не названа: %v", catalog.Report.Dropped)
	}
}
