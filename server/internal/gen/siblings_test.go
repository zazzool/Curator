package gen

import (
	"context"
	"strings"
	"testing"
)

// планКруга — заказ узнавания с эталоном 3.1 и двумя соседями.
func планКруга() Plan {
	return Plan{
		TaskKind:      KindRecognise,
		UnitWord:      "пункт",
		StatementWord: "указание",
		Unit:          UnitRef{Label: "3.1", Title: "Сроки"},
		Siblings: []UnitRef{
			{Label: "3.2", Title: "Отказ", StatementsMd: "- Отказ оформляется письменно."},
			{Label: "4", Title: "Прочее", StatementsMd: ""},
		},
	}
}

func кругЗаказа(t *testing.T, plan Plan) AnswerSet {
	t.Helper()
	set, err := plan.AnswerSet()
	if err != nil {
		t.Fatalf("круг не собрался: %v", err)
	}
	return set
}

// круг — черновик задачи-узнавания, собранный из того же круга.
func круг() Draft {
	set, err := планКруга().AnswerSet()
	if err != nil {
		panic(err)
	}
	return Composed{
		Title: "Срок рассмотрения",
		Segments: []Segment{
			{Text: "Заявление подано в понедельник."},
			{Text: "Ответ готовят в общем порядке."},
		},
		Difficulty: 3,
	}.Draft(set)
}

func TestСверяетсяРовноТоЧтоВЧерновике(t *testing.T) {
	// Прежде варианты писала модель, и сверять приходилось ВЫБРАННОЕ ею,
	// а не кандидатов плана: сверенный кандидат, в задачу не попавший, —
	// это проверка задачи, которой никто не увидит. Теперь круг один и
	// тот же, и правило из оговорки стало устройством: то, что уходит
	// обучающемуся, и то, что сверяется, — один список.
	set := кругЗаказа(t, планКруга())
	options, refs := set.Options(), set.Refs()
	if len(options) != len(refs)+1 {
		t.Fatalf("вариантов %d, а сверяется %d", len(options), len(refs))
	}
	for i, ref := range refs {
		if ref.Label != options[i+1].Label {
			t.Fatalf("сверяется %q, а показан %q", ref.Label, options[i+1].Label)
		}
	}
	if got := круг().Options; len(got) != len(options) {
		t.Fatalf("в черновике вариантов %d, а в круге %d", len(got), len(options))
	}
}

func TestЭталонНеСверяетсяСамССобой(t *testing.T) {
	// Эталон условие подтверждает — на то он и эталон. Попади он в
	// соперники, каждая исправная задача получала бы замечание «второй
	// верный ответ», и читать эти замечания перестали бы вовсе.
	for _, r := range кругЗаказа(t, планКруга()).Refs() {
		if strings.EqualFold(r.Label, "3.1") {
			t.Fatal("эталон попал в соперники")
		}
	}
}

func TestВариантБезПоложенийНеВыбрасываетсяИзСверки(t *testing.T) {
	// Выброшенный молча, он неотличим от сверенного и чистого: у задачи
	// три варианта, сверено два, и составитель об этом не узнает.
	нашёлся := false
	for _, r := range кругЗаказа(t, планКруга()).Refs() {
		if r.Label == "4" {
			нашёлся = true
			if r.StatementsMd != "" {
				t.Fatal("у варианта без положений взялись положения")
			}
		}
	}
	if !нашёлся {
		t.Fatal("вариант без положений выброшен из круга сверки")
	}
}

func TestЗадачаДействияТеперьТожеСверяется(t *testing.T) {
	// Прежде не сверялась вовсе: варианты там были текстами от модели, за
	// которыми не стояло ни единицы, ни положения, и различающая сверка
	// пропускала такую задачу целиком — молча и полностью. Теперь вариант
	// действия ЕСТЬ положение документа, и мерится он этим положением.
	plan := планКруга()
	plan.TaskKind = KindAction
	plan.Statements = []StatementRef{
		{Designation: "абз. 1", Body: "Рассмотреть в десятидневный срок."},
		{Designation: "абз. 2", Body: "Продлить срок однократно."},
	}
	plan.Target = &plan.Statements[0]

	set := кругЗаказа(t, plan)
	refs := set.Refs()
	if len(refs) == 0 {
		t.Fatal("у задачи-действия не набралось ни одного сверяемого варианта")
	}
	for i, r := range refs {
		if strings.TrimSpace(r.StatementsMd) == "" {
			t.Fatalf("варианту %q сверять нечем: положение потеряно", r.Label)
		}
		if r.StatementsMd != set.Options()[i+1].Text {
			// Сверяется ровно то, что показано. Возьми сверка всю единицу
			// соседа — подтверждением объявлялось бы совпадение с любым
			// её положением, а показано было одно.
			t.Fatalf("сверяется %q, а показано %q", r.StatementsMd, set.Options()[i+1].Text)
		}
	}
}

func TestНесверенныйВариантГоворитОСебеВслух(t *testing.T) {
	// Молчание составитель примет за «соседи чисты» и отпустит задачу к
	// врачу непроверенной — тот же довод, что у несостоявшейся слепой
	// сверки.
	one := SiblingCheck{
		Label: "3.2", Title: "Отказ",
		Check: Check{Note: "у единицы нет положений — сверить не с чем"},
	}
	note := one.Note()
	if note == "" {
		t.Fatal("несверенный вариант промолчал")
	}
	if !strings.Contains(note, "3.2") || !strings.Contains(note, "Отказ") {
		t.Fatalf("замечание не называет вариант: %q", note)
	}
	if !strings.Contains(note, "нет положений") {
		t.Fatalf("замечание не называет причину: %q", note)
	}
}

func TestВторойВерныйОтветНазванПрямо(t *testing.T) {
	// «Условие подтверждает и соседа» значит, что врач решит задачу
	// правильно и получит «неверно». Это надо сказать словами, а не
	// пометкой в углу.
	one := SiblingCheck{
		Label: "3.2", Title: "Отказ",
		Check:    Check{Done: true, Verdict: &Verdict{Answer: verdictConfirms, Why: "срок назван и там"}},
		Confirms: true,
	}
	note := one.Note()
	if !strings.Contains(note, "второй верный ответ") {
		t.Fatalf("о втором верном ответе не сказано: %q", note)
	}
	if !strings.Contains(note, "срок назван и там") {
		t.Fatalf("довод сверки потерян: %q", note)
	}
}

func TestЧистыйВариантЗамечанияНеРодит(t *testing.T) {
	// Замечание у исправного варианта — это шум, из-за которого перестанут
	// читать замечания вообще.
	for _, verdict := range []string{verdictContradicts, verdictInsufficient} {
		one := SiblingCheck{
			Label: "3.2",
			Check: Check{Done: true, Verdict: &Verdict{Answer: verdict}},
		}
		if note := one.Note(); note != "" {
			t.Fatalf("вердикт %q дал замечание: %q", verdict, note)
		}
	}
}

func TestНедостаточноСказаноЭтоНеВторойОтвет(t *testing.T) {
	// У задачи на различение «в условии недостаточно сказано» про соседа —
	// замысел, а не порок: условие и не обязано его подтверждать. Второй
	// верный ответ — это ТОЛЬКО прямое подтверждение.
	one := SiblingCheck{
		Label: "3.2",
		Check: Check{Done: true, Verdict: &Verdict{Answer: verdictInsufficient}},
	}
	one.Confirms = one.Check.Done && one.Check.Verdict.Answer == verdictConfirms
	if one.Confirms {
		t.Fatal("«недостаточно сказано» засчиталось за второй верный ответ")
	}
}

func TestPgИтогиРазличающейСверкиОседаютВЧерновике(t *testing.T) {
	// Пустой список тоже записывается: «сверяли, и сверять было нечего» —
	// не то же самое, что «не сверяли».
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
	if было[0].Siblings != nil {
		t.Fatal("у несверенного черновика взялись итоги различающей сверки")
	}

	if err := jobs.SaveSiblingChecks(ctx, draftID, nil); err != nil {
		t.Fatal(err)
	}
	пусто, err := jobs.Drafts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if пусто[0].Siblings == nil {
		t.Fatal("«сверять было нечего» записалось как «не сверяли»")
	}
	if len(*пусто[0].Siblings) != 0 {
		t.Fatalf("пустая сверка вернулась с %d записями", len(*пусто[0].Siblings))
	}

	checks := []SiblingCheck{{
		Label: "3.2", Title: "Отказ",
		Check:    Check{Done: true, Verdict: &Verdict{Answer: verdictConfirms, Why: "и там срок"}},
		Confirms: true,
	}}
	if err := jobs.SaveSiblingChecks(ctx, draftID, checks); err != nil {
		t.Fatal(err)
	}
	стало, err := jobs.Drafts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if стало[0].Siblings == nil || len(*стало[0].Siblings) != 1 {
		t.Fatalf("итоги не прочитались обратно: %+v", стало[0].Siblings)
	}
	if !(*стало[0].Siblings)[0].Confirms {
		t.Fatal("подтверждение соседа потерялось при записи")
	}
}

func TestPgИтогиВЧужойЧерновикНеПишутся(t *testing.T) {
	// Ноль изменённых строк при успехе выглядел бы как записанная сверка,
	// которой не существует.
	gate := testGate(t)
	err := NewJobs(gate).SaveSiblingChecks(context.Background(), -1, []SiblingCheck{})
	if err == nil {
		t.Fatal("запись в несуществующий черновик отчиталась успехом")
	}
}

func TestPgРазличающаяСверкаВидитТолькоСвоегоСоседа(t *testing.T) {
	// Слепота у этого узла та же, что у слепой сверки, и проверяется она
	// здесь, а не в pipeline_test.go: там предмет — слепота ПЕРВОЙ сверки,
	// и сложенные в одну проверку две слепоты перестали бы называть, какая
	// именно протекла.
	//
	// Протечь тут есть чему в обе стороны. Уедут положения эталона — и
	// модель ответит «подтверждает» про соседа, потому что прочтёт про
	// срок; скажи ей, какая единица заказана, — и она ответит «не
	// подтверждает» не глядя.
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	model := &подставнаяМодель{ответы: []string{
		годныйОтвет,
		ответВычитки,
		`{"answer":"3.1","why":"срок назван прямо","sure":true}`,
		ответСоседа,
		ответСоседа,
	}}
	runner, jobs, _ := конвейер(t, model, order)

	if _, err := прогнать(t, runner, jobs, sourceID); err != nil {
		t.Fatal(err)
	}
	if len(model.спрошено) != узловВсего {
		t.Fatalf("узлов прошло %d вместо %d", len(model.спрошено), узловВсего)
	}
	сверка := model.спрошено[узелСоседей].System + "\n" + model.спрошено[узелСоседей].User

	// Сверяемое — положения СОСЕДА, и они обязаны быть: без них сверять
	// нечего, и молчаливо пустой запрос вернул бы вердикт ни о чём.
	if !strings.Contains(сверка, "Отказ оформляется письменно") {
		t.Fatalf("в сверку не уехали положения соседа:\n%s", сверка)
	}
	if !strings.Contains(сверка, "Ответ отправлен на десятый рабочий день") {
		t.Fatalf("в сверку не уехало само условие:\n%s", сверка)
	}
	for _, подсказка := range []string{
		"десять рабочих дней", // положение эталона
		"продлевается",        // второе положение эталона
		"заказан",             // какая единица заказана
	} {
		if strings.Contains(сверка, подсказка) {
			t.Fatalf("в различающую сверку уехала подсказка %q:\n%s", подсказка, сверка)
		}
	}
}

func TestPgЗамечаниеСчитаетсяПриЧтенииИНеХранится(t *testing.T) {
	// Слова составителю живут в одном месте — в Note(). Запишись они в
	// базу, и правка формулировки не достала бы черновиков, записанных
	// вчера: составитель читал бы про один порок задачи, а сверка нашла бы
	// другой.
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

	// Слово приходит вместе с итогами — так их отдаёт чтение, и так их
	// вернёт повтор задания.
	if err := jobs.SaveSiblingChecks(ctx, draftID, []SiblingCheck{{
		Label: "3.2", Title: "Отказ",
		Check:    Check{Done: true, Verdict: &Verdict{Answer: verdictConfirms, Why: "и там срок"}},
		Confirms: true,
		Remark:   "слово из прошлой выкатки",
	}}); err != nil {
		t.Fatal(err)
	}

	var raw string
	if err := gate.QueryRow(ctx,
		`SELECT sibling_checks::text FROM case_drafts WHERE id = $1`, draftID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "слово из прошлой выкатки") {
		t.Fatalf("слова составителю осели в базе:\n%s", raw)
	}

	drafts, err := jobs.Drafts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if drafts[0].Siblings == nil || len(*drafts[0].Siblings) != 1 {
		t.Fatalf("итоги не прочитались: %+v", drafts[0].Siblings)
	}
	remark := (*drafts[0].Siblings)[0].Remark
	if !strings.Contains(remark, "второй верный ответ") || !strings.Contains(remark, "3.2 (Отказ)") {
		t.Fatalf("замечание не посчиталось при чтении: %q", remark)
	}
}
