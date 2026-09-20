package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestПотолокВремениСходитсяСПриложением.
//
// Потолок стоит по обе стороны провода намеренно (довод — у maxSpentMs в
// attempts.go), и потому это ДВА места для одного числа. Разойдутся они
// молча: обрежет приложение по своему, сервер по своему, и заметить это
// можно будет только по сводке решаемости — то есть тогда, когда числа
// уже собраны.
func TestПотолокВремениСходитсяСПриложением(t *testing.T) {
	path := filepath.Join("..", "..", "..", "app", "lib", "cases", "outbox.dart")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("исходник приложения %s не прочитан: %v. "+
			"Это отказ, а не пропуск: два числа, сверяемые только на словах, "+
			"расходятся молча", path, err)
	}

	m := regexp.MustCompile(`maxSpentMs\s*=\s*([0-9*\s]+);`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("в приложении не нашлось потолка maxSpentMs: либо он исчез, " +
			"либо записан иначе — и сверять стало нечего")
	}

	// Число записано произведением («10 * 60 * 1000») ради читаемости, и
	// сверять надо значение, а не запись: 600000 и 10*60*1000 — одно и то
	// же, а разные строки.
	got := int64(1)
	for _, part := range regexp.MustCompile(`\s*\*\s*`).Split(string(m[1]), -1) {
		n, err := strconv.ParseInt(regexp.MustCompile(`\s`).ReplaceAllString(part, ""), 10, 64)
		if err != nil {
			t.Fatalf("потолок приложения %q не разобран как число: %v", m[1], err)
		}
		got *= n
	}

	if got != maxSpentMs {
		t.Errorf("приложение обрезает время по %d, сервер по %d", got, maxSpentMs)
	}
}

func TestВремяНадЗадачейОбрезаетсяНаСервере(t *testing.T) {
	// Присылает его устройство, а устройству мы не верим: на руках у
	// врачей стоят сборки, которые обновятся не завтра, и обрезка в
	// приложении доедет до них тогда же.
	cases := []struct {
		name string
		in   int64
		want int64
	}{
		{"ночь над задачей", 9 * 60 * 60 * 1000, maxSpentMs},
		{"часы перевели назад", -5000, 0},
		{"обычный ответ", 12_000, 12_000},
		{"ровно по границе", maxSpentMs, maxSpentMs},
	}
	for _, one := range cases {
		if got := clampSpent(one.in); got != one.want {
			t.Errorf("%s: %d обрезалось в %d, ожидалось %d",
				one.name, one.in, got, one.want)
		}
	}
}
