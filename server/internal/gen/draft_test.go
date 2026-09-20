package gen

import (
	"encoding/json"
	"strings"
	"testing"
)

// планЗаказа — разрешённый заказ, каким он уходит в задание.
func планЗаказа(kind string) Plan {
	statements := []StatementRef{
		{Designation: "абз. 1", Body: "Срок рассмотрения — десять рабочих дней.", PlaceRef: "с. 4"},
		{Designation: "абз. 2", Body: "Срок продлевается однократно."},
	}
	return Plan{
		SourceID: 1, Slug: "приказ", Title: "Приказ", Kind: "decree",
		UnitWord: "пункт", StatementWord: "указание", Hierarchy: "part-of",
		TaskKind: kind,
		Unit:     UnitRef{Label: "3.1", Title: "Сроки"},
		Siblings: []UnitRef{
			{Label: "3.2", Title: "Отказ", StatementsMd: "Заявление возвращается без рассмотрения."},
			{Label: "3.3", Title: "Передача", StatementsMd: "Заявление передаётся по подведомственности."},
		},
		Statements:   statements,
		StatementsMd: statementsMarkdown(statements),
	}
}

// годнаяПроза — то, что модель возвращает: круга вариантов в ней нет.
func годнаяПроза() Composed {
	return Composed{
		Title: "Срок рассмотрения",
		Segments: []Segment{
			{Text: "Заявление поступило 1 марта.", Statements: nil},
			{Text: "Ответ отправлен на десятый рабочий день.", Statements: []string{"абз. 1"}},
		},
		ExplanationMd: "Срок считается рабочими днями.",
		Difficulty:    3,
	}
}

// годныйЧерновик — та же проза, собранная с кругом сервера.
func годныйЧерновик() Draft {
	set, err := планЗаказа(KindRecognise).AnswerSet()
	if err != nil {
		panic(err)
	}
	return годнаяПроза().Draft(set)
}

func TestЧерновикСнимаетОградуНоНеЧинитОстальное(t *testing.T) {
	// Модели ставят ```json даже там, где схема этого не просит, и ронять
	// из-за обёртки готовую задачу незачем. Всё прочее — отказ.
	composed, err := ParseComposed("```json\n{\"title\":\"Срок\",\"difficulty\":3}\n```")
	if err != nil {
		t.Fatalf("ограда не снята: %v", err)
	}
	if composed.Title != "Срок" {
		t.Fatalf("разобрано не то: %+v", composed)
	}
	if _, err := ParseComposed(`{"title":"Срок","выдумка":1}`); err == nil {
		t.Fatal("незнакомое поле принято: непонятое не применяется")
	}
	if _, err := ParseComposed("  "); err == nil {
		t.Fatal("пустой ответ принят")
	}
}

func TestКругВариантовУМоделиНеСпрашивается(t *testing.T) {
	// Круг собрал сервер и прислал модели готовым. Ответ с вариантами —
	// признак того, что задание и схема разошлись: молча выброшенные,
	// эти варианты значили бы, что мы платим за сочинение выбрасываемого.
	if _, err := ParseComposed(
		`{"title":"Срок","difficulty":3,"options":[{"label":"3.1","text":"Сроки"}],"answer":"3.1"}`,
	); err == nil {
		t.Fatal("варианты от модели приняты: круг собирает сервер")
	}

	raw := ComposedSchema(планЗаказа(KindRecognise))
	for _, gone := range []string{`"options"`, `"answer"`} {
		if strings.Contains(string(raw), gone) {
			t.Fatalf("схема всё ещё просит %s: %s", gone, raw)
		}
	}
}

func TestГодныйЧерновикПроходит(t *testing.T) {
	if err := годнаяПроза().Validate(планЗаказа(KindRecognise)); err != nil {
		t.Fatalf("годная проза отбита: %v", err)
	}
}

func TestСсылкаНаНесуществующееПоложениеОтбивает(t *testing.T) {
	// Молча выброшенная ссылка оставила бы фрагмент без разметки,
	// выглядящий размеченным.
	composed := годнаяПроза()
	composed.Segments[1].Statements = []string{"абз. 9"}
	err := composed.Validate(планЗаказа(KindRecognise))
	if err == nil || !strings.Contains(err.Error(), "абз. 9") {
		t.Fatalf("выдуманная ссылка принята: %v", err)
	}
	if !strings.Contains(err.Error(), "указание") {
		// Отказ читает составитель, и звать положение надо словом
		// источника.
		t.Fatalf("отказ написан не словарём источника: %v", err)
	}
}

func TestБезРазметкиЧерновикНеПринимается(t *testing.T) {
	// Разметка — половина ценности задачи: без неё обучающийся видит
	// вердикт, но не видит, чем он обоснован.
	composed := годнаяПроза()
	composed.Segments[1].Statements = nil
	if err := composed.Validate(планЗаказа(KindRecognise)); err == nil {
		t.Fatal("черновик без разметки принят")
	}
}

func TestОтбиваетсяВсёРазомАНеПоОдному(t *testing.T) {
	// Отбивать черновик по одному замечанию значит гонять модель столько
	// раз, сколько в ответе ошибок, — и платить за каждый заход.
	err := Composed{}.Validate(планЗаказа(KindRecognise))
	if err == nil {
		t.Fatal("пустой черновик принят")
	}
	if strings.Count(err.Error(), ";") < 3 {
		t.Fatalf("названо меньше трёх несоответствий: %v", err)
	}
}

func TestСхемаНеДаётВыдуматьЕдиницуИлиПоложение(t *testing.T) {
	// Схема строится под заказ: перечисленные в ней значения — это
	// обозначения положений именно этого заказа.
	raw := ComposedSchema(планЗаказа(KindRecognise))
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{`"абз. 1"`, `"абз. 2"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("схема не перечисляет %s: %s", want, text)
		}
	}
	if strings.Contains(text, `"абз. 9"`) {
		t.Fatal("в схеме оказалось положение, которого в заказе нет")
	}
}

func TestУсловиеСобираетсяИзФрагментов(t *testing.T) {
	// Отдельного поля с условием нет намеренно: два места для одного
	// текста расходятся молча, и видно это только читающему задачу
	// целиком.
	draft := годныйЧерновик()
	condition := draft.Condition()
	for _, s := range draft.Segments {
		if !strings.Contains(condition, s.Text) {
			t.Fatalf("фрагмент %q потерян в условии %q", s.Text, condition)
		}
	}
}

func TestНеподписанныеПоложенияНумеруютсяНами(t *testing.T) {
	// Модель, выдумавшая нумерацию сама, разметила бы задачу по своему
	// счёту, а не по документу.
	plan := планЗаказа(KindRecognise)
	plan.Statements = []StatementRef{{Body: "Первое"}, {Body: "Второе"}}
	plan.StatementsMd = statementsMarkdown(plan.Statements)
	got := plan.Designations()
	if len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("нумерация не наша: %v", got)
	}
	if !strings.Contains(plan.StatementsMd, "- 1. Первое") {
		// Модель обязана видеть в задании ровно те имена, которыми ей
		// разрешено ссылаться.
		t.Fatalf("в задании имена другие: %q", plan.StatementsMd)
	}
}
