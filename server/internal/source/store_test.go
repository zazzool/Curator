package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"curator/server/internal/dbgate"
)

// testGate открывает дверь к проверочной базе.
//
// Без строки подключения — отказ, а не пропуск. Проверка, которая молча
// пропускается и возвращает успех, выдаёт зелёное за непроверенное; ровно
// так однажды перестала работать сверка каталога величин у донора. Набор
// tools/checks.sh строку подключения задаёт сам, так что цена этого правила
// — ноль.
func testGate(t *testing.T) *dbgate.Gate {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут. " +
			"Это отказ, а не пропуск — см. server/.env.example")
	}
	gate, err := dbgate.Open(context.Background(), dsn, dbgate.Options{})
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

// newSource заводит источник со своим кратким именем.
//
// Своим — чтобы проверки не мешали друг другу: база одна на весь прогон, и
// две проверки, взявшие одно имя, ловили бы друг друга за руку через раз.
func newSource(t *testing.T, s *Store) int64 {
	t.Helper()
	slug := fmt.Sprintf("проверка-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	id, err := s.CreateSource(context.Background(), Source{
		Slug:          slug,
		Kind:          KindDecree,
		Title:         "Приказ для проверки",
		UnitWord:      "пункт",
		StatementWord: "положение",
		Purpose:       PurposeLegal,
		Hierarchy:     HierarchyPartOf,
		Completeness:  CompletenessFragment,
	})
	if err != nil {
		t.Fatalf("источник не заведён: %v", err)
	}
	return id
}

func TestPgИсточникБезСловаряНеЗаводится(t *testing.T) {
	// Словарь интерфейса пустым не бывает: без него студия покажет
	// «единица» врачу, который ждёт слова «пункт».
	s := NewStore(testGate(t))
	_, err := s.CreateSource(context.Background(), Source{
		Slug: "без-словаря", Kind: KindDecree, Title: "Приказ",
		Purpose: PurposeLegal, Hierarchy: HierarchyPartOf,
		Completeness: CompletenessFragment,
	})
	if err == nil {
		t.Fatal("источник без словаря интерфейса заведён")
	}
}

func TestPgИсточникБезПолнотыНеЗаводится(t *testing.T) {
	// Умолчания у полноты нет намеренно: разобранный кусок, объявленный
	// полным, даёт ложные доли охвата.
	s := NewStore(testGate(t))
	_, err := s.CreateSource(context.Background(), Source{
		Slug: "без-полноты", Kind: KindDecree, Title: "Приказ",
		UnitWord: "пункт", StatementWord: "положение",
		Purpose: PurposeLegal, Hierarchy: HierarchyPartOf,
	})
	if err == nil {
		t.Fatal("источник без названной полноты заведён")
	}
}

func TestPgОдинФайлДваждыОстаётсяОднимДокументом(t *testing.T) {
	// Иначе разбор пойдёт по обеим копиям и даст две редакции одного
	// источника.
	ctx := context.Background()
	s := NewStore(testGate(t))
	body := []byte(fmt.Sprintf("приказ %d", time.Now().UnixNano()))
	sum := sha256.Sum256(body)
	doc := Document{
		Filename: "приказ.md", MIME: "text/markdown",
		SHA256: hex.EncodeToString(sum[:]), Body: body,
	}
	first, err := s.SaveDocument(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SaveDocument(ctx, doc)
	if err != nil {
		t.Fatalf("повторная загрузка того же файла отказала: %v", err)
	}
	if first != second {
		t.Errorf("тот же файл лёг двумя документами: %d и %d", first, second)
	}
}

// draftDoc кладёт документ и черновик разбора, возвращая номера.
func draftDoc(t *testing.T, s *Store, sourceID int64, units []Unit, statements []Statement) int64 {
	t.Helper()
	ctx := context.Background()
	body := []byte(fmt.Sprintf("документ %d-%d", time.Now().UnixNano(), rand.Intn(1000)))
	sum := sha256.Sum256(body)
	docID, err := s.SaveDocument(ctx, Document{
		SourceID: sourceID, Filename: "приказ.docx",
		MIME:   "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		SHA256: hex.EncodeToString(sum[:]), Body: body,
	})
	if err != nil {
		t.Fatalf("документ не сохранён: %v", err)
	}
	if err := s.SaveDraft(ctx, docID, units, statements); err != nil {
		t.Fatalf("черновик не сохранён: %v", err)
	}
	return docID
}

func TestPgПриёмкаЧерновикаСчитаетПуть(t *testing.T) {
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)
	docID := draftDoc(t, s, srcID,
		[]Unit{
			{Label: "3", Title: "Порядок"},
			{Label: "3.2", ParentLabel: "3", Title: "Показания"},
			{Label: "3.2.1", ParentLabel: "3.2", Title: "Первое"},
		},
		[]Statement{{UnitLabel: "3.2.1", Body: "Помощь оказывается при…", Designation: "абз. 1"}})

	n, err := s.AcceptDraft(ctx, srcID, docID, "врач")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("принято единиц %d, ожидалось 3", n)
	}

	units, err := s.Units(ctx, srcID)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 3 {
		t.Fatalf("в источнике %d единиц", len(units))
	}
	if units[2].Path != "3/3.2/3.2.1" || units[2].Depth != 2 {
		t.Errorf("путь %q глубина %d", units[2].Path, units[2].Depth)
	}
}

func TestPgПриёмкаОтказываетЦеликомНаПотеряннойВетке(t *testing.T) {
	// Главная проверка приёмки: источник с половиной принятой ветки хуже
	// непринятого — у части единиц путь есть, у части нет, и срез отдаёт
	// то одно, то другое.
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)
	docID := draftDoc(t, s, srcID,
		[]Unit{
			{Label: "3", Title: "Порядок"},
			{Label: "4.1", ParentLabel: "4", Title: "Сирота"},
		}, nil)

	if _, err := s.AcceptDraft(ctx, srcID, docID, "врач"); err == nil {
		t.Fatal("черновик с потерянным родителем принят")
	}
	units, err := s.Units(ctx, srcID)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 0 {
		t.Errorf("после отказа в источнике осталось %d единиц: приёмка не одна транзакция", len(units))
	}
}

func TestPgСрезНаSQLСходитсяСРазбором(t *testing.T) {
	// Совпадение двух реализаций. Срез написан и на Go (Slice), и на SQL
	// (SliceUnits): первый нужен разбору, второй — запросу по тысяче
	// единиц. Такие пары расходятся молча, поэтому сверяются поле за
	// полем.
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)
	docID := draftDoc(t, s, srcID, []Unit{
		{Label: "F", Title: "Класс"},
		{Label: "F3", ParentLabel: "F", Title: "Раздел"},
		{Label: "F30", ParentLabel: "F", Title: "Сосед по началу строки"},
		{Label: "F32", ParentLabel: "F3", Title: "Рубрика"},
		{Label: "F32.1", ParentLabel: "F32", Title: "Диагноз"},
	}, nil)
	if _, err := s.AcceptDraft(ctx, srcID, docID, "врач"); err != nil {
		t.Fatal(err)
	}

	all, err := s.Units(ctx, srcID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "F", "F/F3", "F/F3/F32", "нет-такого"} {
		fromSQL, err := s.SliceUnits(ctx, srcID, path)
		if err != nil {
			t.Fatalf("срез %q на SQL: %v", path, err)
		}
		fromGo := Slice(all, path)
		if len(fromSQL) != len(fromGo) {
			t.Fatalf("срез %q: на SQL %d единиц, разбором %d", path, len(fromSQL), len(fromGo))
		}
		for i := range fromGo {
			if fromSQL[i] != fromGo[i] {
				t.Errorf("срез %q, единица %d: SQL %+v, разбор %+v", path, i+1, fromSQL[i], fromGo[i])
			}
		}
	}

	// Отдельно — сосед по началу строки: без разделителя в сверке срез F3
	// утащил бы F30, и молча.
	got, err := s.SliceUnits(ctx, srcID, "F/F3")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range got {
		if u.Label == "F30" {
			t.Error("в срез F3 попал сосед F30")
		}
	}
}

func TestPgСрезНеЛовитсяНаПодчёркиваниеВМетке(t *testing.T) {
	// Для LIKE подчёркивание — «любой знак». Без экранирования срез по
	// пути «п_1» захватил бы «п-1», то есть отдал бы чужие единицы.
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)
	docID := draftDoc(t, s, srcID, []Unit{
		{Label: "п_1", Title: "Свой"},
		{Label: "п_1.1", ParentLabel: "п_1", Title: "Свой вложенный"},
		{Label: "п-1", Title: "Чужой"},
		{Label: "п-1.1", ParentLabel: "п-1", Title: "Чужой вложенный"},
	}, nil)
	if _, err := s.AcceptDraft(ctx, srcID, docID, "врач"); err != nil {
		t.Fatal(err)
	}

	got, err := s.SliceUnits(ctx, srcID, "п_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("в срезе %d единиц, ожидалось 2: %+v", len(got), got)
	}
	for _, u := range got {
		if u.Label == "п-1" || u.Label == "п-1.1" {
			t.Errorf("в срез п_1 попала чужая единица %q", u.Label)
		}
	}
}

func TestPgПустойСрезНеNil(t *testing.T) {
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)
	got, err := s.SliceUnits(ctx, srcID, "нет-такого")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("вернулся nil вместо пустого списка")
	}
}

func TestPgПовторныйРазборЗаменяетКуски(t *testing.T) {
	// Куски прошлого разбора, оставшиеся рядом с новыми, — это два ответа
	// на один вопрос, и модель получит оба.
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)
	docID := draftDoc(t, s, srcID, []Unit{{Label: "1", Title: "Пункт"}}, nil)

	if err := s.SaveFragments(ctx, docID, SplitText("# Первый\nтело\n# Второй\nтело\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFragments(ctx, docID, SplitText("# Единственный\nтело\n")); err != nil {
		t.Fatal(err)
	}

	var count int
	err := s.gate.QueryRow(ctx,
		`SELECT count(*) FROM source_fragments WHERE document_id = $1`, docID).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("кусков после повторного разбора %d, ожидался 1", count)
	}
}

func TestPgЗаголовокКускаУезжаетВместеСТелом(t *testing.T) {
	// Без заголовка кусок теряет то единственное, что говорит, о чём он, —
	// и модель дописывает это сама, то есть выдумывает.
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)
	docID := draftDoc(t, s, srcID, []Unit{{Label: "1", Title: "Пункт"}}, nil)
	if err := s.SaveFragments(ctx, docID, SplitText("# Показания\nтело пункта\n")); err != nil {
		t.Fatal(err)
	}
	var body string
	err := s.gate.QueryRow(ctx,
		`SELECT body_md FROM source_fragments WHERE document_id = $1 ORDER BY ord LIMIT 1`, docID).Scan(&body)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Показания\n\nтело пункта"; body != want {
		t.Errorf("кусок %q, ожидался %q", body, want)
	}
}

func TestPgТотЖеФайлВДругомИсточникеЭтоДругойДокумент(t *testing.T) {
	// Совпадение по отпечатку считается внутри источника. Считалось оно
	// по всей базе, и это стоило подмены: приказ, положенный во второй
	// источник, возвращал документ первого — вместе с его источником.
	// Дальше приёмка разбора принимала его в источник, которого
	// составитель не открывал, и ответ при этом был успешным.
	ctx := context.Background()
	s := NewStore(testGate(t))
	first := newSource(t, s)
	second := newSource(t, s)

	body := []byte(fmt.Sprintf("общий приказ %d", time.Now().UnixNano()))
	sum := sha256.Sum256(body)
	doc := Document{
		Filename: "приказ.md", MIME: "text/markdown",
		SHA256: hex.EncodeToString(sum[:]), Body: body,
	}

	doc.SourceID = first
	inFirst, err := s.SaveDocument(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.SourceID = second
	inSecond, err := s.SaveDocument(ctx, doc)
	if err != nil {
		t.Fatalf("тот же файл во втором источнике не принят: %v", err)
	}
	if inFirst == inSecond {
		t.Fatalf("файл во втором источнике вернул документ первого: %d", inSecond)
	}

	// И повтор внутри одного источника по-прежнему один документ.
	doc.SourceID = first
	again, err := s.SaveDocument(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if again != inFirst {
		t.Errorf("тот же файл в том же источнике лёг дважды: %d и %d", inFirst, again)
	}
}

func TestPgПоложенияПодВыпущеннойЗадачейНеЗаменяются(t *testing.T) {
	// Разметка выпущенной задачи показывает на положения источника. Убери
	// их приёмка нового разбора — и врач увидел бы задачу без обоснования.
	// База это и так не даст (внешний ключ на case_chunks), но её отказ
	// приезжает составителю кодом нарушения; проверяем, что отказ говорит
	// словами и называет единицу.
	ctx := context.Background()
	gate := testGate(t)
	s := NewStore(gate)
	sourceID := newSource(t, s)

	units := []Unit{{Label: "п1", Title: "Пункт первый"}}
	statements := []Statement{{UnitLabel: "п1", Kind: "criterion", Designation: "абз. 1", Body: "Первое положение"}}
	docID := draftDoc(t, s, sourceID, units, statements)
	if _, err := s.AcceptDraft(ctx, sourceID, docID, "проверка"); err != nil {
		t.Fatalf("первая приёмка отказала: %v", err)
	}

	// Выпущенная задача с разметкой на это положение.
	var stID int64
	err := gate.QueryRow(ctx,
		`SELECT id FROM source_unit_statements WHERE source_id = $1 AND unit_label = 'п1'`,
		sourceID).Scan(&stID)
	if err != nil {
		t.Fatalf("положение не найдено: %v", err)
	}
	caseID := fmt.Sprintf("c-проверка-%d", time.Now().UnixNano())
	if _, err := gate.Exec(ctx,
		`INSERT INTO cases (id, source_id, unit_label, unit_path, status, body)
		 VALUES ($1, $2, 'п1', 'п1', 'published', '{}'::jsonb)`, caseID, sourceID); err != nil {
		t.Fatalf("задача не заведена: %v", err)
	}
	if _, err := gate.Exec(ctx,
		`INSERT INTO case_chunks (case_id, ord, text, statement_id) VALUES ($1, 0, 'фрагмент', $2)`,
		caseID, stID); err != nil {
		t.Fatalf("разметка не заведена: %v", err)
	}

	second := draftDoc(t, s, sourceID, units,
		[]Statement{{UnitLabel: "п1", Kind: "criterion", Designation: "абз. 1", Body: "Переписанное положение"}})
	_, err = s.AcceptDraft(ctx, sourceID, second, "проверка")
	if err == nil {
		t.Fatal("приёмка заменила положения, на которые ссылается выпущенная задача")
	}
	for _, want := range []string{"п1", "выпущенных задач"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ не называет %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "SQLSTATE") {
		t.Errorf("отказ приехал кодом нарушения, а не словами: %v", err)
	}
}

func TestPgПриёмкаУвеличиваетВыпускСправочника(t *testing.T) {
	// По этому числу устройство понимает, что лежащая у него офлайн-копия
	// отстала. Не меняйся оно приёмкой — врач читал бы старые критерии,
	// а приложение считало бы копию свежей: расхождение молчаливое и
	// ровно в ту сторону, где ошибка опаснее всего.
	ctx := context.Background()
	gate := testGate(t)
	s := NewStore(gate)
	sourceID := newSource(t, s)

	version := func() int {
		t.Helper()
		var v int
		if err := gate.QueryRow(ctx,
			`SELECT reference_version FROM sources WHERE id = $1`, sourceID).Scan(&v); err != nil {
			t.Fatalf("выпуск справочника не прочитан: %v", err)
		}
		return v
	}

	before := version()
	docID := draftDoc(t, s, sourceID,
		[]Unit{{Label: "п1", Title: "Пункт первый"}},
		[]Statement{{UnitLabel: "п1", Kind: "criterion", Designation: "абз. 1", Body: "Положение"}})
	if _, err := s.AcceptDraft(ctx, sourceID, docID, "проверка"); err != nil {
		t.Fatalf("приёмка отказала: %v", err)
	}
	if after := version(); after <= before {
		t.Errorf("выпуск справочника не вырос: было %d, стало %d", before, after)
	}
}

func TestPgПустойИсточникДействующимНеОбъявляется(t *testing.T) {
	// Объявленный действующим источник без единой единицы врач увидит как
	// пустую книгу и решит, что приложение сломано. Сломано при этом не
	// приложение, а решение, и сказать об этом надо тому, кто его
	// принимает.
	ctx := context.Background()
	s := NewStore(testGate(t))
	sourceID := newSource(t, s)

	err := s.SetStatus(ctx, sourceID, StatusActive)
	if err == nil {
		t.Fatal("пустой источник объявлен действующим")
	}
	if !strings.Contains(err.Error(), "примите разбор") {
		t.Errorf("отказ не говорит, что делать: %v", err)
	}

	src, err := s.SourceByID(ctx, sourceID)
	if err != nil {
		t.Fatalf("паспорт не прочитан: %v", err)
	}
	if src.Status != StatusDraft {
		t.Errorf("состояние поменялось вопреки отказу: %q", src.Status)
	}
}

func TestPgИсточникОбъявляетсяДействующимИОбратно(t *testing.T) {
	// До этого вызова источника для устройства не существует: справочник
	// отдаёт только действующие. Не будь этого действия вовсе — а его и не
	// было, — офлайн-справочник был бы пуст у всех и всегда.
	ctx := context.Background()
	s := NewStore(testGate(t))
	sourceID := newSource(t, s)
	docID := draftDoc(t, s, sourceID,
		[]Unit{{Label: "п1", Title: "Пункт первый"}},
		[]Statement{{UnitLabel: "п1", Kind: "criterion", Body: "Положение"}})
	if _, err := s.AcceptDraft(ctx, sourceID, docID, "проверка"); err != nil {
		t.Fatalf("приёмка отказала: %v", err)
	}

	if err := s.SetStatus(ctx, sourceID, StatusActive); err != nil {
		t.Fatalf("источник не объявлен действующим: %v", err)
	}
	src, _ := s.SourceByID(ctx, sourceID)
	if src.Status != StatusActive {
		t.Fatalf("состояние не записано: %q", src.Status)
	}

	// Обратно — можно: источник, объявленный по ошибке, снимается тем же
	// действием, а не правкой в базе руками.
	if err := s.SetStatus(ctx, sourceID, StatusRetired); err != nil {
		t.Fatalf("источник не отменён: %v", err)
	}
	src, _ = s.SourceByID(ctx, sourceID)
	if src.Status != StatusRetired {
		t.Errorf("состояние не записано: %q", src.Status)
	}
}

func TestPgСостоянияВнеСловаряНеПринимаются(t *testing.T) {
	// Словарь закрыт тем же, чем закрыт в схеме: состояние, появившееся
	// строкой в коде, прошло бы проверку схемы и осталось бы невидимым для
	// всех списков.
	ctx := context.Background()
	s := NewStore(testGate(t))
	sourceID := newSource(t, s)

	if err := s.SetStatus(ctx, sourceID, "опубликован"); err == nil {
		t.Fatal("придуманное состояние принято")
	}
	if err := s.SetStatus(ctx, 10_000_000, StatusDraft); err == nil {
		t.Fatal("состояние записано несуществующему источнику")
	}
}

func TestPgРодЕдиницыДоезжаетДоИсточника(t *testing.T) {
	// Род берётся у разбора, а не угадывается по виду метки: «если это
	// МКБ» — дефект. Приёмка хранила его только в черновике и записывала
	// всем «запись», и девятьсот диагнозов ложились одним плоским списком
	// без единого входа в него.
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)
	docID := draftDoc(t, s, srcID, []Unit{
		{Label: "F30-F39", Title: "Расстройства настроения", Kind: KindGroup},
		{Label: "F32", ParentLabel: "F30-F39", Title: "Депрессивный эпизод"},
	}, nil)

	if _, err := s.AcceptDraft(ctx, srcID, docID, "врач"); err != nil {
		t.Fatal(err)
	}
	units, err := s.Units(ctx, srcID)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("в источнике %d единиц", len(units))
	}
	byLabel := map[string]Unit{}
	for _, u := range units {
		byLabel[u.Label] = u
	}
	group := byLabel["F30-F39"]
	if group.Kind != KindGroup {
		t.Errorf("род группы %q, ожидался %q", group.Kind, KindGroup)
	}
	// Задача по группе — это задача «отгадайте раздел», и отгадывать в ней
	// нечего: группа отвечаемой не бывает, и следует это из рода, а не из
	// отдельного поля черновика.
	if group.Answerable {
		t.Error("группа объявлена отвечаемой: по разделу будет сгенерирована задача")
	}
	entry := byLabel["F32"]
	if entry.Kind != KindEntry {
		t.Errorf("род записи %q, ожидался %q", entry.Kind, KindEntry)
	}
	if !entry.Answerable {
		t.Error("запись объявлена неотвечаемой: по ней не будет задач")
	}
}

func TestPgПовторнаяПриёмкаМеняетРод(t *testing.T) {
	// Разбор ошибся родом — второй заход обязан его исправить, а не
	// оставить прежний. Прежний ключ включал род, и «F32» записью и «F32»
	// группой уживались в таблице двумя строками: чтение по метке отдавало
	// то одну, то другую, и какую именно — зависело от порядка вставки.
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)

	first := draftDoc(t, s, srcID, []Unit{
		{Label: "F32", Title: "Депрессивный эпизод", Kind: KindEntry},
	}, nil)
	if _, err := s.AcceptDraft(ctx, srcID, first, "врач"); err != nil {
		t.Fatal(err)
	}
	second := draftDoc(t, s, srcID, []Unit{
		{Label: "F32", Title: "Депрессивный эпизод", Kind: KindGroup},
	}, nil)
	if _, err := s.AcceptDraft(ctx, srcID, second, "врач"); err != nil {
		t.Fatal(err)
	}

	units, err := s.Units(ctx, srcID)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("метка «F32» легла %d строками, а не одной", len(units))
	}
	if units[0].Kind != KindGroup || units[0].Answerable {
		t.Errorf("род остался %q (отвечаемость %v): повторная приёмка не исправила его",
			units[0].Kind, units[0].Answerable)
	}
}

func TestPgПарыПутаютСЗамещаютсяЦеликом(t *testing.T) {
	// Разбор источника переделывают, и пары прошлого разбора рядом с
	// новыми — это два ответа на вопрос «с чем это путают». Подбор
	// неверных вариантов взял бы из них случайный.
	ctx := context.Background()
	s := NewStore(testGate(t))
	srcID := newSource(t, s)

	if err := s.SaveDifferentials(ctx, srcID, []Differential{
		{UnitLabel: "F32", Counterpart: "F41.2", Ord: 0},
		{UnitLabel: "F32", Counterpart: "F33", Ord: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDifferentials(ctx, srcID, []Differential{
		{UnitLabel: "F32", Counterpart: "F41.2", Ord: 0},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Differentials(ctx, srcID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Counterpart != "F41.2" {
		t.Fatalf("после второго разбора пар %d: %v", len(got), got)
	}
}

func TestPgПустыхПарЭтоПустойСписок(t *testing.T) {
	// У источника без такой разметки пар просто нет, и это исправный
	// случай: пустое значение вместо списка роняет читающего белым
	// экраном именно на хороших источниках.
	ctx := context.Background()
	s := NewStore(testGate(t))
	got, err := s.Differentials(ctx, newSource(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("пустой список отдан пустым значением")
	}
	if len(got) != 0 {
		t.Errorf("у нового источника %d пар", len(got))
	}
}
