package source

import "testing"

func units(pairs ...[2]string) []Unit {
	out := make([]Unit, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, Unit{Label: p[0], ParentLabel: p[1]})
	}
	return out
}

func TestBuildPathsСчитаетПутьИГлубину(t *testing.T) {
	got, err := BuildPaths(units(
		[2]string{"F", ""},
		[2]string{"F3", "F"},
		[2]string{"F32", "F3"},
		[2]string{"F32.1", "F32"},
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		path  string
		depth int
	}{{"F", 0}, {"F/F3", 1}, {"F/F3/F32", 2}, {"F/F3/F32/F32.1", 3}}
	for i, w := range want {
		if got[i].Path != w.path || got[i].Depth != w.depth {
			t.Errorf("%s: путь %q глубина %d, ожидалось %q и %d",
				got[i].Label, got[i].Path, got[i].Depth, w.path, w.depth)
		}
	}
}

func TestBuildPathsРаботаетНаНумерацииПунктов(t *testing.T) {
	// Ради этого случая путь и заведён: у пункта приказа нет формата, из
	// которого можно отрезать «раздел», а путь считается из связи.
	got, err := BuildPaths(units(
		[2]string{"3", ""},
		[2]string{"3.2", "3"},
		[2]string{"3.2.1", "3.2"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if got[2].Path != "3/3.2/3.2.1" {
		t.Errorf("путь %q", got[2].Path)
	}
}

func TestBuildPathsОтказываетНаПотеряннойВетке(t *testing.T) {
	// Пропустить такую единицу нельзя: источник выглядел бы исправным, а
	// задачи по ней не выпадали бы в подборе — и узнали бы об этом через
	// месяц по жалобе.
	_, err := BuildPaths(units([2]string{"F32", "F3"}))
	if err == nil {
		t.Fatal("родитель отсутствует, а разбор прошёл")
	}
}

func TestBuildPathsЛовитПетлю(t *testing.T) {
	_, err := BuildPaths(units(
		[2]string{"A", "B"},
		[2]string{"B", "A"},
	))
	if err == nil {
		t.Fatal("петля в родителях, а разбор прошёл")
	}
}

func TestBuildPathsОтказываетНаРазделителеВМетке(t *testing.T) {
	_, err := BuildPaths(units([2]string{"F3/2", ""}))
	if err == nil {
		t.Fatal("метка с разделителем принята")
	}
}

func TestBuildPathsОтказываетНаДвойнойМетке(t *testing.T) {
	_, err := BuildPaths(units([2]string{"F32", ""}, [2]string{"F32", ""}))
	if err == nil {
		t.Fatal("две единицы под одной меткой приняты")
	}
}

func TestSliceНеЗахватываетСоседа(t *testing.T) {
	// F3 и F30 начинаются одинаково, и срез по началу строки без
	// разделителя утащил бы соседа. Ошибка тихая: в подборе появились бы
	// задачи не из того раздела.
	all, err := BuildPaths(units(
		[2]string{"F", ""},
		[2]string{"F3", "F"},
		[2]string{"F30", "F"},
		[2]string{"F32", "F3"},
	))
	if err != nil {
		t.Fatal(err)
	}
	got := Slice(all, "F/F3")
	if len(got) != 2 {
		t.Fatalf("в срезе %d единиц, ожидалось 2: %v", len(got), got)
	}
	for _, u := range got {
		if u.Label == "F30" {
			t.Error("в срез F3 попал сосед F30")
		}
	}
}

func TestSliceПустойПутьДаётВсё(t *testing.T) {
	all, _ := BuildPaths(units([2]string{"A", ""}, [2]string{"B", ""}))
	if got := Slice(all, ""); len(got) != 2 {
		t.Errorf("в срезе %d единиц, ожидалось 2", len(got))
	}
}

func TestSliceПустойРезультатНеNil(t *testing.T) {
	// Пустой список — [], а не nil: он уедет в ответ ручки, и null вместо
	// списка роняет студию именно на исправных случаях.
	all, _ := BuildPaths(units([2]string{"A", ""}))
	if got := Slice(all, "нет-такого"); got == nil {
		t.Fatal("вернулся nil вместо пустого списка")
	}
}
