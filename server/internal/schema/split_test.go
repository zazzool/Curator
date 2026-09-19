package schema

import "testing"

func TestSplitРежетПоТочкеСЗапятой(t *testing.T) {
	got := Split("CREATE TABLE a (id INT);\nCREATE TABLE b (id INT);\n")
	if len(got) != 2 {
		t.Fatalf("команд %d, ожидалось 2: %q", len(got), got)
	}
	if got[0] != "CREATE TABLE a (id INT);" {
		t.Errorf("первая команда %q", got[0])
	}
}

func TestSplitВыбрасываетКомментарии(t *testing.T) {
	// Точка с запятой внутри пояснения — обычное дело, и разрезать по ней
	// команду нельзя: ровно этого ради комментарии и выбрасываются.
	text := "-- пояснение; с точкой с запятой\nCREATE TABLE a (\n" +
		"    -- ещё пояснение; и тут\n    id INT\n);\n"
	got := Split(text)
	if len(got) != 1 {
		t.Fatalf("команд %d, ожидалась 1: %q", len(got), got)
	}
	if want := "CREATE TABLE a (\n    id INT\n);"; got[0] != want {
		t.Errorf("команда %q, ожидалась %q", got[0], want)
	}
}

func TestSplitМногострочнаяКоманда(t *testing.T) {
	got := Split("CREATE TABLE a (\n  id INT,\n  name TEXT\n);\n")
	if len(got) != 1 {
		t.Fatalf("команд %d, ожидалась 1: %q", len(got), got)
	}
}

func TestSplitПустойФайлДаётПустойСписок(t *testing.T) {
	got := Split("\n-- только пояснение\n\n")
	if got == nil {
		t.Fatal("вернулся nil: пустой список — это [], а не nil")
	}
	if len(got) != 0 {
		t.Errorf("команд %d, ожидалось 0: %q", len(got), got)
	}
}
