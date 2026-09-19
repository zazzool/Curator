package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Сверка словаря событий с общим эталоном.
//
// События шлёт приложение, а считает по ним сервер. Два словаря, набранных
// порознь, расходятся молча: приложение шлёт case_skip, сервер ждёт
// case_skipped — и событие тихо отбрасывается, а отчёт показывает ноль
// пропусков. Ноль выглядит как данные.
//
// Построчной сверки с исходниками приложения здесь пока нет: самого
// приложения ещё нет. Она обязана появиться вместе с ним, и это записано в
// эталоне, а не оставлено на память.

type catalogFile struct {
	Events []struct {
		Name  string   `json:"name"`
		Title string   `json:"title"`
		Props []string `json:"props"`
	} `json:"events"`
}

func loadCatalog(t *testing.T) catalogFile {
	t.Helper()
	path := filepath.Join("..", "..", "..", "shared", "telemetry-events.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("общий словарь событий %s не прочитан: %v. "+
			"Это отказ, а не пропуск: без него словарь сервера и словарь "+
			"приложения расходятся молча", path, err)
	}
	var out catalogFile
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("общий словарь событий не разобран: %v", err)
	}
	if len(out.Events) == 0 {
		t.Fatal("в общем словаре нет ни одного события: сверять нечего")
	}
	return out
}

func TestСловарьСобытийСходитсяСЭталоном(t *testing.T) {
	f := loadCatalog(t)
	got := Catalog()

	if len(got) != len(f.Events) {
		t.Fatalf("в словаре сервера %d событий, в эталоне %d", len(got), len(f.Events))
	}
	for i, want := range f.Events {
		// Порядок сверяется тоже: он же порядок разделов в отчётах, и в
		// нём читается путь врача — от запуска к разбору, от разбора к
		// покупке. Пересортированный словарь ломает отчёт молча.
		one := got[i]
		if one.Name != want.Name || one.Title != want.Title {
			t.Errorf("событие %d: %q/%q, в эталоне %q/%q",
				i, one.Name, one.Title, want.Name, want.Title)
			continue
		}
		if len(one.Props) != len(want.Props) {
			t.Errorf("у события %s свойств %d, в эталоне %d",
				one.Name, len(one.Props), len(want.Props))
			continue
		}
		for j, prop := range want.Props {
			if one.Props[j] != prop {
				t.Errorf("у события %s свойство %d: %q, в эталоне %q",
					one.Name, j, one.Props[j], prop)
			}
		}
	}
}

func TestСловарьСобытийЗакрытИБезПовторов(t *testing.T) {
	seen := map[string]bool{}
	for _, one := range Catalog() {
		if one.Name == "" || one.Title == "" {
			t.Errorf("событие без имени или названия: %+v", one)
		}
		if seen[one.Name] {
			t.Errorf("событие %q объявлено дважды", one.Name)
		}
		seen[one.Name] = true

		props := map[string]bool{}
		for _, prop := range one.Props {
			if props[prop] {
				t.Errorf("у события %s свойство %q объявлено дважды", one.Name, prop)
			}
			props[prop] = true
		}
	}

	if _, ok := Known("такого-события-нет"); ok {
		t.Error("словарь принял имя, которого в нём нет")
	}
}

func TestОтветНеЕдетТелеметрией(t *testing.T) {
	// Что врач ответил и верно ли — это попытка, и у неё своя дверь.
	// Запиши мы ответ обоими путями, два счёта решённых задач разошлись
	// бы молча, и разбираться в этом пришлось бы по журналу.
	for _, one := range Catalog() {
		for _, prop := range one.Props {
			if prop == "correct" || prop == "answer" {
				t.Errorf("событие %s несёт свойство %q — это дело попытки, а не телеметрии",
					one.Name, prop)
			}
		}
	}
}
