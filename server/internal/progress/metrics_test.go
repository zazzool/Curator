package progress

import "testing"

func TestКаталогВеличинСходитсяСЭталономВОбеСтороны(t *testing.T) {
	// Сверка двусторонняя намеренно. Односторонняя пропускает ровно то,
	// ради чего заведена: величина, появившаяся в коде и не дописанная в
	// эталон, проходит молча — и считает её одно приложение, а другое о
	// ней не знает.
	f := load(t)
	if len(f.Metrics.Catalog) == 0 {
		t.Fatal("в общем эталоне нет каталога величин: сверять нечего")
	}

	code := Metrics()
	if len(code) != len(f.Metrics.Catalog) {
		t.Fatalf("величин в коде %d, в эталоне %d", len(code), len(f.Metrics.Catalog))
	}

	// Порядок тоже сверяется: каталог показывается врачу списком, и
	// порядок в нём — часть того, что видит человек.
	for i, want := range f.Metrics.Catalog {
		got := code[i]
		if string(got.Key) != want.Key {
			t.Errorf("величина %d: в коде %q, в эталоне %q", i, got.Key, want.Key)
		}
		if got.Title != want.Title {
			t.Errorf("величина %q: название в коде %q, в эталоне %q",
				want.Key, got.Title, want.Title)
		}
	}

	for _, want := range f.Metrics.Catalog {
		if !KnownMetric(MetricKey(want.Key)) {
			t.Errorf("величина %q есть в эталоне и неизвестна коду", want.Key)
		}
	}
}

func TestНеизвестнаяВеличинаНеПризнаётся(t *testing.T) {
	// Закрытость словаря и есть то, что делает его дешёвым: величина,
	// принятая «для гибкости», перестаёт быть общей с приложением.
	if KnownMetric("придумана-по-месту") {
		t.Error("каталог принял величину, которой в нём нет")
	}
}
