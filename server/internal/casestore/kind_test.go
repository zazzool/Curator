package casestore

import (
	"strings"
	"testing"
)

// Чем сверяется ответ, решает вид задачи.
//
// Проверки здесь без базы намеренно: правило чистое, и прогон его не
// должен зависеть от того, поднята ли база. Прежде оно не было покрыто
// ничем — и разошлось с устройством молча.

func годнаяЗадача(kind string) Case {
	body := Body{
		Title: "Задача",
		Kind:  kind,
		Segments: []Segment{
			{Text: "Больной поступил с жалобами", Statements: []string{"абз. 1"}},
		},
		Explanation: "Разбор",
		Difficulty:  3,
	}
	if kind == KindAction {
		body.Options = []Option{{Text: "Назначить осмотр"}, {Text: "Отложить осмотр"}}
		body.Answer = "Назначить осмотр"
	} else {
		body.Options = []Option{
			{Label: "F20.0", Text: "Параноидная шизофрения"},
			{Label: "F20.1", Text: "Гебефреническая шизофрения"},
		}
		body.Answer = "F20.0"
	}
	return Case{ID: "c-проверка", SourceID: 1, UnitLabel: "F20.0", Body: body}
}

func знакомыеПоложения() map[string]bool {
	return map[string]bool{"абз. 1": true}
}

// сказаноПро отвечает, названа ли беда там, где её ждут.
func сказаноПро(faults Faults, where string) bool {
	for _, one := range faults {
		if one.Where == where {
			return true
		}
	}
	return false
}

func TestУзнаваниеСверяетМетку(t *testing.T) {
	body := годнаяЗадача(KindRecognise).Body
	if _, ok := body.CorrectOption(); !ok {
		t.Fatal("верный вариант не найден по метке, хотя метка совпадает с ответом")
	}

	// Тот самый случай, на котором стороны разошлись: меток нет, ответ
	// совпадает с ТЕКСТОМ варианта. Прежнее правило считало такую задачу
	// годной, сервер её публиковал, а устройство выбрасывало целиком.
	body.Options = []Option{
		{Text: "Параноидная шизофрения"},
		{Text: "Гебефреническая шизофрения"},
	}
	body.Answer = "Параноидная шизофрения"
	if _, ok := body.CorrectOption(); ok {
		t.Error("у узнавания ответ сошёлся с текстом варианта: " +
			"так сервер снова издаст задачу, которой устройство не поймёт")
	}
}

func TestДействиеСверяетТекст(t *testing.T) {
	body := годнаяЗадача(KindAction).Body
	if _, ok := body.CorrectOption(); !ok {
		t.Fatal("верный вариант не найден по тексту действия")
	}

	// Метка у варианта-действия — признак задачи другого вида, и ответ
	// по ней не сверяется: иначе вид задачи перестаёт что-либо решать.
	body.Options = []Option{
		{Label: "F20.0", Text: "Назначить осмотр"},
		{Label: "F20.1", Text: "Отложить осмотр"},
	}
	body.Answer = "F20.0"
	if _, ok := body.CorrectOption(); ok {
		t.Error("у действия ответ сошёлся с меткой варианта")
	}
}

func TestНезнакомыйВидНеСверяетсяНичем(t *testing.T) {
	body := годнаяЗадача(KindRecognise).Body
	body.Kind = "выбор-из-двух"
	if _, ok := body.CorrectOption(); ok {
		t.Error("у задачи незнакомого вида нашёлся верный вариант: " +
			"непонятое не применяется, а отступление к умолчанию снова " +
			"развело бы сервер с устройством")
	}
}

func TestУзнаваниеБезМетокНеПубликуется(t *testing.T) {
	c := годнаяЗадача(KindRecognise)
	c.Body.Options = []Option{
		{Text: "Параноидная шизофрения"},
		{Label: "F20.1", Text: "Гебефреническая шизофрения"},
	}
	faults := CheckPublishable(c, знакомыеПоложения())
	if !сказаноПро(faults, "вариант 1") {
		t.Errorf("про вариант без метки не сказано ничего: %v", faults)
	}
	if strings.Contains(faults.Error(), "вариант 2") {
		t.Errorf("названа беда у варианта с меткой: %v", faults)
	}
}

func TestДействиеСМеткойНеПубликуется(t *testing.T) {
	c := годнаяЗадача(KindAction)
	c.Body.Options[0].Label = "F20.0"
	faults := CheckPublishable(c, знакомыеПоложения())
	if !сказаноПро(faults, "вариант 1") {
		t.Errorf("про вариант-действие с меткой единицы не сказано ничего: %v", faults)
	}
}

func TestВидЗадачиНазываетсяВОтказе(t *testing.T) {
	c := годнаяЗадача(KindRecognise)
	c.Body.Kind = ""
	if faults := CheckPublishable(c, знакомыеПоложения()); !сказаноПро(faults, "вид задачи") {
		t.Errorf("про незаполненный вид не сказано ничего: %v", faults)
	}

	c.Body.Kind = "третий"
	faults := CheckPublishable(c, знакомыеПоложения())
	if !сказаноПро(faults, "вид задачи") {
		t.Fatalf("про незнакомый вид не сказано ничего: %v", faults)
	}
	if !strings.Contains(faults.Error(), "третий") {
		t.Errorf("отказ не называет сам вид, и составителю нечего искать: %v", faults)
	}
}

func TestГоднаяЗадачаПубликуется(t *testing.T) {
	// Иначе все проверки выше зелены оттого, что задача негодна вообще.
	for _, kind := range []string{KindRecognise, KindAction} {
		c := годнаяЗадача(kind)
		if kind == KindAction {
			// У действия метка единицы в условии не при чём, а проверка
			// на подсказку сверяет её с UnitLabel — оставляем тот же.
			c.Body.Answer = "Назначить осмотр"
		}
		if faults := CheckPublishable(c, знакомыеПоложения()); len(faults) > 0 {
			t.Errorf("годная задача вида %q не публикуется: %v", kind, faults)
		}
	}
}
