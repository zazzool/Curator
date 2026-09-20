package gen

import (
	"strings"
	"testing"
	"time"

	"curator/server/internal/rules"
)

var судныйДень = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func сводИз(list ...rules.Rule) rules.Book {
	out := make([]rules.Rule, 0, len(list))
	for _, r := range list {
		if r.Status == "" {
			r.Status = rules.Active
		}
		if r.Source == "" {
			r.Source = rules.FromCurator
		}
		if r.Kind == "" {
			r.Kind = rules.KindConsistency
		}
		r.Pinned = true
		if err := r.Normalize(судныйДень); err != nil {
			panic(err)
		}
		out = append(out, r)
	}
	return rules.NewBook(out, судныйДень)
}

func TestСудьяПроходитМолчаНоНеВыдаётЭтоЗаЧистоту(t *testing.T) {
	// Свод может быть полон правил, у которых предиката нет. Судья тогда
	// честно прошёл, не проверив ничего, и «нарушений нет» тут ложь:
	// составитель решил бы, что свод стоит на страже, а он не стоял.
	book := сводИз(rules.Rule{
		ID: "без-проверки", Title: "Пиши проще",
		Text: "Пиши так, будто читает специалист.",
	})
	out := Judge(book, планЗаказа(KindRecognise), условие("Любой текст."))
	if !out.Done {
		t.Fatalf("судья не прошёл: %s", out.Note)
	}
	if out.Checked != 0 {
		t.Fatalf("прогнано правил %d, а проверок нет ни одной", out.Checked)
	}
	if !strings.Contains(out.Remarks(), "машинной проверки не несёт") {
		t.Fatalf("ноль прогнанных выдан за чистоту: %q", out.Remarks())
	}
}

func TestСудьяМолчитТолькоПрогнавХотьЧтоТо(t *testing.T) {
	book := сводИз(rules.Rule{
		ID: "с-проверкой", Title: "Без слова «явка»",
		Text:  "Не пиши в условии слова «явка».",
		Check: &rules.Check{Type: rules.CheckForbidWords, Words: []string{"явка"}},
	})
	out := Judge(book, планЗаказа(KindRecognise), условие("Заявление подано в понедельник."))
	if out.Checked != 1 {
		t.Fatalf("прогнано правил %d вместо одного", out.Checked)
	}
	if !out.Clean() {
		t.Fatalf("на исправном условии найдено: %+v", out.Findings)
	}
	if out.Remarks() != "" {
		t.Fatalf("на прогнанной и чистой задаче сказано лишнее: %q", out.Remarks())
	}
}

func TestНаходкаНазываетПравилоАНеТолькоМесто(t *testing.T) {
	// Замечание читает составитель, и «в условии стоит «явка»» не
	// говорит, чинить задачу или правило.
	book := сводИз(rules.Rule{
		ID: "с-проверкой", Title: "Без слова «явка»",
		Text:  "Не пиши в условии слова «явка».",
		Check: &rules.Check{Type: rules.CheckForbidWords, Words: []string{"явка"}},
	})
	out := Judge(book, планЗаказа(KindRecognise), условие("Явка назначена на среду."))
	if len(out.Findings) != 1 {
		t.Fatalf("находок %d вместо одной: %+v", len(out.Findings), out.Findings)
	}
	if out.Findings[0].RuleID != "с-проверкой" || out.Findings[0].Title == "" {
		t.Fatalf("находка не называет правила: %+v", out.Findings[0])
	}
	if out.Findings[0].Where != "segments[0]" {
		t.Fatalf("находка не указывает места: %+v", out.Findings[0])
	}
	if !strings.Contains(out.Remarks(), "Без слова") {
		t.Fatalf("в замечании не названо правило: %q", out.Remarks())
	}
}

func TestСудьяМеритТемЖеОтборомЧтоИЗадание(t *testing.T) {
	// Проверяется то же, чего требовали: разойдись отборы — и задача
	// получала бы замечание по правилу, которого ей не ставили.
	чужое := rules.Rule{
		ID: "чужой-источник", Title: "Правило другого источника",
		Text:  "Не пиши в условии слова «явка».",
		Scope: rules.Scope{Sources: []int64{999}},
		Check: &rules.Check{Type: rules.CheckForbidWords, Words: []string{"явка"}},
	}
	book := сводИз(чужое)
	plan := планЗаказа(KindRecognise)

	out := Judge(book, plan, условие("Явка назначена на среду."))
	if len(out.Findings) != 0 {
		t.Fatalf("сработало правило чужого источника: %+v", out.Findings)
	}
	if out.Checked != 0 {
		t.Fatalf("чужое правило пошло в счёт прогнанных: %d", out.Checked)
	}

	// А своё — срабатывает.
	своё := чужое
	своё.ID = "свой-источник"
	своё.Scope = rules.Scope{Sources: []int64{plan.SourceID}}
	if out := Judge(сводИз(своё), plan, условие("Явка назначена.")); len(out.Findings) != 1 {
		t.Fatalf("правило своего источника не сработало: %+v", out)
	}
}

func TestПогашенноеПравилоНеСудит(t *testing.T) {
	// Погашенное правило остаётся в своде ответом на «а почему система
	// перестала этого требовать». Суди оно по-прежнему — гашение ничего
	// бы не значило.
	book := сводИз(rules.Rule{
		ID: "погашено", Title: "Без слова «явка»",
		Text:   "Не пиши в условии слова «явка».",
		Status: rules.Muted,
		Check:  &rules.Check{Type: rules.CheckForbidWords, Words: []string{"явка"}},
	})
	out := Judge(book, планЗаказа(KindRecognise), условие("Явка назначена."))
	if len(out.Findings) != 0 || out.Checked != 0 {
		t.Fatalf("погашенное правило судило: %+v", out)
	}
}

func TestНаходкиИдутПоМестуВЗадачеАНеПоПорядкуПравил(t *testing.T) {
	// Составитель читает их, идя по условию сверху вниз, и прыгающий по
	// фрагментам список заставляет искать место каждой заново.
	book := сводИз(
		rules.Rule{ID: "второе", Title: "Без «среды»",
			Text:  "Не пиши в условии слова «среда».",
			Check: &rules.Check{Type: rules.CheckForbidWords, Words: []string{"среда"}}},
		rules.Rule{ID: "первое", Title: "Без «явки»",
			Text:  "Не пиши в условии слова «явка».",
			Check: &rules.Check{Type: rules.CheckForbidWords, Words: []string{"явка"}}},
	)
	out := Judge(book, планЗаказа(KindRecognise),
		условие("Явка назначена.", "Приём в среду."))
	if len(out.Findings) != 2 {
		t.Fatalf("находок %d вместо двух: %+v", len(out.Findings), out.Findings)
	}
	if out.Findings[0].Where != "segments[0]" || out.Findings[1].Where != "segments[1]" {
		t.Fatalf("находки идут не по местам: %+v", out.Findings)
	}
}

func TestНесостоявшийсяСудГоворитОСебеВслух(t *testing.T) {
	// Молчание составитель примет за «правила соблюдены» и отпустит
	// задачу к врачу непроверенной — то же правило, что у всех прочих
	// узлов.
	молчание := RuleCheckResult{Note: "свод правил не прочитан"}
	if молчание.Clean() {
		t.Fatal("несостоявшийся суд объявлен чистым")
	}
	if !strings.Contains(молчание.Remarks(), "не прогонялись") {
		t.Fatalf("о несостоявшемся суде сказано невнятно: %q", молчание.Remarks())
	}
	// И без причины он тоже не молчит: «судья не ходил» — само по себе
	// сведение.
	var безПричины RuleCheckResult
	if безПричины.Remarks() == "" {
		t.Fatal("суд без причины промолчал вовсе")
	}
}
