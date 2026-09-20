package gen

import (
	"context"
	"strings"
	"testing"
)

// круг — черновик задачи-узнавания с эталоном 3.1 и двумя соперниками.
func круг() Draft {
	return Draft{
		Title: "Срок рассмотрения",
		Segments: []Segment{
			{Text: "Заявление подано в понедельник."},
			{Text: "Ответ готовят в общем порядке."},
		},
		Options: []Option{
			{Label: "3.1", Text: "Десять рабочих дней"},
			{Label: "3.2", Text: "Отказ письменно"},
			{Label: "4", Text: "Прочее"},
		},
		Answer:     "3.1",
		Difficulty: 3,
	}
}

func планКруга() Plan {
	return Plan{
		TaskKind:      KindRecognise,
		StatementWord: "указание",
		Unit:          UnitRef{Label: "3.1", Title: "Сроки"},
		Siblings: []UnitRef{
			{Label: "3.2", Title: "Отказ", StatementsMd: "- Отказ оформляется письменно."},
			{Label: "4", Title: "Прочее", StatementsMd: ""},
		},
	}
}

func TestСверяютсяВариантыЧерновикаАНеКандидатыПлана(t *testing.T) {
	// Круг плана — это кандидаты, и модель выбрала из них не обязательно
	// всех. Сверив кандидатов, мы проверили бы задачу, которой никто не
	// увидит.
	plan := планКруга()
	plan.Siblings = append(plan.Siblings, UnitRef{
		Label: "3.9", Title: "Невзятый", StatementsMd: "- Что-то ещё.",
	})
	rivals := круг().Rivals(plan)
	for _, r := range rivals {
		if r.Label == "3.9" {
			t.Fatal("сверяется кандидат, которого модель в задачу не взяла")
		}
	}
	if len(rivals) != 2 {
		t.Fatalf("соперников %d, ждали два", len(rivals))
	}
}

func TestЭталонНеСверяетсяСамССобой(t *testing.T) {
	// Эталон условие подтверждает — на то он и эталон. Попади он в
	// соперники, каждая исправная задача получала бы замечание «второй
	// верный ответ», и читать эти замечания перестали бы вовсе.
	for _, r := range круг().Rivals(планКруга()) {
		if strings.EqualFold(r.Label, "3.1") {
			t.Fatal("эталон попал в соперники")
		}
	}
}

func TestВариантБезПоложенийНеВыбрасываетсяИзСверки(t *testing.T) {
	// Выброшенный молча, он неотличим от сверенного и чистого: у задачи
	// три варианта, сверено два, и составитель об этом не узнает.
	rivals := круг().Rivals(планКруга())
	нашёлся := false
	for _, r := range rivals {
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

func TestУЗадачиДействияСверятьНечего(t *testing.T) {
	// У действия вариант — это текст, и единицы за ним нет: сверять его
	// против положений соседа нечем. Это разные предметы, а не пробел.
	plan := планКруга()
	plan.TaskKind = KindAction
	draft := круг()
	draft.Options = []Option{
		{Text: "Продлить срок"}, {Text: "Отказать"}, {Text: "Запросить документы"},
	}
	draft.Answer = "Продлить срок"
	if got := draft.Rivals(plan); len(got) != 0 {
		t.Fatalf("у задачи-действия набралось %d соперников", len(got))
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
