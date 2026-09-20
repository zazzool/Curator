package gen

import (
	"fmt"
	"strings"
	"testing"
)

// планДействия — заказ действия: эталон первое положение, у соседей есть
// свои.
func планДействия() Plan {
	plan := планЗаказа(KindAction)
	plan.Target = &plan.Statements[0]
	return plan
}

func TestЭталонСтоитВКругеПервым(t *testing.T) {
	// Порядок читают обе сверки и разбор задачи; тасует варианты
	// устройство при выдаче. Перетасуй мы их здесь — ссылки разбора на
	// варианты разъехались бы с самими вариантами.
	for _, kind := range []string{KindRecognise, KindAction} {
		plan := планЗаказа(kind)
		if kind == KindAction {
			plan.Target = &plan.Statements[0]
		}
		set, err := plan.AnswerSet()
		if err != nil {
			t.Fatalf("%s: круг не собрался: %v", KindWord(kind), err)
		}
		first := set.Options()[0]
		switch kind {
		case KindRecognise:
			if first.Label != plan.Unit.Label {
				t.Fatalf("первым вариантом стоит %q, а эталон %q", first.Label, plan.Unit.Label)
			}
		default:
			if first.Text != plan.Target.Body {
				t.Fatalf("первым вариантом стоит %q, а эталон %q", first.Text, plan.Target.Body)
			}
		}
		if set.Answer != первыйОтвет(plan) {
			t.Fatalf("%s: ответ круга %q", KindWord(kind), set.Answer)
		}
	}
}

func первыйОтвет(plan Plan) string {
	if plan.TaskKind == KindAction {
		return plan.Target.Body
	}
	return plan.Unit.Label
}

func TestНеверныеДействияБерутсяИзДокументаАНеСочиняются(t *testing.T) {
	// Выдуманное действие узнаётся обучающимся с первого взгляда: такая
	// задача мерит чутьё на неестественную формулировку, а не знание
	// документа. Поэтому сперва соседние положения той же единицы, потом
	// головные положения соседей.
	set, err := планДействия().AnswerSet()
	if err != nil {
		t.Fatal(err)
	}
	известные := map[string]bool{
		"Срок продлевается однократно.":               true,
		"Заявление возвращается без рассмотрения.":    true,
		"Заявление передаётся по подведомственности.": true,
	}
	if len(set.Rivals) != len(известные) {
		t.Fatalf("неверных действий %d: %+v", len(set.Rivals), set.Rivals)
	}
	for _, r := range set.Rivals {
		if !известные[r.Option.Text] {
			t.Fatalf("вариант %q в документе не найден", r.Option.Text)
		}
	}
	// Своё положение идёт раньше соседского: оно написано про тот же
	// предмет и потому правдоподобнее.
	if set.Rivals[0].Option.Text != "Срок продлевается однократно." {
		t.Fatalf("первым неверным стоит %q, а не соседнее положение своей единицы",
			set.Rivals[0].Option.Text)
	}
}

func TestЭталонНеПопадаетВНеверныеДажеСловоВСлово(t *testing.T) {
	// Положение, повторённое в документе дважды, дало бы задачу с двумя
	// верными ответами — и слепая сверка объявила бы её сошедшейся.
	plan := планДействия()
	plan.Statements = append(plan.Statements, StatementRef{
		Designation: "абз. 3", Body: plan.Statements[0].Body,
	})
	plan.StatementsMd = statementsMarkdown(plan.Statements)
	plan.Target = &plan.Statements[0]

	set, err := plan.AnswerSet()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range set.Rivals {
		if strings.EqualFold(r.Option.Text, set.Answer) {
			t.Fatalf("эталон попал в неверные вариантом %q", r.Option.Text)
		}
	}
}

func TestКругНеРастётШестиНеверных(t *testing.T) {
	// Круг шире семи вариантов перестаёт читаться, а варианты за
	// пределами первой пятёрки выбирают единицы.
	plan := планЗаказа(KindRecognise)
	plan.Siblings = nil
	for i := 0; i < 20; i++ {
		plan.Siblings = append(plan.Siblings, UnitRef{
			Label: fmt.Sprintf("3.%d", i+10),
			Title: fmt.Sprintf("Сосед %d", i),
		})
	}
	set, err := plan.AnswerSet()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Rivals) != rivalsMax {
		t.Fatalf("неверных вариантов %d вместо %d", len(set.Rivals), rivalsMax)
	}
}

func TestНазваннаяПараНеВылетаетПриОбрезке(t *testing.T) {
	// Пара «путают с» — единственное место, где сходство двух единиц
	// назвал САМ источник. Обрежь круг порядком документа — и первой
	// вылетела бы ровно она.
	plan := планЗаказа(KindRecognise)
	plan.Siblings = nil
	for i := 0; i < rivalsMax+3; i++ {
		plan.Siblings = append(plan.Siblings, UnitRef{
			Label: fmt.Sprintf("3.%d", i+10),
			Title: fmt.Sprintf("Сосед %d", i),
		})
	}
	// Названная пара стоит в документе последней — то есть ровно там,
	// откуда обрезка её и выбросила бы.
	plan.Siblings = append(plan.Siblings, UnitRef{
		Label: "9.9", Title: "Путаемый", Confusable: true,
	})

	set, err := plan.AnswerSet()
	if err != nil {
		t.Fatal(err)
	}
	нашлась := false
	for _, r := range set.Rivals {
		if r.Option.Label == "9.9" {
			нашлась = true
		}
	}
	if !нашлась {
		t.Fatalf("названная пара вылетела при обрезке: %+v", set.Rivals)
	}
}

func TestИсточникБезКругаОтказываетДоМодели(t *testing.T) {
	// Отказ приходит ДО обращения к модели — то есть бесплатно, — и
	// называет настоящую причину: круга не даёт сам источник. Прежде это
	// узнавалось после того, как за задачу заплачено, и выглядело как
	// «модель написала плохо».
	plan := планЗаказа(KindRecognise)
	plan.Siblings = []UnitRef{{Label: "3.2", Title: "Отказ"}}
	_, err := plan.AnswerSet()
	if err == nil {
		t.Fatal("круг из двух вариантов принят: это не выбор, а подсказка")
	}
	if !strings.Contains(err.Error(), "3.1") {
		// Отказ читает составитель, и починить он может только названное.
		t.Fatalf("в отказе не названа единица заказа: %v", err)
	}

	действие := планДействия()
	действие.Statements = действие.Statements[:1]
	действие.Target = &действие.Statements[0]
	действие.Siblings = nil
	if _, err := действие.AnswerSet(); err == nil {
		t.Fatal("задача-действие без неверных действий принята")
	}
}

func TestПовторовВКругеНеБывает(t *testing.T) {
	// Два одинаковых варианта сокращают выбор молча: обучающийся видит
	// четыре строки, а выбирает из трёх. У действия это не выдумка —
	// одно и то же предписание стоит и в единице, и у соседа.
	plan := планДействия()
	plan.Siblings = []UnitRef{
		{Label: "3.2", Title: "Отказ", StatementsMd: "Срок продлевается однократно."},
		{Label: "3.3", Title: "Передача", StatementsMd: "Заявление передаётся по подведомственности."},
	}
	set, err := plan.AnswerSet()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, o := range set.Options() {
		key := strings.ToLower(o.Text)
		if seen[key] {
			t.Fatalf("вариант %q повторяется", o.Text)
		}
		seen[key] = true
	}
}

func TestОткудаВзятВариантУезжаетОтдельноОтСамогоВарианта(t *testing.T) {
	// Модель перечисляет варианты обучающемуся дословно. Припиши мы
	// происхождение к самому варианту — «(соседнее указание той же
	// единицы)» уехало бы в задачу строкой ответа.
	set, err := планДействия().AnswerSet()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(set.Listing(), "рядом с эталоном") {
		t.Fatalf("происхождение попало в сам круг:\n%s", set.Listing())
	}
	if !strings.Contains(set.Origins(), "рядом с эталоном") {
		t.Fatalf("происхождение не названо вовсе:\n%s", set.Origins())
	}
}

func TestСлепомуЗаданиюКругНеПоказывается(t *testing.T) {
	// Слепота держится тем, что круг подставляет ДРУГАЯ функция: чтобы
	// показать его сверке, пришлось бы позвать RenderSet, а не забыть
	// оговорку.
	plan := планЗаказа(KindRecognise)
	set, err := plan.AnswerSet()
	if err != nil {
		t.Fatal(err)
	}
	if got := Render("{круг}", plan.Blinded()); strings.Contains(got, "3.2") {
		t.Fatalf("круг уехал в слепое задание: %q", got)
	}
	if got := RenderSet("{круг}", plan, set); !strings.Contains(got, "3.2") {
		t.Fatalf("круг не уехал в задание написания: %q", got)
	}
}
