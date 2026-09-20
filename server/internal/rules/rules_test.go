package rules

import (
	"strings"
	"testing"
	"time"
)

var сейчас = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// правило — годная заготовка, которую проверки правят под свой случай.
func правило(id string, over func(*Rule)) Rule {
	r := Rule{
		ID:     id,
		Title:  "Правило " + id,
		Text:   "Не пиши в условии метку единицы.",
		Kind:   KindStructure,
		Source: FromLint,
	}
	if over != nil {
		over(&r)
	}
	return r
}

func TestВыведенноеПравилоЖдётКворума(t *testing.T) {
	// Одна правка — случайность, две — совпадение. Пусти правило в
	// задание с первого замечания, и модель получит в работу возведённую
	// в закон случайность соседнего задания.
	r := правило("r1", func(r *Rule) { r.Confirmations = Quorum - 1 })
	if err := r.Normalize(сейчас); err != nil {
		t.Fatal(err)
	}
	if r.Status != Candidate {
		t.Fatalf("правило с %d подтверждениями объявлено «%s»", r.Confirmations, r.Status)
	}

	r.Confirmations = Quorum
	if err := r.Normalize(сейчас); err != nil {
		t.Fatal(err)
	}
	if r.Status != Active {
		t.Fatalf("правило с кворумом осталось «%s»", r.Status)
	}
}

func TestПравилоСоставителяКворумаНеЖдёт(t *testing.T) {
	// Он и есть подтверждение. Заставь его ждать трёх задач — и
	// написанное им требование не действует ровно тогда, когда оно
	// написано: сразу после того, как он увидел ошибку.
	r := правило("r2", func(r *Rule) { r.Source = FromCurator })
	if err := r.Normalize(сейчас); err != nil {
		t.Fatal(err)
	}
	if r.Status != Active {
		t.Fatalf("правило составителя объявлено «%s» без подтверждений", r.Status)
	}
}

func TestПогашенноеПодтверждениямиНеВоскресает(t *testing.T) {
	// Погашенное побывало перед глазами и было отвергнуто. Воскреси его
	// счётчиком — и составитель гасил бы одно и то же правило каждую
	// неделю, не понимая, кто его включает.
	r := правило("r3", func(r *Rule) {
		r.Status = Muted
		r.Confirmations = Quorum * 10
	})
	if err := r.Normalize(сейчас); err != nil {
		t.Fatal(err)
	}
	if r.Status != Muted {
		t.Fatalf("погашенное правило воскресло как «%s»", r.Status)
	}
}

func TestРешениеЧеловекаСчётчикНеОтменяет(t *testing.T) {
	// Живой отказ, ради которого признак и заведён: обработчик ставит
	// «действует», хранилище пересчитывает по кворуму и возвращает
	// «кандидат», отвечая при этом УСПЕХОМ. Составитель видит чужое
	// состояние и считает, что включил правило.
	r := правило("r4", func(r *Rule) {
		r.Status = Active
		r.Pinned = true
		r.Confirmations = 0
	})
	if err := r.Normalize(сейчас); err != nil {
		t.Fatal(err)
	}
	if r.Status != Active {
		t.Fatalf("назначенное человеком состояние пересчитано в «%s»", r.Status)
	}
}

func TestВыведенноеПравилоЗатухаетАВрачебноеНет(t *testing.T) {
	// Правило, месяц не подтверждавшееся ни одним заданием, описывает
	// ошибку, которой модель больше не делает. Правил составителя это не
	// касается: их писал человек, и молчание тут значит «работает».
	давно := сейчас.Add(-Idle - time.Hour)

	выведенное := правило("r5", func(r *Rule) {
		r.Status = Active
		r.LastSeenAt = давно
	})
	if !выведенное.faded(сейчас) {
		t.Fatal("молчавшее месяц выведенное правило не затухло")
	}

	врачебное := правило("r6", func(r *Rule) {
		r.Source = FromCurator
		r.Status = Active
		r.LastSeenAt = давно
	})
	if врачебное.faded(сейчас) {
		t.Fatal("правило составителя затухло от молчания")
	}
}

func TestЗакрытоеДатойВБлокНеИдётНоИзСводаНеИсчезает(t *testing.T) {
	// Закрытие датой вместо удаления: вопрос «чего мы требовали в марте»
	// обязан иметь ответ, а свод дрейфует, и дрейф замечают через недели.
	когда := сейчас.Add(-time.Hour)
	r := правило("r7", func(r *Rule) {
		r.Status = Active
		r.ValidTo = &когда
	})
	if r.InForce() {
		t.Fatal("закрытое датой правило считается действующим")
	}
	book := NewBook([]Rule{r}, сейчас)
	if book.Len() != 1 {
		t.Fatal("закрытое правило исчезло из свода целиком")
	}
	if got := book.Applicable(Context{}); len(got) != 0 {
		t.Fatalf("закрытое правило ушло в отбор: %+v", got)
	}
}

func TestТекстПравилаМеряетсяБайтамиАНеЗнаками(t *testing.T) {
	// «300 знаков» кириллицей — это шестьсот байт, то есть пересказ
	// случая вместо правила. Мера байтовая намеренно.
	длинное := strings.Repeat("я", TextMax/2+10) // > TextMax байт, < TextMax знаков
	r := правило("r8", func(r *Rule) { r.Text = длинное })
	if err := (&r).Normalize(сейчас); err == nil {
		t.Fatalf("правило в %d байт (%d знаков) принято", len(длинное), len([]rune(длинное)))
	}
}

func TestПравилоБезТекстаИлиИмениНеПринимается(t *testing.T) {
	// Правило без текста ничего не требует, а в списке выглядит
	// требованием: составитель считает, что оно работает.
	безТекста := правило("r9", func(r *Rule) { r.Text = "  " })
	if err := безТекста.Normalize(сейчас); err == nil {
		t.Fatal("правило без текста принято")
	}
	безИмени := правило("r10", func(r *Rule) { r.Title = "" })
	if err := безИмени.Normalize(сейчас); err == nil {
		t.Fatal("правило без имени принято")
	}
}

func TestРодИИсточникТолькоИзСловаря(t *testing.T) {
	// Род, появившийся строкой по месту, получил бы вес ноль и молча
	// ушёл в хвост блока — то есть выпал бы из задания первым.
	чужойРод := правило("r11", func(r *Rule) { r.Kind = "клиника" })
	if err := чужойРод.Normalize(сейчас); err == nil {
		t.Fatal("род не из словаря принят")
	}
	чужойИсточник := правило("r12", func(r *Rule) { r.Source = "откуда-то" })
	if err := чужойИсточник.Normalize(сейчас); err == nil {
		t.Fatal("источник не из словаря принят")
	}
}

func TestВстроенныеПравилаГодныеИДействуют(t *testing.T) {
	list := Builtins(сейчас)
	if len(list) == 0 {
		t.Fatal("встроенных правил нет: свод на пустой установке пуст")
	}
	seen := map[string]bool{}
	for i := range list {
		r := list[i]
		if err := r.Normalize(сейчас); err != nil {
			t.Fatalf("встроенное правило %q негодно: %v", r.ID, err)
		}
		if !r.InForce() {
			t.Fatalf("встроенное правило %q не действует", r.ID)
		}
		if seen[r.ID] {
			t.Fatalf("встроенное правило %q заведено дважды", r.ID)
		}
		seen[r.ID] = true
		if r.Why == "" {
			t.Fatalf("у встроенного правила %q не сказано, почему оно есть", r.ID)
		}
	}
}
