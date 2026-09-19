package source

import "strings"

import "testing"

func TestSplitTextРежетПоЗаголовкам(t *testing.T) {
	got := SplitText("# Раздел\n\nтело раздела\n\n## Пункт\n\nтело пункта\n")
	if len(got) != 2 {
		t.Fatalf("кусков %d, ожидалось 2: %+v", len(got), got)
	}
	if got[0].Title != "Раздел" || got[0].Level != 1 {
		t.Errorf("первый кусок: %q уровень %d", got[0].Title, got[0].Level)
	}
	if got[1].Title != "Пункт" || got[1].Level != 2 {
		t.Errorf("второй кусок: %q уровень %d", got[1].Title, got[1].Level)
	}
	if got[1].Body != "тело пункта" {
		t.Errorf("тело второго куска %q", got[1].Body)
	}
}

func TestSplitTextСохраняетПреамбулу(t *testing.T) {
	// В приказах перед первым заголовком стоит преамбула, и терять её
	// нельзя: там бывает и предмет приказа, и дата.
	got := SplitText("преамбула приказа\n\n# Раздел\n\nтело\n")
	if len(got) != 2 {
		t.Fatalf("кусков %d, ожидалось 2: %+v", len(got), got)
	}
	if got[0].Body != "преамбула приказа" || got[0].Level != 0 {
		t.Errorf("преамбула: %q уровень %d", got[0].Body, got[0].Level)
	}
}

func TestSplitTextБезЗаголовковДаётОдинКусок(t *testing.T) {
	got := SplitText("приказ сплошняком, без разметки\nвторая строка\n")
	if len(got) != 1 {
		t.Fatalf("кусков %d, ожидался 1", len(got))
	}
	if !strings.Contains(got[0].Body, "вторая строка") {
		t.Errorf("тело %q", got[0].Body)
	}
}

func TestSplitTextНеРежетПоНумерацииПунктов(t *testing.T) {
	// «#3.2» — это текст, а не заголовок третьего уровня. Приняв его за
	// заголовок, разбор порезал бы приказ по нумерации и рассыпал бы
	// положения.
	got := SplitText("#3.2 Порядок оказания\nтело\n")
	if len(got) != 1 {
		t.Fatalf("кусков %d, ожидался 1: %+v", len(got), got)
	}
	if got[0].Title != "" {
		t.Errorf("строка принята за заголовок: %q", got[0].Title)
	}
}

func TestSplitTextМестоВИсходномТексте(t *testing.T) {
	text := "# Первый\nтело\n# Второй\nтело\n"
	got := SplitText(text)
	if len(got) != 2 {
		t.Fatalf("кусков %d", len(got))
	}
	// По этим числам человек находит кусок в оригинале, когда разбор
	// ошибся, — значит они обязаны указывать на заголовок.
	if text[got[1].CharFrom:got[1].CharFrom+len("# Второй")] != "# Второй" {
		t.Errorf("CharFrom второго куска = %d, указывает не на заголовок", got[1].CharFrom)
	}
	if got[1].CharTo != len(text) {
		t.Errorf("CharTo последнего куска %d, ожидалось %d", got[1].CharTo, len(text))
	}
}

func TestSplitTextПустойТекстДаётПустойСписок(t *testing.T) {
	got := SplitText("")
	if got == nil {
		t.Fatal("вернулся nil вместо пустого списка")
	}
	if len(got) != 0 {
		t.Errorf("кусков %d, ожидалось 0: %+v", len(got), got)
	}
}
