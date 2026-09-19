package progress

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// Сверка расчёта с общим эталоном.
//
// # Отсутствие файла роняет проверку, а не пропускает её
//
// Эталон — единственное, что держит вместе два расчёта: серверный и тот,
// что живёт в приложении. Проверка, молча пропустившаяся из-за пропавшего
// файла, выдала бы зелёное за непроверенное ровно там, где расхождение
// невидимо до встречи с врачом: на устройстве один уровень, в отчёте
// другой.

type fixtures struct {
	Levels struct {
		Thresholds []int64 `json:"thresholds"`
		Cases      []struct {
			XP    int64 `json:"xp"`
			Level int   `json:"level"`
		} `json:"cases"`
	} `json:"levels"`

	XP struct {
		Correct       int64 `json:"correct"`
		Wrong         int64 `json:"wrong"`
		RepeatCorrect int64 `json:"repeatCorrect"`
		RepeatWrong   int64 `json:"repeatWrong"`
		ReviewCorrect int64 `json:"reviewCorrect"`
		ReviewWrong   int64 `json:"reviewWrong"`
		Cases         []struct {
			Correct bool  `json:"correct"`
			Repeat  bool  `json:"repeat"`
			Review  bool  `json:"review"`
			XP      int64 `json:"xp"`
		} `json:"cases"`
	} `json:"xp"`

	Review struct {
		EaseStart      float64 `json:"easeStart"`
		EaseFloor      float64 `json:"easeFloor"`
		FirstInterval  int     `json:"firstInterval"`
		SecondInterval int     `json:"secondInterval"`
		Cases          []struct {
			Why     string `json:"why"`
			Before  state  `json:"before"`
			Correct bool   `json:"correct"`
			After   state  `json:"after"`
		} `json:"cases"`
	} `json:"review"`

	Metrics struct {
		Catalog []struct {
			Key   string `json:"key"`
			Title string `json:"title"`
		} `json:"catalog"`
	} `json:"metrics"`
}

type state struct {
	Ease         float64 `json:"ease"`
	IntervalDays int     `json:"intervalDays"`
	Repetitions  int     `json:"repetitions"`
}

func load(t *testing.T) fixtures {
	t.Helper()
	path := filepath.Join("..", "..", "..", "shared", "progress-fixtures.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("общий эталон расчёта %s не прочитан: %v. "+
			"Это отказ, а не пропуск: без эталона два расчёта — серверный и в "+
			"приложении — расходятся молча", path, err)
	}
	var out fixtures
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("общий эталон расчёта не разобран: %v", err)
	}
	if len(out.Levels.Cases) == 0 || len(out.XP.Cases) == 0 || len(out.Review.Cases) == 0 {
		t.Fatal("в общем эталоне нет примеров: сверять нечего")
	}
	return out
}

func TestПравилаПоУмолчаниюСходятсяСЭталоном(t *testing.T) {
	// Числа в коде нужны, чтобы свежая установка работала до первой
	// настройки. Разойдись они с эталоном — и свежая установка считала бы
	// иначе, чем настроенная, а заметили бы это по чужому уровню в отчёте.
	f := load(t)
	r := Default()

	if len(r.Thresholds) != len(f.Levels.Thresholds) {
		t.Fatalf("порогов уровней %d, в эталоне %d", len(r.Thresholds), len(f.Levels.Thresholds))
	}
	for i, want := range f.Levels.Thresholds {
		if r.Thresholds[i] != want {
			t.Errorf("порог уровня %d: %d, в эталоне %d", i+1, r.Thresholds[i], want)
		}
	}
	for _, c := range []struct {
		name      string
		got, want int64
	}{
		{"correct", r.Correct, f.XP.Correct},
		{"wrong", r.Wrong, f.XP.Wrong},
		{"repeatCorrect", r.RepeatCorrect, f.XP.RepeatCorrect},
		{"repeatWrong", r.RepeatWrong, f.XP.RepeatWrong},
		{"reviewCorrect", r.ReviewCorrect, f.XP.ReviewCorrect},
		{"reviewWrong", r.ReviewWrong, f.XP.ReviewWrong},
	} {
		if c.got != c.want {
			t.Errorf("награда %s: %d, в эталоне %d", c.name, c.got, c.want)
		}
	}
	if r.EaseStart != f.Review.EaseStart || r.EaseFloor != f.Review.EaseFloor {
		t.Errorf("лёгкость: начало %v пол %v, в эталоне %v и %v",
			r.EaseStart, r.EaseFloor, f.Review.EaseStart, f.Review.EaseFloor)
	}
	if r.FirstInterval != f.Review.FirstInterval || r.SecondInterval != f.Review.SecondInterval {
		t.Errorf("интервалы %d и %d, в эталоне %d и %d",
			r.FirstInterval, r.SecondInterval, f.Review.FirstInterval, f.Review.SecondInterval)
	}
}

func TestУровниСходятсяСЭталоном(t *testing.T) {
	f := load(t)
	r := Default()
	for _, c := range f.Levels.Cases {
		if got := r.Level(c.XP); got != c.Level {
			t.Errorf("при опыте %d уровень %d, в эталоне %d", c.XP, got, c.Level)
		}
	}
}

func TestНаградаЗаПопыткуСходитсяСЭталоном(t *testing.T) {
	f := load(t)
	r := Default()
	for _, c := range f.XP.Cases {
		got := r.Award(c.Correct, c.Repeat, c.Review)
		if got != c.XP {
			t.Errorf("верно=%v повтор=%v интервал=%v: награда %d, в эталоне %d",
				c.Correct, c.Repeat, c.Review, got, c.XP)
		}
	}
}

func TestПовторениеСходитсяСЭталоном(t *testing.T) {
	// Здесь и ловится расхождение двух реализаций: SM-2 легко написать
	// «почти так же», и разница вылезет на третьем повторении, через две
	// недели после выкатки.
	f := load(t)
	r := Default()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	for _, c := range f.Review.Cases {
		got := r.Next(State{
			Ease:         c.Before.Ease,
			IntervalDays: c.Before.IntervalDays,
			Repetitions:  c.Before.Repetitions,
		}, c.Correct, now)

		if got.Ease != c.After.Ease {
			t.Errorf("%s: лёгкость %v, в эталоне %v", c.Why, got.Ease, c.After.Ease)
		}
		if got.IntervalDays != c.After.IntervalDays {
			t.Errorf("%s: интервал %d, в эталоне %d", c.Why, got.IntervalDays, c.After.IntervalDays)
		}
		if got.Repetitions != c.After.Repetitions {
			t.Errorf("%s: повторений %d, в эталоне %d", c.Why, got.Repetitions, c.After.Repetitions)
		}
		// Срок назначается от интервала, а не сам по себе: разойдись они —
		// и задача вернулась бы не тогда, когда обещано.
		if want := now.AddDate(0, 0, c.After.IntervalDays); !got.DueAt.Equal(want) {
			t.Errorf("%s: срок %v, ожидался %v", c.Why, got.DueAt, want)
		}
	}
}

func TestОшибкаНеСтираетНакопленнуюЛёгкость(t *testing.T) {
	// Лёгкость — свойство задачи для этого врача, накопленное за месяцы.
	// Сбросить её к началу из-за одной ошибки значит потерять накопленное
	// на пустом месте, и заметить это нельзя ничем, кроме этой проверки.
	r := Default()
	now := time.Now()

	after := r.Next(State{Ease: 1.7, IntervalDays: 30, Repetitions: 5}, false, now)
	if after.Ease == r.EaseStart {
		t.Fatal("ошибка сбросила лёгкость к началу")
	}
	if after.Ease >= 1.7 {
		t.Fatalf("ошибка не понизила лёгкость: %v", after.Ease)
	}
	if after.Repetitions != 0 || after.IntervalDays != r.FirstInterval {
		t.Fatalf("ошибка не сбросила счёт повторений: %+v", after)
	}
}

func TestЛёгкостьНеПроваливаетсяНижеПола(t *testing.T) {
	// На меньшей задача возвращается каждый день и превращает повторение
	// в наказание.
	r := Default()
	now := time.Now()

	s := r.Fresh()
	for i := 0; i < 50; i++ {
		s = r.Next(s, false, now)
	}
	if s.Ease < r.EaseFloor {
		t.Fatalf("лёгкость провалилась до %v при поле %v", s.Ease, r.EaseFloor)
	}
}

func TestКаталогВеличинЗакрытИНеПуст(t *testing.T) {
	// Словарь закрыт и зеркален с приложением: величина, появившаяся
	// строкой по месту, считалась бы одним приложением и не считалась бы
	// другим — и разошлись бы они молча.
	//
	// Построчная сверка с исходником приложения стоит отдельно —
	// TestКаталогВеличинСходитсяСИсходникомПриложения. Эталон держит
	// смысл, она — то, что приложение действительно умеет считать.
	f := load(t)
	if len(f.Metrics.Catalog) == 0 {
		t.Fatal("каталог величин пуст")
	}
	seen := map[string]bool{}
	for _, m := range f.Metrics.Catalog {
		if m.Key == "" || m.Title == "" {
			t.Errorf("величина без ключа или названия: %+v", m)
		}
		if seen[m.Key] {
			t.Errorf("величина %q объявлена дважды", m.Key)
		}
		seen[m.Key] = true
	}
}

// mirror читает исходник приложения и отдаёт строки, попавшие под образец.
//
// Отказ, а не пропуск: сверка, зовущая «пропустить» на недоступном
// исходнике, не сверяет ничего, а выглядит зелёной ровно так же, как
// сверка прошедшая.
func mirror(t *testing.T, path string, pattern *regexp.Regexp) [][]string {
	t.Helper()
	full := filepath.Join("..", "..", "..", "app", path)
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("исходник приложения %s не прочитан: %v. "+
			"Это отказ, а не пропуск: словарь, зеркальный только на словах, "+
			"расходится молча", full, err)
	}
	found := pattern.FindAllStringSubmatch(string(raw), -1)
	if len(found) == 0 {
		t.Fatalf("в %s не нашлось ни одной строки по образцу %s: "+
			"либо каталог переехал, либо его переписали иначе — и сверять "+
			"стало нечего", full, pattern)
	}
	return found
}

func TestКаталогВеличинСходитсяСИсходникомПриложения(t *testing.T) {
	// Здесь сверка построчная, а не через эталон: эталон держит СМЫСЛ, а
	// эта проверка держит то, что приложение действительно умеет считать.
	// Каталог, разошедшийся с эталоном на одной стороне, эталонная сверка
	// поймает; каталог, у которого эталон правили вместе с одной из
	// сторон, — только эта.
	found := mirror(t, filepath.Join("lib", "progress", "metrics.dart"),
		regexp.MustCompile(`Metric\('([^']+)', '([^']+)'\)`))

	want := Metrics()
	if len(found) != len(want) {
		t.Fatalf("в каталоге приложения %d величин, в серверном %d", len(found), len(want))
	}
	for i, one := range found {
		// Порядок сверяется тоже: он задан эталоном, и разошедшийся
		// порядок означает, что один из каталогов правили мимо эталона.
		if one[1] != string(want[i].Key) || one[2] != want[i].Title {
			t.Errorf("величина %d в приложении %q/%q, на сервере %q/%q",
				i, one[1], one[2], want[i].Key, want[i].Title)
		}
	}
}
