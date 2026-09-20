package rules

import (
	"strings"
	"testing"
	"time"
)

// живое — действующее правило для проверок уплотнения. Строится тем же
// «правило», что и всюду в пакете: вторая заготовка разошлась бы с
// первой молча, и проверки уплотнения мерили бы не тот свод, который
// уходит в задание.
func живое(id string, src Source, kind Kind, text string) Rule {
	return правило(id, func(r *Rule) {
		r.Text = text
		r.Kind = kind
		r.Source = src
		r.Status = Active
		r.Pinned = true
		r.ValidFrom = сейчас.Add(-time.Hour)
		r.LastSeenAt = сейчас
	})
}

func свод(list ...Rule) Book { return NewBook(list, сейчас) }

func группа(keep string, merge []string, text string) CompactionGroup {
	return CompactionGroup{KeepID: keep, MergeIDs: merge, Text: text, Why: "оба про одно"}
}

func TestПравилоСоставителяНеСливаетсяВЧужое(t *testing.T) {
	// Формулировка составителя — требование, а не заготовка для
	// пересказа. Позволь её слить, и модель однажды перепишет требование
	// человека во что-то другое, а заметить это будет негде.
	book := свод(
		живое("lint:1", FromLint, KindLanguage, "Не пиши канцелярских оборотов в условии задачи."),
		живое("curator:1", FromCurator, KindLanguage, "Пиши условие простыми предложениями."),
	)
	plan := Sanitize([]CompactionGroup{
		группа("lint:1", []string{"curator:1"}, "Пиши просто, без канцелярита."),
	}, book)
	if len(plan.Groups) != 0 {
		t.Fatalf("правило составителя слито в выведенное: %+v", plan.Groups)
	}

	// А выжившим оно быть может: слияние в обратную сторону законно.
	plan = Sanitize([]CompactionGroup{
		группа("curator:1", []string{"lint:1"}, "Пиши просто, без канцелярита."),
	}, book)
	if len(plan.Groups) != 1 {
		t.Fatalf("слияние В правило составителя отклонено: %+v", plan)
	}
}

func TestПравилоСПроверкойНеСливается(t *testing.T) {
	// Предикат привязан к своей формулировке. Слей его носителя — и
	// проверка уедет в закрытые, то есть перестанет срабатывать, не
	// сказав об этом ни слова.
	сПроверкой := живое("edit:1", FromEdit, KindConsistency,
		"Супруг должен быть противоположного пола тому, о ком условие.")
	сПроверкой.Check = &Check{Type: CheckSpouseGender}
	сПроверкой.Check.Normalize()

	book := свод(
		живое("lint:2", FromLint, KindConsistency, "Следи за согласованием по роду во всём условии."),
		сПроверкой,
	)
	plan := Sanitize([]CompactionGroup{
		группа("lint:2", []string{"edit:1"}, "Следи за родом: супруг противоположного пола."),
	}, book)
	if len(plan.Groups) != 0 {
		t.Fatalf("правило с машинной проверкой слито: %+v", plan.Groups)
	}
}

func TestРазныеОбластиНеСливаются(t *testing.T) {
	// Слияние частного в общее расширило бы область молча: правило про
	// один источник начало бы уезжать во все.
	частное := живое("lint:3", FromLint, KindSubstance, "Не путай очную форму с заочной.")
	частное.Scope = Scope{Sources: []int64{7}}
	общее := живое("lint:4", FromLint, KindSubstance, "Не путай формы участия между собой.")

	plan := Sanitize([]CompactionGroup{
		группа("lint:4", []string{"lint:3"}, "Не путай формы участия и очную с заочной."),
	}, свод(частное, общее))
	if len(plan.Groups) != 0 {
		t.Fatalf("правило с областью слито в правило без области: %+v", plan.Groups)
	}
}

func TestОдинаковыеОбластиСливаются(t *testing.T) {
	// Обратная сторона той же проверки: совпадающие области не должны
	// мешать. Порядок меток внутри области ничего не значит, и
	// сравнение, зависящее от него, объявило бы разными две одинаковые
	// области — уплотнение перестало бы находить что бы то ни было,
	// оставаясь на вид работающим.
	a := живое("lint:5", FromLint, KindSubstance, "Не путай очную форму с заочной нигде в условии.")
	a.Scope = Scope{Sources: []int64{7, 3}, Units: []string{"3.2", "3.1"}}
	b := живое("lint:6", FromLint, KindSubstance, "Очную форму не называй заочной.")
	b.Scope = Scope{Sources: []int64{3, 7}, Units: []string{"3.1", "3.2"}}

	plan := Sanitize([]CompactionGroup{
		группа("lint:5", []string{"lint:6"}, "Не путай очную форму с заочной."),
	}, свод(a, b))
	if len(plan.Groups) != 1 {
		t.Fatalf("одинаковые области сочтены разными: %+v", plan)
	}
}

func TestРазныеРодаНеСливаются(t *testing.T) {
	// Род задаёт вес в блоке. Слив правило о существе дела в правило о
	// слоге, мы перевели бы его в тот разряд, которым блок жертвует
	// первым, — и потеря была бы молчаливой.
	plan := Sanitize([]CompactionGroup{
		группа("lint:8", []string{"lint:7"}, "Не выдумывай обстоятельств и пиши просто."),
	}, свод(
		живое("lint:7", FromLint, KindSubstance, "Не выдумывай обстоятельств, которых нет в источнике."),
		живое("lint:8", FromLint, KindLanguage, "Пиши простыми предложениями без канцелярита."),
	))
	if len(plan.Groups) != 0 {
		t.Fatalf("правила разного рода слиты: %+v", plan.Groups)
	}
}

func TestВстроенноеПравилоУплотнениемНеТрогается(t *testing.T) {
	// У встроенного своя причина существовать: оно закрывает то, что
	// промптом не лечится.
	book := свод(
		живое("builtin:one-person", Builtin, KindConsistency, "Пиши про одного человека."),
		живое("lint:9", FromLint, KindConsistency, "Не заводи второго человека в середине условия."),
	)
	plan := Sanitize([]CompactionGroup{
		группа("lint:9", []string{"builtin:one-person"}, "Пиши про одного человека и не заводи второго."),
	}, book)
	if len(plan.Groups) != 0 {
		t.Fatalf("встроенное правило слито: %+v", plan.Groups)
	}
	plan = Sanitize([]CompactionGroup{
		группа("builtin:one-person", []string{"lint:9"}, "Пиши про одного человека."),
	}, book)
	if len(plan.Groups) != 0 {
		t.Fatalf("встроенное правило переписано уплотнением: %+v", plan.Groups)
	}
}

func TestСлияниеБезЭкономииОтклоняется(t *testing.T) {
	// Уплотнение, не освобождающее места, — это переписывание свода без
	// причины. Цена у него есть (правила меняются), выгоды нет.
	long := strings.Repeat("я", 100)
	plan := Sanitize([]CompactionGroup{
		// Группа из одного правила: текст не короче прежнего.
		{KeepID: "lint:10", MergeIDs: []string{}, Text: long + "!", Why: "сжать"},
	}, свод(живое("lint:10", FromLint, KindLanguage, long)))
	if len(plan.Groups) != 0 {
		t.Fatalf("переписывание без экономии принято: %+v", plan.Groups)
	}

	// А то же самое, но короче, — законное сжатие.
	plan = Sanitize([]CompactionGroup{
		{KeepID: "lint:10", MergeIDs: []string{}, Text: "Коротко.", Why: "сжать"},
	}, свод(живое("lint:10", FromLint, KindLanguage, long)))
	if len(plan.Groups) != 1 {
		t.Fatalf("сжатие многословного правила отклонено: %+v", plan)
	}
	if plan.Groups[0].Saved != len(long)-len("Коротко.") {
		t.Fatalf("экономия посчитана неверно: %d", plan.Groups[0].Saved)
	}
}

func TestСлияниеРастящееБлокОтклоняется(t *testing.T) {
	// Второй случай отсутствия выгоды, и он не ловится потолком текста:
	// правил в группе двое, сводная формулировка длиннее обоих вместе, и
	// блок после такого «уплотнения» становится БОЛЬШЕ. Первая проверка
	// на экономию сюда не дотягивается — она про группу из одного
	// правила, где сравнивать не с чем.
	keep := живое("lint:18", FromLint, KindLanguage, "Пиши просто.")
	victim := живое("lint:19", FromLint, KindLanguage, "Коротко.")

	длинная := "Пиши простыми предложениями, без канцелярита и без оборотов."
	if len(длинная) <= len(keep.Text)+ruleCost(victim) {
		t.Fatalf("фикстура не растит блок: %d против %d",
			len(длинная), len(keep.Text)+ruleCost(victim))
	}
	plan := Sanitize([]CompactionGroup{
		группа("lint:18", []string{"lint:19"}, длинная),
	}, свод(keep, victim))
	if len(plan.Groups) != 0 {
		t.Fatalf("слияние, растящее блок, принято: %+v", plan.Groups)
	}
}

func TestПравилоНеПопадаетВДвеГруппы(t *testing.T) {
	// Слитое дважды исчезло бы вместе с требованием, которое несло.
	// Вторая группа при этом не урезается до «перепиши выжившего», а
	// отклоняется целиком: её сводный текст сочинялся на два правила, и
	// поставить его одному значит подменить требование текстом, которого
	// никто для него не писал.
	book := свод(
		живое("lint:11", FromLint, KindLanguage, "Не пиши канцелярских оборотов в условии задачи."),
		живое("lint:12", FromLint, KindLanguage, "Пиши условие простыми предложениями без оборотов."),
		живое("lint:13", FromLint, KindLanguage, "Не усложняй синтаксис условия задачи без нужды."),
	)
	plan := Sanitize([]CompactionGroup{
		группа("lint:11", []string{"lint:12"}, "Пиши просто, без канцелярита."),
		группа("lint:13", []string{"lint:12"}, "Не усложняй синтаксис."),
	}, book)
	if len(plan.Groups) != 1 {
		t.Fatalf("групп принято %d вместо одной: %+v", len(plan.Groups), plan.Groups)
	}
	if plan.Groups[0].KeepID != "lint:11" {
		t.Fatalf("принята не первая группа: %+v", plan.Groups[0])
	}
	// И правило lint:13 осталось нетронутым: переписывать его никто не
	// просил.
	for _, g := range plan.Groups {
		if g.KeepID == "lint:13" {
			t.Fatalf("правило переписано остатком чужой группы: %+v", g)
		}
	}
}

func TestЧислаПланаСчитаютсяКодом(t *testing.T) {
	// Числа модели проверять нечем, и потому они не принимаются вовсе:
	// «освобождено 900 байт» при настоящих ста — это обещание, по
	// которому составитель решит, что места хватило.
	a := живое("lint:14", FromLint, KindLanguage, "Не пиши канцелярских оборотов в условии задачи.")
	b := живое("lint:15", FromLint, KindLanguage, "Пиши условие простыми предложениями без оборотов.")
	book := свод(a, b)

	before, count := blockBytes(book.inForce())
	if count != 2 || before != ruleCost(a)+ruleCost(b) {
		t.Fatalf("счёт блока разошёлся: %d байт, %d правил", before, count)
	}

	plan := Sanitize([]CompactionGroup{{
		KeepID: "lint:14", MergeIDs: []string{"lint:15"},
		Text: "Пиши просто.", Why: "оба про слог",
		Saved: 9000, // выдумка модели
	}}, book)
	if len(plan.Groups) != 1 {
		t.Fatalf("группа отклонена: %+v", plan)
	}
	want := len(a.Text) - len("Пиши просто.") + ruleCost(b)
	if plan.Groups[0].Saved != want {
		t.Fatalf("экономия взята у модели: %d вместо %d", plan.Groups[0].Saved, want)
	}
	if plan.Before != before || plan.After != before-want {
		t.Fatalf("размеры блока разошлись: до %d, после %d", plan.Before, plan.After)
	}
}

func TestВытесненноеСчитаетсяТемЖеОтбором(t *testing.T) {
	// Спрашивать «сколько правил не доезжает до модели» и отвечать по
	// другому правилу значило бы успокаивать составителя числом, к делу
	// не относящимся.
	var list []Rule
	for i := 0; i < blockLimit+4; i++ {
		list = append(list, живое(
			// Опознаватели разной длины не влияют: в блок уходит текст.
			"lint:many-"+string(rune('a'+i)), FromLint, KindLanguage,
			"Не пиши канцелярских оборотов в условии задачи номер "+string(rune('a'+i))+".",
		))
	}
	book := свод(list...)
	if got := book.Dropped(); got <= 0 {
		t.Fatalf("вытеснение не замечено при %d правилах: %d", len(list), got)
	}

	scoped, core := book.pick(Context{})
	if want := len(list) - len(scoped) - len(core); book.Dropped() != want {
		t.Fatalf("вытеснено %d, а отбор берёт %d", book.Dropped(), len(scoped)+len(core))
	}
}

func TestЗакрытоеПравилоВСчётБлокаНеИдёт(t *testing.T) {
	// Погашенное правило в задание не уходит, и считать его занятым
	// местом значило бы предложить уплотнение там, где места довольно.
	closed := живое("lint:16", FromLint, KindLanguage, "Погашенное правило.")
	closed.Status = Muted
	book := свод(
		живое("lint:17", FromLint, KindLanguage, "Действующее правило."),
		closed,
	)
	if _, count := blockBytes(book.inForce()); count != 1 {
		t.Fatalf("в счёт блока попало погашенное правило: %d", count)
	}
}
