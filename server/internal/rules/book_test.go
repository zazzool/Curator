package rules

import (
	"fmt"
	"strings"
	"testing"
)

func действующее(id string, over func(*Rule)) Rule {
	r := правило(id, func(r *Rule) {
		r.Source = Builtin
		r.Status = Active
		if over != nil {
			over(r)
		}
	})
	return r
}

func TestПравилоЧужогоИсточникаВЗаданиеНеУходит(t *testing.T) {
	// То, ради чего область действия и заведена. Правило про один
	// документ, ушедшее ко всем, занимает в задании место правила,
	// которое было бы кстати, — а выглядит это как полный свод.
	свой := действующее("свой", func(r *Rule) {
		r.Text = "В этом приказе явка называется очной формой."
		r.Scope = Scope{Sources: []int64{1}}
	})
	чужой := действующее("чужой", func(r *Rule) {
		r.Text = "В этих рекомендациях степень пишется римской цифрой."
		r.Scope = Scope{Sources: []int64{2}}
	})
	book := NewBook([]Rule{свой, чужой}, сейчас)

	block := book.Block(Context{SourceID: 1})
	if !strings.Contains(block, "явка называется очной формой") {
		t.Fatalf("правило своего источника не ушло в задание:\n%s", block)
	}
	if strings.Contains(block, "римской цифрой") {
		t.Fatalf("правило чужого источника ушло в задание:\n%s", block)
	}
}

func TestПравилоПроРазделДостаётсяЕгоПунктам(t *testing.T) {
	// Иначе область пришлось бы перечислять поимённо и переписывать при
	// каждом пополнении источника — то есть она устаревала бы молча.
	r := действующее("раздел", func(r *Rule) {
		r.Text = "В третьем разделе сроки считаются рабочими днями."
		r.Scope = Scope{Sources: []int64{1}, Units: []string{"3"}}
	})
	book := NewBook([]Rule{r}, сейчас)

	if block := book.Block(Context{SourceID: 1, UnitPath: "3/3.1"}); block == "" {
		t.Fatal("правило про раздел не досталось его пункту")
	}
	if block := book.Block(Context{SourceID: 1, UnitPath: "4/4.1"}); block != "" {
		t.Fatalf("правило про третий раздел ушло в четвёртый:\n%s", block)
	}
}

func TestМеткаРазделаНеРоднитТретийСТридцатым(t *testing.T) {
	// Голое начало строки роднит «3» с «30», а «3.1» с «3.10»: у
	// источника с десятью пунктами в разделе правило про первый
	// досталось бы десятому, и заметить это можно только по тексту
	// готовой задачи. Сверять надо звено пути целиком.
	раздел := действующее("раздел3", func(r *Rule) {
		r.Text = "Правило про третий раздел."
		r.Scope = Scope{Sources: []int64{1}, Units: []string{"3"}}
	})
	пункт := действующее("пункт31", func(r *Rule) {
		r.Text = "Правило про пункт 3.1."
		r.Scope = Scope{Sources: []int64{2}, Units: []string{"3.1"}}
	})
	book := NewBook([]Rule{раздел, пункт}, сейчас)

	// Тридцатый раздел начинается с той же цифры, что и третий.
	if block := book.Block(Context{SourceID: 1, UnitPath: "30/30.1"}); block != "" {
		t.Fatalf("правило про раздел 3 досталось разделу 30:\n%s", block)
	}
	if block := book.Block(Context{SourceID: 1, UnitPath: "3/3.1"}); block == "" {
		t.Fatal("правило про раздел 3 не досталось его пункту")
	}

	// И то же самое на плоском пути, каким он был у старых заданий:
	// «3.10» начинается с «3.1».
	if block := book.Block(Context{SourceID: 2, UnitPath: "3.10"}); block != "" {
		t.Fatalf("правило про 3.1 досталось 3.10:\n%s", block)
	}
	if block := book.Block(Context{SourceID: 2, UnitPath: "3.1"}); block == "" {
		t.Fatal("правило не досталось своей же единице")
	}
}

func TestСтараяЗаписьБезПутиСверяетсяПоМетке(t *testing.T) {
	// Путь единицы появился позже самих заданий. Не падай сверка на
	// метку — правило про источник перестало бы действовать на его
	// старых заданиях, и объяснить это было бы нечем.
	r := действующее("по метке", func(r *Rule) {
		r.Scope = Scope{Sources: []int64{1}, Units: []string{"3.1"}}
	})
	book := NewBook([]Rule{r}, сейчас)
	if block := book.Block(Context{SourceID: 1, UnitPath: "3.1"}); block == "" {
		t.Fatal("правило не досталось заданию, у которого путь равен метке")
	}
}

func TestМеткаБезИсточникаОбластьюНеСчитается(t *testing.T) {
	// «3.1» есть в любом приказе. Правило, написанное про один документ,
	// уехало бы во все, а область выглядела бы узкой.
	r := действующее("без источника", func(r *Rule) {
		r.Scope = Scope{Units: []string{"3.1"}}
	})
	if err := r.Normalize(сейчас); err != nil {
		t.Fatal(err)
	}
	if len(r.Scope.Units) != 0 {
		t.Fatalf("метка без источника осталась областью: %+v", r.Scope)
	}
}

func TestИзмеренияОбластиСкладываютсяПоИ(t *testing.T) {
	// Иначе область, которую составитель сужал двумя измерениями,
	// становилась бы шире от каждого уточнения.
	r := действующее("два измерения", func(r *Rule) {
		r.Scope = Scope{Sources: []int64{1}, TaskKinds: []string{"action"}}
	})
	book := NewBook([]Rule{r}, сейчас)

	if block := book.Block(Context{SourceID: 1, TaskKind: "action"}); block == "" {
		t.Fatal("правило не досталось задаче, где сошлись оба измерения")
	}
	if block := book.Block(Context{SourceID: 1, TaskKind: "recognise"}); block != "" {
		t.Fatalf("сошлось одно измерение из двух, а правило ушло:\n%s", block)
	}
}

func TestУзелВидитСвоиПравилаАПредпросмотрВсе(t *testing.T) {
	// Правило про существо дела, уехавшее вычитке, зовёт её судить о
	// существе, — а весь этап держится на том, что существа она не
	// трогает. Но пустой узел это НЕ «узел без правил»: так свод
	// показывают составителю, и там он обещан весь.
	r := действующее("узловое", func(r *Rule) {
		r.Scope = Scope{Nodes: []string{"compose"}}
	})
	book := NewBook([]Rule{r}, сейчас)

	if block := book.Block(Context{Node: "compose"}); block == "" {
		t.Fatal("правило не досталось своему узлу")
	}
	if block := book.Block(Context{Node: "proofread"}); block != "" {
		t.Fatalf("правило написания ушло вычитке:\n%s", block)
	}
	if block := book.Block(Context{}); block == "" {
		t.Fatal("правило с названным узлом исчезло из предпросмотра всего свода")
	}
}

func TestЧастныеПравилаСтоятПередОбщими(t *testing.T) {
	// Начало списка модель удерживает лучше конца, и если чем-то
	// придётся пренебречь, пусть это будет общее место: общее она знает
	// и из самого задания, а частное ей больше взять неоткуда.
	общее := действующее("общее", func(r *Rule) {
		r.Text = "Общее правило про устройство задачи."
		r.Kind = KindSubstance // старший род, чтобы порядок решала не важность
	})
	частное := действующее("частное", func(r *Rule) {
		r.Text = "Частное правило про этот источник."
		r.Kind = KindLanguage
		r.Scope = Scope{Sources: []int64{1}}
	})
	book := NewBook([]Rule{общее, частное}, сейчас)

	block := book.Block(Context{SourceID: 1})
	чП := strings.Index(block, "Частное правило")
	оП := strings.Index(block, "Общее правило")
	if чП < 0 || оП < 0 {
		t.Fatalf("в блок попало не всё:\n%s", block)
	}
	if чП > оП {
		t.Fatalf("общее правило встало перед частным:\n%s", block)
	}
}

func TestОбщиеПравилаНеВытесняютсяЧастнымиЦеликом(t *testing.T) {
	// Живой отказ у донора: каждое ведро наполнялось само по себе, а
	// общий потолок числа правил урезался с конца — и шестнадцать
	// частных правил, выведенных моделью, выносили из задания ВСЕ
	// общие, включая написанные человеком.
	list := []Rule{
		действующее("врачебное", func(r *Rule) {
			r.Source = FromCurator
			r.Text = "Требование составителя, которое обязано доехать."
		}),
	}
	// Правила нарочно КОРОТКИЕ: упрись отбор в байты, и потолок числа
	// правил, ради которого проверка и написана, не сработал бы вовсе.
	for i := 0; i < blockLimit*2; i++ {
		номер := fmt.Sprintf("%02d", i)
		list = append(list, действующее("выведенное"+номер, func(r *Rule) {
			r.Source = FromLint
			r.Confirmations = Quorum
			r.Text = "Частное правило " + номер + "."
			r.Scope = Scope{Sources: []int64{1}}
		}))
	}
	book := NewBook(list, сейчас)

	block := book.Block(Context{SourceID: 1})
	if !strings.Contains(block, "Требование составителя") {
		t.Fatalf("врачебное правило вытеснено выведенными:\n%s", block)
	}
}

func TestПустойСводДаётПустуюСтрокуАНеЗаголовок(t *testing.T) {
	// «ПРАВИЛА:» с пустотой под ним модель истолкует по-своему — вернее
	// всего, примет за требование следующий абзац.
	book := NewBook(nil, сейчас)
	if block := book.Block(Context{SourceID: 1}); block != "" {
		t.Fatalf("пустой свод дал заголовок:\n%q", block)
	}

	// И то же самое, когда правила есть, но ни одно не подошло.
	r := действующее("чужое", func(r *Rule) { r.Scope = Scope{Sources: []int64{7}} })
	if block := NewBook([]Rule{r}, сейчас).Block(Context{SourceID: 1}); block != "" {
		t.Fatalf("свод без подходящих правил дал заголовок:\n%q", block)
	}
}

func TestКандидатВЗаданиеНеУходит(t *testing.T) {
	// Кандидат — это правило, которого ещё не подтвердили. Пусти его в
	// задание, и кворум перестанет что-либо значить.
	r := действующее("кандидат", func(r *Rule) {
		r.Source = FromLint
		r.Status = Candidate
	})
	if block := NewBook([]Rule{r}, сейчас).Block(Context{}); block != "" {
		t.Fatalf("кандидат ушёл в задание:\n%s", block)
	}
}

func TestОдинОтветНаОдинСвод(t *testing.T) {
	// Блок собирается обходом среза, а не карты, и порядок в нём не
	// должен плясать: запись обращения к модели иначе перестаёт быть
	// следом того, что было, а разбор «почему задача вышла такой»
	// упирается в то, что задание каждый раз другое.
	list := []Rule{}
	for i := 0; i < 8; i++ {
		list = append(list, действующее(string(rune('a'+i)), func(r *Rule) {
			r.Text = "Правило номер " + string(rune('a'+i)) + "."
		}))
	}
	book := NewBook(list, сейчас)
	первый := book.Block(Context{SourceID: 1})
	for i := 0; i < 20; i++ {
		if ещё := book.Block(Context{SourceID: 1}); ещё != первый {
			t.Fatalf("блок пляшет:\n%q\n%q", первый, ещё)
		}
	}
}
