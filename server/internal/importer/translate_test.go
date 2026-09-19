package importer

import (
	"encoding/json"
	"testing"
)

// Перевод тела задачи проверяется без базы: здесь ничего, кроме перевода,
// и не происходит. Всё, что между двумя базами, — в importer_pg_test.go.

func старое(t *testing.T, one map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(one)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func узнавание(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"id":         "c-1",
		"title":      "Задача",
		"difficulty": 2,
		"targetCode": "F32.1",
		"options":    []string{"F32.1", "F41", "F20"},
		"segments": []map[string]any{
			{"text": "Сниженное настроение две недели", "criterionIds": []string{"cr-1"}},
		},
		"criteria": []map[string]any{
			{"id": "cr-1", "code": "G1", "label": "Первый критерий"},
		},
		"explanationMd": "Разбор",
	}
}

func TestЗадачаУзнаванияПереводитсяЦеликом(t *testing.T) {
	out, err := Translate(старое(t, узнавание(t)))
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.Kind != "recognise" {
		t.Errorf("вид %q", out.Body.Kind)
	}
	if out.Body.Answer != "F32.1" || out.UnitLabel != "F32.1" {
		t.Errorf("ответ %q, единица %q", out.Body.Answer, out.UnitLabel)
	}
	if len(out.Body.Options) != 3 || out.Body.Options[0].Label != "F32.1" {
		t.Errorf("варианты переведены без меток: %+v", out.Body.Options)
	}
	if out.Body.Difficulty != 2 || out.Body.Explanation != "Разбор" {
		t.Errorf("сложность или разбор потеряны: %+v", out.Body)
	}
}

func TestЗадачаДействияУзнаётсяПоЭталону(t *testing.T) {
	// Отдельного поля «вид задачи» в прежней модели не было: два вида
	// различались непустым эталоном-действием, и только им.
	one := узнавание(t)
	one["answer"] = "Начать антидепрессант"
	one["options"] = []string{"Начать антидепрессант", "Наблюдать"}

	out, err := Translate(старое(t, one))
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.Kind != "action" {
		t.Fatalf("вид %q", out.Body.Kind)
	}
	if out.Body.Answer != "Начать антидепрессант" {
		t.Errorf("ответ %q", out.Body.Answer)
	}
	// Метка единицы при этом остаётся прежней: задача написана ПО
	// единице, и метится ею, что бы ни выбирал обучающийся.
	if out.UnitLabel != "F32.1" {
		t.Errorf("метка единицы подменена ответом: %q", out.UnitLabel)
	}
	// У действия метки нет: вариант это текст, а не единица источника.
	if out.Body.Options[0].Label != "" {
		t.Errorf("вариант действия получил метку: %+v", out.Body.Options[0])
	}
}

func TestРазметкаПереводитсяОбозначениемПервоисточника(t *testing.T) {
	// Номер критерия осмыслен только внутри той базы, которой скоро не
	// будет. Обозначение врач найдёт в самих указаниях.
	out, err := Translate(старое(t, узнавание(t)))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Segments) != 1 {
		t.Fatalf("фрагментов %d", len(out.Body.Segments))
	}
	got := out.Body.Segments[0].Statements
	if len(got) != 1 || got[0] != "G1" {
		t.Errorf("разметка %v, а обозначение критерия G1", got)
	}
}

func TestНеподписанныйКритерийБерётсяМеткойСоставителя(t *testing.T) {
	// Выдумывать номер нельзя: выдуманная ссылка на место хуже
	// отсутствующей.
	one := узнавание(t)
	one["criteria"] = []map[string]any{
		{"id": "cr-1", "code": "", "label": "Настроение"},
	}
	out, err := Translate(старое(t, one))
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Body.Segments[0].Statements; len(got) != 1 || got[0] != "Настроение" {
		t.Errorf("разметка %v", got)
	}
}

func TestПрежнееОдиночноеПолеРазметкиНеТеряется(t *testing.T) {
	// Задачи с ним — самые старые, то есть те, которые писались дольше
	// всех.
	one := узнавание(t)
	one["segments"] = []map[string]any{
		{"text": "Сниженное настроение", "criterionId": "cr-1"},
	}
	out, err := Translate(старое(t, one))
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Body.Segments[0].Statements; len(got) != 1 || got[0] != "G1" {
		t.Errorf("разметка старого образца потеряна: %v", got)
	}
}

func TestСсылкаНаНесуществующийКритерийНеРоняетЗадачу(t *testing.T) {
	// Разметка — подсказка, чем фрагмент подтверждается. Потеря одной
	// ссылки не делает задачу нерешаемой; потеря верного ответа — делает.
	one := узнавание(t)
	one["segments"] = []map[string]any{
		{"text": "Сниженное настроение", "criterionIds": []string{"cr-нет"}},
	}
	out, err := Translate(старое(t, one))
	if err != nil {
		t.Fatalf("задача отброшена из-за одной ссылки: %v", err)
	}
	if len(out.Body.Segments[0].Statements) != 0 {
		t.Errorf("ссылка в никуда переведена: %v", out.Body.Segments[0].Statements)
	}
}

func TestЗадачаБезВерногоОтветаНеПереводится(t *testing.T) {
	// Ввезённая наполовину задача выглядит исправной: у неё есть условие
	// и варианты, просто верный ответ оказался не тем. Найти такую после
	// ввоза нельзя ничем, кроме как решив её.
	one := узнавание(t)
	one["options"] = []string{"F41", "F20"}
	if _, err := Translate(старое(t, one)); err == nil {
		t.Error("задача без верного ответа среди вариантов переведена")
	}
}

func TestЗадачаБезМеткиЕдиницыНеПереводится(t *testing.T) {
	one := узнавание(t)
	one["targetCode"] = ""
	if _, err := Translate(старое(t, one)); err == nil {
		t.Error("задача без метки единицы переведена")
	}
}

func TestЗадачаБезУсловияНеПереводится(t *testing.T) {
	one := узнавание(t)
	one["segments"] = []map[string]any{}
	if _, err := Translate(старое(t, one)); err == nil {
		t.Error("задача без условия переведена")
	}
}

func TestЗадачаСОднимВариантомНеПереводится(t *testing.T) {
	one := узнавание(t)
	one["options"] = []string{"F32.1"}
	if _, err := Translate(старое(t, one)); err == nil {
		t.Error("один вариант принят за выбор")
	}
}

func TestНеразобранноеТелоОтказывает(t *testing.T) {
	if _, err := Translate([]byte("совсем не json")); err == nil {
		t.Error("неразобранное тело переведено")
	}
}

func TestСвойстваИсточникаОбязательныИЗакрыты(t *testing.T) {
	// Словарь закрыт: значение, появившееся строкой по месту, прошло бы
	// мимо всех проверок и осело бы в колонке, которую читает подбор.
	if err := (Profile{}).Valid(); err == nil {
		t.Error("пустые свойства приняты")
	}
	bad := Profile{Purpose: "тема", Hierarchy: "is-a", Completeness: "complete"}
	if err := bad.Valid(); err == nil {
		t.Error("неизвестная ось классификации принята")
	}
	good := Profile{Purpose: "topic", Hierarchy: "is-a", Completeness: "complete"}
	if err := good.Valid(); err != nil {
		t.Errorf("исправные свойства отвергнуты: %v", err)
	}
}

func TestПутьСчитаетсяИзСвязейАНеИзФорматаМетки(t *testing.T) {
	// Для МКБ отрезание знаков от кода сработало бы — и ровно для него
	// одного. Здесь метки нарочно не похожи на коды: путь всё равно
	// обязан сойтись.
	list := []unit{
		{label: "раздел-1", parent: ""},
		{label: "глава-7", parent: "раздел-1"},
		{label: "пункт-12", parent: "глава-7"},
	}
	paths := Paths(list)
	if paths["пункт-12"] != "раздел-1/глава-7/пункт-12" {
		t.Errorf("путь %q", paths["пункт-12"])
	}
	if depthOf(paths["пункт-12"]) != 3 {
		t.Errorf("глубина %d", depthOf(paths["пункт-12"]))
	}
}

func TestМеткаСРазделителемПутиНеПолучает(t *testing.T) {
	// Молча испорченный путь хуже отсутствующего: задача уехала бы в
	// чужой раздел, и заметить это можно было бы только открыв раздел.
	paths := Paths([]unit{{label: "A/B", parent: ""}})
	if _, ok := paths["A/B"]; ok {
		t.Error("метка с разделителем получила путь")
	}
}

func TestЦиклВРодителяхНеУходитВБесконечность(t *testing.T) {
	// Данные чужие, и доверять их связности нельзя.
	paths := Paths([]unit{
		{label: "A", parent: "B"},
		{label: "B", parent: "A"},
	})
	if len(paths) != 0 {
		t.Errorf("зацикленная ветка получила путь: %v", paths)
	}
}

func TestНазванныйНоОтсутствующийРодительВПутьВходит(t *testing.T) {
	// Выгрузка может не содержать корень, и это не отказ. Названный
	// родитель при этом в путь ВХОДИТ, хотя своей строки у него нет:
	// выкинуть его значит поднять единицу в корень и соврать о дереве —
	// срез «всё, что под F3» перестал бы её находить.
	paths := Paths([]unit{{label: "F32", parent: "F3"}})
	if paths["F32"] != "F3/F32" {
		t.Errorf("путь %q, а родителем названа F3", paths["F32"])
	}
}

func TestЕдиницаБезРодителяЛежитВКорне(t *testing.T) {
	paths := Paths([]unit{{label: "F3", parent: ""}})
	if paths["F3"] != "F3" {
		t.Errorf("путь %q", paths["F3"])
	}
}
