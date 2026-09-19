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
		TaskKind:     kind,
		Unit:         UnitRef{Label: "3.1", Title: "Сроки"},
		Siblings:     []UnitRef{{Label: "3.2", Title: "Отказ"}},
		Statements:   statements,
		StatementsMd: statementsMarkdown(statements),
	}
}

func годныйЧерновик() Draft {
	return Draft{
		Title: "Срок рассмотрения",
		Segments: []Segment{
			{Text: "Заявление поступило 1 марта.", Statements: nil},
			{Text: "Ответ отправлен на десятый рабочий день.", Statements: []string{"абз. 1"}},
		},
		Options: []Option{
			{Label: "3.1", Text: "Сроки"},
			{Label: "3.2", Text: "Отказ"},
			{Label: "3.1", Text: "Сроки исчисляются иначе"},
		},
		Answer:        "3.1",
		ExplanationMd: "Срок считается рабочими днями.",
		Difficulty:    3,
	}
}

func TestЧерновикСнимаетОградуНоНеЧинитОстальное(t *testing.T) {
	// Модели ставят ```json даже там, где схема этого не просит, и ронять
	// из-за обёртки готовую задачу незачем. Всё прочее — отказ.
	draft, err := ParseDraft("```json\n{\"title\":\"Срок\",\"difficulty\":3}\n```")
	if err != nil {
		t.Fatalf("ограда не снята: %v", err)
	}
	if draft.Title != "Срок" {
		t.Fatalf("разобрано не то: %+v", draft)
	}
	if _, err := ParseDraft(`{"title":"Срок","выдумка":1}`); err == nil {
		t.Fatal("незнакомое поле принято: непонятое не применяется")
	}
	if _, err := ParseDraft("  "); err == nil {
		t.Fatal("пустой ответ принят")
	}
}

func TestГодныйЧерновикПроходит(t *testing.T) {
	if err := годныйЧерновик().Validate(планЗаказа(KindRecognise)); err != nil {
		t.Fatalf("годный черновик отбит: %v", err)
	}
}

func TestСсылкаНаНесуществующееПоложениеОтбивает(t *testing.T) {
	// Молча выброшенная ссылка оставила бы фрагмент без разметки,
	// выглядящий размеченным.
	draft := годныйЧерновик()
	draft.Segments[1].Statements = []string{"абз. 9"}
	err := draft.Validate(планЗаказа(KindRecognise))
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
	draft := годныйЧерновик()
	draft.Segments[1].Statements = nil
	if err := draft.Validate(планЗаказа(KindRecognise)); err == nil {
		t.Fatal("черновик без разметки принят")
	}
}

func TestВерныйОтветОбязанБытьЗаказанным(t *testing.T) {
	// Эталон выбирает заказ, а не модель: задача, ответившая другой
	// единицей, отвечает не на тот вопрос, который заказывали.
	draft := годныйЧерновик()
	draft.Answer = "3.2"
	err := draft.Validate(планЗаказа(KindRecognise))
	if err == nil || !strings.Contains(err.Error(), "заказан") {
		t.Fatalf("подмена эталона принята: %v", err)
	}
}

func TestВариантСоСторонойНеПринимается(t *testing.T) {
	// Круг различения собран при заказе из данных источника. Единица со
	// стороны — либо выдумка, либо сосед, которого источник соседом не
	// считает.
	draft := годныйЧерновик()
	draft.Options[1] = Option{Label: "9.9", Text: "Чужое"}
	err := draft.Validate(планЗаказа(KindRecognise))
	if err == nil || !strings.Contains(err.Error(), "круга различения") {
		t.Fatalf("вариант со стороны принят: %v", err)
	}
}

func TestПовторВариантаСокращаетВыборМолча(t *testing.T) {
	draft := годныйЧерновик()
	draft.Options[2] = Option{Label: "3.2", Text: "Отказ"}
	err := draft.Validate(планЗаказа(KindRecognise))
	if err == nil || !strings.Contains(err.Error(), "повторяется") {
		t.Fatalf("повтор варианта принят: %v", err)
	}
}

func TestЗадачаДействияМеряетсяИначе(t *testing.T) {
	plan := планЗаказа(KindAction)
	plan.Target = &plan.Statements[0]

	draft := Draft{
		Title: "Что сделать",
		Segments: []Segment{
			{Text: "Заявление поступило.", Statements: []string{"абз. 1"}},
		},
		Options: []Option{
			{Text: "Рассмотреть в десятидневный срок"},
			{Text: "Вернуть без рассмотрения"},
			{Text: "Передать в другой орган"},
		},
		Answer:        "Рассмотреть в десятидневный срок",
		ExplanationMd: "Срок считается рабочими днями.",
		Difficulty:    2,
	}
	if err := draft.Validate(plan); err != nil {
		t.Fatalf("годная задача-действие отбита: %v", err)
	}

	// Метка единицы у варианта-действия — признак того, что модель
	// написала задачу другого вида.
	draft.Options[0].Label = "3.1"
	if err := draft.Validate(plan); err == nil {
		t.Fatal("вариант-действие с меткой единицы принят")
	}
}

func TestОтбиваетсяВсёРазомАНеПоОдному(t *testing.T) {
	// Отбивать черновик по одному замечанию значит гонять модель столько
	// раз, сколько в ответе ошибок, — и платить за каждый заход.
	draft := Draft{}
	err := draft.Validate(планЗаказа(KindRecognise))
	if err == nil {
		t.Fatal("пустой черновик принят")
	}
	if strings.Count(err.Error(), ";") < 3 {
		t.Fatalf("названо меньше трёх несоответствий: %v", err)
	}
}

func TestСхемаНеДаётВыдуматьЕдиницуИлиПоложение(t *testing.T) {
	// Схема строится под заказ: перечисленные в ней значения — это круг
	// различения и обозначения положений именно этого заказа.
	raw := DraftSchema(планЗаказа(KindRecognise))
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{`"3.1"`, `"3.2"`, `"абз. 1"`, `"абз. 2"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("схема не перечисляет %s: %s", want, text)
		}
	}
	if strings.Contains(text, `"3.3"`) {
		t.Fatal("в схеме оказалась единица, которой в заказе нет")
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
