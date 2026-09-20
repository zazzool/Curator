package gen

import (
	"context"
	"testing"
)

func TestСостоявшаясяИНесостоявшаясяСверкаЭтоРазное(t *testing.T) {
	// Три состояния, и все три требуют разного. Сверки не было вовсе —
	// задачу не смотрел никто. Сверка не состоялась — попытка была, и
	// причина названа. Сверка прошла — сошлась или нет. Смешай первые два
	// с третьим, и непроверенная задача покажется чистой.
	не := Check{}
	if не.Agrees() {
		t.Fatal("несостоявшаяся сверка объявила согласие")
	}
	сорвалась := Check{Note: "у поставщика кончились деньги"}
	if сорвалась.Agrees() {
		t.Fatal("сорвавшаяся сверка объявила согласие")
	}
	if сорвалась.Done {
		t.Fatal("сорвавшаяся сверка объявила себя состоявшейся")
	}
	несогласна := Check{Done: true, Verdict: &Verdict{Answer: "3.2", Agrees: false}}
	if несогласна.Agrees() {
		t.Fatal("несогласие прочиталось как согласие")
	}
	сошлась := Check{Done: true, Verdict: &Verdict{Answer: "3.1", Agrees: true}}
	if !сошлась.Agrees() {
		t.Fatal("согласие прочиталось как несогласие")
	}
}

func TestСверкаОбъявленнаяПрошедшейБезОтветаНеСогласие(t *testing.T) {
	// Такое приезжает из черновика, записанного прежней выкаткой: пометка
	// «прошла» есть, ответа нет. Непонятое не применяется — и уж точно не
	// выдаётся за согласие: согласие отпускает задачу к врачу.
	пустая := Check{Done: true}
	if пустая.Agrees() {
		t.Fatal("сверка без ответа выдала согласие")
	}
}

// черновик — любая задача: проверки этого файла про то, что рядом с
// черновиком оседает, а не про сам черновик.
func черновик() Draft {
	return Draft{Title: "Задача для проверки", Difficulty: 3}
}

func TestPgИтогСверкиОседаетВЧерновике(t *testing.T) {
	// До этой записи вердикт вычислялся и ПРОПАДАЛ: за сверку платили, а
	// составитель её не видел, и задача, с которой сверка не согласилась,
	// выглядела ровно как та, с которой согласилась.
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

	// Свежий черновик сверки не знает, и это именно «не было», а не «не
	// сошлась».
	было, err := jobs.Drafts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(было) != 1 {
		t.Fatalf("черновиков %d, ждали один", len(было))
	}
	if было[0].Check != nil {
		t.Fatalf("у несверенного черновика взялся итог сверки: %+v", было[0].Check)
	}

	check := Check{Done: true, Verdict: &Verdict{Answer: "3.2", Why: "срок иной", Agrees: false}}
	if err := jobs.SaveCheck(ctx, draftID, check); err != nil {
		t.Fatal(err)
	}
	стало, err := jobs.Drafts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if стало[0].Check == nil {
		t.Fatal("записанный итог сверки не прочитался обратно")
	}
	if стало[0].Check.Agrees() {
		t.Fatal("несогласие сверки прочиталось как согласие")
	}
	if стало[0].Check.Verdict == nil || стало[0].Check.Verdict.Answer != "3.2" {
		t.Fatalf("ответ сверки потерян: %+v", стало[0].Check)
	}
}

func TestPgПричинаНесостоявшейсяСверкиЗаписываетсяАНеТеряется(t *testing.T) {
	// Кончились деньги у поставщика, отменили задание, модель вернула не
	// тот JSON — на глаз это одинаково пустой разбор у сотни задач подряд,
	// а делать надо разное.
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
	if err := jobs.SaveCheck(ctx, draftID, Check{Note: "ответ сверки не разобран"}); err != nil {
		t.Fatal(err)
	}
	drafts, err := jobs.Drafts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if drafts[0].Check == nil {
		t.Fatal("итог сверки не прочитался")
	}
	if drafts[0].Check.Done {
		t.Fatal("сорвавшаяся сверка записалась состоявшейся")
	}
	if drafts[0].Check.Note == "" {
		t.Fatal("причина сорвавшейся сверки потеряна: пустая причина ничем не лучше её отсутствия")
	}
}

func TestPgИтогСверкиВЧужойЧерновикНеПишется(t *testing.T) {
	// Ноль изменённых строк при успехе выглядел бы как записанная сверка,
	// которой не существует, — и задача уехала бы к врачу с чужим вердиктом
	// или вовсе без него.
	gate := testGate(t)
	if err := NewJobs(gate).SaveCheck(context.Background(), -1, Check{Done: true}); err == nil {
		t.Fatal("запись итога сверки в несуществующий черновик отчиталась успехом")
	}
}

func TestPgОтказавшееЗаданиеНеЗапираетЕдиницуНавсегда(t *testing.T) {
	// Ключ повторности защищал от двойного нажатия и заодно запирал
	// единицу насовсем: задание отказало — ключ остался, и повторный заказ
	// той же единицы молча возвращал прежнее, закрытое задание. Для
	// составителя это выглядело как «нажал и ничего не произошло».
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	first, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	// Пока задание идёт, второй заказ по-прежнему не заводит второго: от
	// двойного нажатия защита осталась ровно там же, где была.
	same, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	if same.ID != first.ID {
		t.Fatalf("двойное нажатие завело два задания: %d и %d", first.ID, same.ID)
	}

	if err := jobs.Fail(ctx, first.ID, "у поставщика кончились деньги"); err != nil {
		t.Fatal(err)
	}
	again, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID == first.ID {
		t.Fatal("после отказа повторный заказ вернул прежнее закрытое задание: единица заперта")
	}
	if again.Status != "queued" {
		t.Fatalf("повторный заказ встал не в очередь, а в %q", again.Status)
	}
	// Отказ прежнего цел: он единственный след того, почему задача не
	// написалась, и оживление прежнего задания стёрло бы его.
	was, err := jobs.Job(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if was.Status != "failed" || was.Error == "" {
		t.Fatalf("отказ прежнего задания затёрт: %q / %q", was.Status, was.Error)
	}
}

func TestPgПовторБерётПланПрежнегоЗаданияИНеТрогаетЕго(t *testing.T) {
	// План берётся у прежнего, а не разрешается заново: повторяют то, что
	// отказало по дороге, а не то, что стало невыполнимым. Разреши мы
	// заказ заново — повтор отказывал бы всякий раз, когда соседнюю
	// единицу успели поправить.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	prev, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Fail(ctx, prev.ID, "модель вернула не тот JSON"); err != nil {
		t.Fatal(err)
	}
	prev, err = jobs.Job(ctx, prev.ID)
	if err != nil {
		t.Fatal(err)
	}

	next, err := jobs.Retry(ctx, prev)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == prev.ID {
		t.Fatal("повтор оживил прежнее задание вместо нового")
	}
	if next.Plan.UnitWord != plan.UnitWord || next.Plan.Unit.Label != plan.Unit.Label {
		t.Fatalf("повтор потерял план прежнего задания: %+v", next.Plan)
	}
	if len(next.Plan.Siblings) != len(plan.Siblings) {
		t.Fatalf("круг различения у повтора другой: %d против %d",
			len(next.Plan.Siblings), len(plan.Siblings))
	}
}

func TestPgИдущаяРаботаПоЕдиницеВидна(t *testing.T) {
	// У повтора ключа повторности нет — от двойного нажатия защищает
	// именно это: два задания на одну единицу пишут две задачи, и
	// заплачено будет за обе.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	jobs := NewJobs(gate)

	if _, busy, err := jobs.RunningFor(ctx, sourceID, "3.1"); err != nil {
		t.Fatal(err)
	} else if busy {
		t.Fatal("по нетронутой единице нашлась идущая работа")
	}

	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	id, busy, err := jobs.RunningFor(ctx, sourceID, "3.1")
	if err != nil {
		t.Fatal(err)
	}
	if !busy || id != job.ID {
		t.Fatalf("идущее задание не найдено: %d, %v", id, busy)
	}
	// Соседняя единица при этом свободна: заслон про единицу, а не про
	// источник целиком — иначе один заказ запирал бы весь приказ.
	if _, busy, err := jobs.RunningFor(ctx, sourceID, "3.2"); err != nil {
		t.Fatal(err)
	} else if busy {
		t.Fatal("заказ по одной единице запер соседнюю")
	}

	if err := jobs.Done(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, busy, err := jobs.RunningFor(ctx, sourceID, "3.1"); err != nil {
		t.Fatal(err)
	} else if busy {
		t.Fatal("закрытое задание считается идущим")
	}
}
