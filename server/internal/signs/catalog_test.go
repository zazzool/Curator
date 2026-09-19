package signs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"curator/server/internal/progress"
)

// Сверка каталога знаков с общим эталоном.
//
// Знак выдаёт сервер, а показывает приложение, и показать оно обязано то
// же самое. Два каталога, набранных порознь, расходятся молча — и первым
// это увидит врач, у которого на экране знак называется иначе, чем в
// свидетельстве.

type catalogFile struct {
	Kinds struct {
		List []string `json:"list"`
	} `json:"kinds"`
	Catalog []struct {
		Slug        string   `json:"slug"`
		Title       string   `json:"title"`
		Kind        string   `json:"kind"`
		Metric      string   `json:"metric"`
		Threshold   int64    `json:"threshold"`
		Requires    []string `json:"requires"`
		EditionSize int      `json:"editionSize"`
		XP          int64    `json:"xp"`
	} `json:"catalog"`
}

func loadCatalog(t *testing.T) catalogFile {
	t.Helper()
	path := filepath.Join("..", "..", "..", "shared", "signs-catalog.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("общий каталог знаков %s не прочитан: %v. "+
			"Это отказ, а не пропуск: без него каталог сервера и каталог "+
			"приложения расходятся молча", path, err)
	}
	var out catalogFile
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("общий каталог знаков не разобран: %v", err)
	}
	if len(out.Catalog) == 0 {
		t.Fatal("в общем каталоге нет ни одного знака: сверять нечего")
	}
	return out
}

func TestКаталогЗнаковСходитсяСЭталономВОбеСтороны(t *testing.T) {
	f := loadCatalog(t)
	code := Catalog()

	if len(code) != len(f.Catalog) {
		t.Fatalf("знаков в коде %d, в эталоне %d", len(code), len(f.Catalog))
	}
	// Порядок тоже сверяется: знаки показываются списком, и порядок в нём
	// — часть того, что видит человек.
	for i, want := range f.Catalog {
		got := code[i]
		if got.Slug != want.Slug {
			t.Errorf("знак %d: в коде %q, в эталоне %q", i, got.Slug, want.Slug)
			continue
		}
		if got.Title != want.Title {
			t.Errorf("%s: название в коде %q, в эталоне %q", want.Slug, got.Title, want.Title)
		}
		if string(got.Kind) != want.Kind {
			t.Errorf("%s: вид в коде %q, в эталоне %q", want.Slug, got.Kind, want.Kind)
		}
		if string(got.Metric) != want.Metric {
			t.Errorf("%s: величина в коде %q, в эталоне %q", want.Slug, got.Metric, want.Metric)
		}
		if got.Threshold != want.Threshold {
			t.Errorf("%s: порог в коде %d, в эталоне %d", want.Slug, got.Threshold, want.Threshold)
		}
		if got.EditionSize != want.EditionSize {
			t.Errorf("%s: тираж в коде %d, в эталоне %d", want.Slug, got.EditionSize, want.EditionSize)
		}
		if got.XP != want.XP {
			t.Errorf("%s: опыт в коде %d, в эталоне %d", want.Slug, got.XP, want.XP)
		}
		if len(got.Requires) != len(want.Requires) {
			t.Errorf("%s: частей в коде %d, в эталоне %d",
				want.Slug, len(got.Requires), len(want.Requires))
			continue
		}
		for j := range want.Requires {
			if got.Requires[j] != want.Requires[j] {
				t.Errorf("%s: часть %d в коде %q, в эталоне %q",
					want.Slug, j, got.Requires[j], want.Requires[j])
			}
		}
	}

	for _, want := range f.Catalog {
		if !Known(want.Slug) {
			t.Errorf("знак %q есть в эталоне и неизвестен коду", want.Slug)
		}
	}
}

func TestВидыЗнаковЗакрыты(t *testing.T) {
	// Вид, заведённый по месту, приложение не нарисует, а сервер выдаст —
	// и знак повиснет невидимым.
	f := loadCatalog(t)
	known := map[string]bool{}
	for _, kind := range f.Kinds.List {
		known[kind] = true
	}
	if len(known) == 0 {
		t.Fatal("в эталоне не перечислены виды знаков")
	}
	for _, s := range Catalog() {
		if !known[string(s.Kind)] {
			t.Errorf("знак %q объявлен видом %q, которого нет в словаре", s.Slug, s.Kind)
		}
	}
}

func TestКаждыйЗнакСсылаетсяНаСуществующее(t *testing.T) {
	// Знак за величину, которой нет в каталоге величин, не выдастся
	// никогда и промолчит об этом. Собирательный знак, требующий
	// несуществующей части, — тоже.
	all := map[string]bool{}
	for _, s := range Catalog() {
		all[s.Slug] = true
	}
	for _, s := range Catalog() {
		switch s.Kind {
		case KindCollective:
			if len(s.Requires) == 0 {
				t.Errorf("собирательный знак %q не требует ничего", s.Slug)
			}
			for _, part := range s.Requires {
				if !all[part] {
					t.Errorf("знак %q требует %q, которого в каталоге нет", s.Slug, part)
				}
			}
		default:
			if !progress.KnownMetric(s.Metric) {
				t.Errorf("знак %q стоит на величине %q, которой нет в каталоге величин",
					s.Slug, s.Metric)
			}
			if s.Threshold <= 0 {
				t.Errorf("знак %q с порогом %d выдался бы всякому", s.Slug, s.Threshold)
			}
		}
	}
}

func TestДоляПутиНеВыходитЗаГраницы(t *testing.T) {
	s := Sign{Kind: KindMetric, Metric: progress.CasesSolved, Threshold: 10}
	cases := []struct {
		have int64
		want float64
	}{{0, 0}, {5, 0.5}, {10, 1}, {1000, 1}}
	for _, one := range cases {
		got := s.Progress(map[progress.MetricKey]int64{progress.CasesSolved: one.have})
		if got != one.want {
			t.Errorf("при %d доля %v, ожидалась %v", one.have, got, one.want)
		}
	}
}

func TestСобирательныйЗнакНеЗаслуживаетсяВеличиной(t *testing.T) {
	// Он смотрит на выданные знаки, и отвечать на вопрос про величины ему
	// нечем. Ответ «да» по пустому порогу выдал бы орден всякому.
	s := Sign{Slug: "order", Kind: KindCollective, Requires: []string{"a"}}
	if s.Earned(map[progress.MetricKey]int64{progress.CasesSolved: 1000000}) {
		t.Error("собирательный знак заслужился величиной")
	}
}
