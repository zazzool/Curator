package gen

import (
	"context"
	"strings"
	"testing"
)

// кУсловию — черновик, который отдают редактору.
func кУсловию() Draft {
	return Draft{
		Title: "Срок рассмотрения",
		Segments: []Segment{
			{Text: "Заявление подано 1 марта, срок рассмотрения 30 дней."},
			{Text: "Ответ был отправлен на десятый рабочий день.", Statements: []string{"абз. 1"}},
		},
		Options: []Option{{Label: "3.1", Text: "Сроки"}, {Label: "3.2", Text: "Отказ"}},
		Answer:  "3.1",
	}
}

// правка собирает ответ редактора из готовых текстов.
func правка(title string, segments map[string]string) editedDraft {
	out := editedDraft{Title: title}
	for id, text := range segments {
		out.Segments = append(out.Segments, struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}{ID: id, Text: text})
	}
	return out
}

func TestЗаслонОтвергаетИзменившиесяЧисла(t *testing.T) {
	// Сроки, доли и количества — то, чего вычитка не касается вовсе.
	// Изменившееся число означает, что редактор правил не язык, и цена
	// такой правки — задача, которая учит другому.
	out, changed, rejected, err := acceptProofread(кУсловию(), правка("", map[string]string{
		"s1": "Заявление подано 1 марта, срок рассмотрения 60 дней.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if out.Segments[0].Text != кУсловию().Segments[0].Text {
		t.Fatalf("правка с изменённым числом прошла: %q", out.Segments[0].Text)
	}
	if len(changed) != 0 {
		t.Fatalf("отклонённое засчиталось правкой: %+v", changed)
	}
	if len(rejected) != 1 || !strings.Contains(rejected[0].Reason, "числа") {
		t.Fatalf("причина отказа не названа: %+v", rejected)
	}
}

func TestПорядокЧиселЗначитНеМеньшеСостава(t *testing.T) {
	// «31 год, доза 10 мг» и «10 лет, доза 31 мг» — разные тексты, хотя
	// числа в них одни и те же. Сравнение множеством пропустило бы второй.
	if numbersIn("подано 1 марта, срок 30 дней") == numbersIn("подано 30 марта, срок 1 день") {
		t.Fatal("перестановка чисел не замечена")
	}
}

func TestЗаслонОтвергаетПерекроенныйФрагмент(t *testing.T) {
	// Вырос почти вдвое — похоже на дописанное обстоятельство; укоротился
	// на треть — на выброшенное. Отличить одно от правки по тексту нельзя,
	// поэтому отвергается и то и другое.
	длинный := кУсловию()
	длинный.Segments[1].Text = "Ответ был отправлен на десятый рабочий день по почте заказным письмом."

	for имя, правки := range map[string]map[string]string{
		"дописанное":  {"s2": "Ответ был отправлен на десятый рабочий день, и заявитель немедленно обжаловал его в вышестоящий орган, приложив копии всех документов, а срок обжалования исчисляется с момента вручения."},
		"выброшенное": {"s2": "Ответ отправлен."},
	} {
		t.Run(имя, func(t *testing.T) {
			out, changed, rejected, err := acceptProofread(длинный, правка("", правки))
			if err != nil {
				t.Fatal(err)
			}
			if out.Segments[1].Text != длинный.Segments[1].Text {
				t.Fatalf("правка прошла: %q", out.Segments[1].Text)
			}
			if len(changed) != 0 || len(rejected) != 1 {
				t.Fatalf("принято %+v, отклонено %+v", changed, rejected)
			}
		})
	}
}

func TestЧужойНомерФрагментаОтбиваетОтветЦеликом(t *testing.T) {
	// Редактор, который путает фрагменты, перекроил условие, а не вычитал
	// его: доверять остальному от него нельзя, и принимается ноль правок,
	// а не всё кроме одной.
	for _, номер := range []string{"s9", "абз. 1", "", "s0"} {
		_, changed, rejected, err := acceptProofread(кУсловию(), правка("Новый заголовок", map[string]string{
			номер: "выдуманный фрагмент",
		}))
		if err == nil {
			t.Fatalf("номер %q прошёл", номер)
		}
		if len(changed) != 0 || len(rejected) != 0 {
			t.Fatalf("при отказе целиком что-то принято: %+v / %+v", changed, rejected)
		}
	}
}

func TestОдинФрагментДваждыОтбиваетОтветЦеликом(t *testing.T) {
	// Какую из двух правок применять — вопрос без ответа, а выбор любой
	// был бы догадкой о том, что редактор имел в виду.
	edited := editedDraft{}
	for _, text := range []string{"первый вариант", "второй вариант"} {
		edited.Segments = append(edited.Segments, struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}{ID: "s1", Text: text})
	}
	if _, _, _, err := acceptProofread(кУсловию(), edited); err == nil {
		t.Fatal("повторённый фрагмент прошёл")
	}
}

func TestНепересказанныйФрагментОстаётсяСоСвоейРазметкой(t *testing.T) {
	// Разметка положений — половина ценности задачи, и редактор её не
	// видит вовсе. Она цела по построению: фрагмент, которого нет в
	// ответе, не трогается.
	out, changed, _, err := acceptProofread(кУсловию(), правка("", map[string]string{
		"s1": "Заявление подано 1 марта; срок рассмотрения 30 дней.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("правка не принята: %+v", changed)
	}
	if len(out.Segments[1].Statements) != 1 || out.Segments[1].Statements[0] != "абз. 1" {
		t.Fatalf("разметка не пережила вычитку: %+v", out.Segments[1])
	}
	if out.Segments[1].Text != кУсловию().Segments[1].Text {
		t.Fatal("нетронутый фрагмент изменился")
	}
}

func TestПустойОтветЗначитЗамечанийНет(t *testing.T) {
	// Законный ответ, а не поломка: редактор возвращает только правленое.
	out, changed, rejected, err := acceptProofread(кУсловию(), editedDraft{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 || len(rejected) != 0 {
		t.Fatalf("из пустого ответа взялись правки: %+v / %+v", changed, rejected)
	}
	if out.Title != кУсловию().Title || out.Segments[0].Text != кУсловию().Segments[0].Text {
		t.Fatal("пустой ответ изменил условие")
	}
}

func TestЗаголовокПравитсяНаравнеСУсловием(t *testing.T) {
	// Рассогласование чаще всего начинается именно с заголовка, и он же
	// попадает в списки составителя.
	out, changed, _, err := acceptProofread(кУсловию(), правка("Срок рассмотрения заявления", nil))
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "Срок рассмотрения заявления" {
		t.Fatalf("заголовок не правился: %q", out.Title)
	}
	if len(changed) != 1 || changed[0].Field != "title" {
		t.Fatalf("правка заголовка не названа: %+v", changed)
	}
}

func TestОтклонённоеЕдетСоставителюЦеликом(t *testing.T) {
	// Отклонённая правка часто верна по сути и не прошла только по
	// заслону. Составитель применит её рукой — если увидит и «было», и
	// «стало», и причину.
	_, _, rejected, err := acceptProofread(кУсловию(), правка("", map[string]string{
		"s1": "Заявление подано 1 марта, срок рассмотрения 60 дней.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(rejected) != 1 {
		t.Fatalf("отклонённое потеряно: %+v", rejected)
	}
	r := rejected[0]
	if r.Before == "" || r.After == "" || r.Reason == "" || r.Field != "s1" {
		t.Fatalf("отклонённая правка не восстановима: %+v", r)
	}
}

func TestЗамечаниеОтличаетНевычитанноеОтВычитанного(t *testing.T) {
	// «Вычитки не было» и «вычитана, замечаний нет» требуют от
	// составителя разного, а слитые в одно дают спокойный вид сотне
	// непроверенных задач подряд.
	не := Proofread{Note: "у поставщика кончились деньги"}
	if remark := не.Remarks(); !strings.Contains(remark, "не вычитано") ||
		!strings.Contains(remark, "кончились деньги") {
		t.Fatalf("несостоявшаяся вычитка молчит: %q", remark)
	}
	чисто := Proofread{Done: true, Changed: []ProofreadChange{{Field: "s1"}}}
	if remark := чисто.Remarks(); remark != "" {
		t.Fatalf("у прошедшей вычитки взялось замечание: %q", remark)
	}
	сОтказом := Proofread{Done: true, Rejected: []ProofreadChange{{Field: "s1", Reason: "изменились числа"}}}
	if remark := сОтказом.Remarks(); !strings.Contains(remark, "s1") ||
		!strings.Contains(remark, "изменились числа") {
		t.Fatalf("отклонённое не названо: %q", remark)
	}
}

func TestPgВычиткаИдётДоСверокИПравитТоЧтоУедетОбучающемуся(t *testing.T) {
	// Порядок узлов — не вкусовщина. Поставь вычитку после сверок, и
	// сверено было бы одно, а показано обучающемуся другое: слепая сверка
	// подтвердила бы условие, которого он не увидит.
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	model := &подставнаяМодель{ответы: []string{
		годныйОтвет,
		`{"title":"","segments":[{"id":"s2","text":"Ответ отправлен на десятый рабочий день по почте."}]}`,
		`{"answer":"3.1","why":"срок назван прямо","sure":true}`,
		ответСоседа,
	}}
	runner, jobs, _ := конвейер(t, model, order)

	result, err := прогнать(t, runner, jobs, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Proofread.Done || len(result.Proofread.Changed) != 1 {
		t.Fatalf("вычитка не состоялась: %+v", result.Proofread)
	}
	// Записан правленый черновик, а не исходный: два текста, из которых
	// один показан, а другой сверен, — это задача, о которой нельзя
	// сказать, какая она.
	if !strings.Contains(result.Draft.Condition(), "по почте") {
		t.Fatalf("в черновик легло невычитанное условие: %q", result.Draft.Condition())
	}
	// И разметка положений при этом цела: редактор её не видит вовсе.
	if len(result.Draft.Segments[1].Statements) != 1 {
		t.Fatalf("разметка не пережила вычитку: %+v", result.Draft.Segments[1])
	}

	сверка := model.спрошено[узелСверки].User
	if !strings.Contains(сверка, "по почте") {
		t.Fatalf("сверка мерила невычитанное условие:\n%s", сверка)
	}
	соседи := model.спрошено[узелСоседей].User
	if !strings.Contains(соседи, "по почте") {
		t.Fatalf("различающая сверка мерила невычитанное условие:\n%s", соседи)
	}
}

func TestPgИтогВычиткиОседаетВЧерновикеАСловаСчитаютсяПриЧтении(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	job, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	draftID, err := jobs.SaveDraft(ctx, job, черновик())
	if err != nil {
		t.Fatal(err)
	}

	было, err := jobs.Drafts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if было[0].Proofread != nil {
		t.Fatal("у невычитанного черновика взялся итог вычитки")
	}

	if err := jobs.SaveProofread(ctx, draftID, Proofread{
		Done:     true,
		Gender:   GenderFemale,
		Rejected: []ProofreadChange{{Field: "s1", Reason: "изменились числа"}},
		Remark:   "слово из прошлой выкатки",
	}); err != nil {
		t.Fatal(err)
	}

	var raw string
	if err := gate.QueryRow(ctx,
		`SELECT proofread::text FROM case_drafts WHERE id = $1`, draftID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "слово из прошлой выкатки") {
		t.Fatalf("слова составителю осели в базе:\n%s", raw)
	}

	стало, err := jobs.Drafts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if стало[0].Proofread == nil {
		t.Fatal("итог вычитки не прочитался обратно")
	}
	if !strings.Contains(стало[0].Proofread.Remark, "изменились числа") {
		t.Fatalf("замечание не посчиталось при чтении: %q", стало[0].Proofread.Remark)
	}
}

func TestPgНесостоявшаясяВычиткаНеРонитЗаданиеИНеМолчит(t *testing.T) {
	// За задачу уже заплачено, и выбрасывать её из-за отказа корректора
	// дороже, чем показать составителю невычитанной. Но «невычитанной»
	// обязано быть видно: молчание он примет за «замечаний нет».
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	model := &подставнаяМодель{ответы: []string{
		годныйОтвет,
		"редактор ответил не тем",
		`{"answer":"3.1","why":"срок назван прямо","sure":true}`,
		ответСоседа,
	}}
	runner, jobs, _ := конвейер(t, model, order)

	result, err := прогнать(t, runner, jobs, sourceID)
	if err != nil {
		t.Fatalf("задание упало из-за несостоявшейся вычитки: %v", err)
	}
	if result.DraftID == 0 {
		t.Fatal("черновик потерян")
	}
	if result.Proofread.Done {
		t.Fatalf("неразобранный ответ засчитан вычиткой: %+v", result.Proofread)
	}
	if result.Proofread.Remarks() == "" {
		t.Fatal("о невычитанном условии не сказано")
	}
	// Условие при этом осталось исходным, а не полуправленым.
	if result.Draft.Condition() != черновикУсловия(годныйОтвет) {
		t.Fatalf("условие изменилось, хотя вычитки не было: %q", result.Draft.Condition())
	}
}

// черновикУсловия — условие из заготовленного ответа модели, чтобы
// сравнивать с ним, не переписывая текст во второй раз.
func черновикУсловия(answer string) string {
	draft, err := ParseDraft(answer)
	if err != nil {
		panic(err)
	}
	return draft.Condition()
}
