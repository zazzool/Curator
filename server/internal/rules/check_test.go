package rules

import (
	"strings"
	"testing"
)

func TestНегоднаяПроверкаОтбрасываетсяАПравилоОстаётся(t *testing.T) {
	// forbid-words без слов запрещает пустоту, то есть не срабатывает
	// никогда, а правило с такой проверкой выглядит проверяемым машинно.
	// Молчаливая проверка хуже отсутствующей: на неё полагаются.
	for _, негодная := range []*Check{
		{Type: CheckForbidWords},
		{Type: CheckForbidWhen, Words: []string{"вальпроат"}},
		{Type: CheckLength},
		{Type: CheckPattern, Pattern: "("},
		{Type: "выдуманный", Words: []string{"что-то"}},
	} {
		if негодная.Known() {
			t.Fatalf("негодная проверка объявлена исполнимой: %+v", негодная)
		}
	}

	r := правилоСПроверкой(&Check{Type: CheckForbidWords})
	if err := r.Normalize(сейчас); err != nil {
		t.Fatalf("правило с негодной проверкой отбито целиком: %v", err)
	}
	if r.Check != nil {
		t.Fatal("негодная проверка осталась при правиле")
	}
	if r.Text == "" {
		t.Fatal("вместе с проверкой пропал и текст правила")
	}
}

func TestГоднаяПроверкаПереживаетПриведение(t *testing.T) {
	r := правилоСПроверкой(&Check{
		Type:  CheckForbidWords,
		Words: []string{"  Ёлка ", "елка", ""},
		Where: []string{"title", "выдумка", "segments", "segments"},
	})
	if err := r.Normalize(сейчас); err != nil {
		t.Fatal(err)
	}
	if r.Check == nil {
		t.Fatal("годная проверка выброшена")
	}
	// «ё» сводится к «е» здесь, а не при сличении: слово, записанное
	// составителем через «ё», и слово, написанное моделью через «е», —
	// одно слово.
	if len(r.Check.Words) != 1 || r.Check.Words[0] != "елка" {
		t.Fatalf("слова приведены не так: %+v", r.Check.Words)
	}
	if len(r.Check.Where) != 2 {
		t.Fatalf("места приведены не так: %+v", r.Check.Where)
	}
	for _, where := range r.Check.Where {
		if where == "выдумка" {
			t.Fatal("незнакомое место принято: проверка смотрела бы в пустоту")
		}
	}
}

func TestВариантыОтветаМестомПроверкиНеБывают(t *testing.T) {
	// У задачи-узнавания вариант — это метка единицы («3.2»), и
	// словарному предикату искать в ней нечего: проверка не находила бы
	// ничего никогда, а правило выглядело бы работающим.
	c := &Check{Type: CheckForbidWords, Words: []string{"срок"}, Where: []string{"options"}}
	c.Normalize()
	for _, where := range c.Fields() {
		if where == "options" {
			t.Fatal("варианты ответа приняты местом проверки")
		}
	}
	// Посчитать их при этом можно: счёт метку читать не обязан.
	count := &Check{Type: CheckCount, What: CountOptions, Min: 3}
	if !count.Known() {
		t.Fatal("счёт вариантов ответа объявлен неисполнимым")
	}
}

func TestКаталогЗакрытИОписанЦеликом(t *testing.T) {
	// Разъедься описание с тем, что исполняет код, — и выбирающий начал
	// бы уверенно ставить проверки, которые не работают.
	catalog := Catalog()
	if len(catalog) == 0 {
		t.Fatal("каталог предикатов пуст")
	}
	seen := map[CheckType]bool{}
	for _, spec := range catalog {
		if spec.Title == "" || spec.About == "" || len(spec.Params) == 0 {
			t.Fatalf("предикат %q описан не до конца: %+v", spec.Type, spec)
		}
		if seen[spec.Type] {
			t.Fatalf("предикат %q назван в каталоге дважды", spec.Type)
		}
		seen[spec.Type] = true
	}
	// Выражение пишет только составитель: предикат слишком остёр, чтобы
	// отдавать его модели.
	for _, spec := range catalog {
		if spec.Type == CheckPattern && !spec.DoctorOnly {
			t.Fatal("регулярное выражение открыто модели")
		}
	}
}

func TestПроверкаЧитаетсяСловамиАНеРодом(t *testing.T) {
	// Фразу читает составитель в списке правил, и «forbid-words» ему
	// ничего не говорит.
	for _, c := range []*Check{
		{Type: CheckForbidWords, Words: []string{"явка"}},
		{Type: CheckRequireWords, Words: []string{"срок"}},
		{Type: CheckForbidWhen, When: []string{"беременность"}, Words: []string{"вальпроат"}},
		{Type: CheckSpouseGender},
		{Type: CheckLength, Min: 200, Max: 800},
		{Type: CheckCount, What: CountMarked, Min: 2},
		{Type: CheckPattern, Pattern: `\d{4}`},
	} {
		words := Describe(c)
		if words == "" || words == string(c.Type) {
			t.Fatalf("проверка %q не описана словами: %q", c.Type, words)
		}
		if strings.Contains(words, "-") && strings.Contains(words, string(c.Type)) {
			t.Fatalf("в описании проверки %q стоит её опознаватель: %q", c.Type, words)
		}
	}
	if Describe(nil) != "" {
		t.Fatal("отсутствующая проверка описана словами")
	}
}

func TestПолВПроверкеТолькоИзЗакрытогоСловаря(t *testing.T) {
	c := &Check{Type: CheckForbidWords, Words: []string{"срок"}, Gender: "неизвестно"}
	c.Normalize()
	if c.Gender != "" {
		t.Fatalf("незнакомый пол принят: %q", c.Gender)
	}
	// Правило, ограниченное неизвестным полом, не сработало бы никогда, и
	// выглядело бы при этом действующим. Пустой пол значит «при любом».
	c.Gender = GenderFemale
	c.Normalize()
	if c.Gender != GenderFemale {
		t.Fatal("знакомый пол потерян при приведении")
	}
}

func правилоСПроверкой(c *Check) Rule {
	return Rule{
		ID:     "проверка:образец",
		Title:  "Правило с машинной проверкой",
		Text:   "Текст этого правила ничем не примечателен.",
		Kind:   KindConsistency,
		Source: FromCurator,
		Status: Active,
		Pinned: true,
		Check:  c,
	}
}
