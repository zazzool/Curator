package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Версии компиляторов названы в двух местах, и это неизбежно: сторож
// читает версию Go из `server/go.mod`, а `FROM` в Dockerfile читать
// оттуда нечем. Два места для одного расходятся молча — поэтому здесь
// они сверяются вслух.
//
// Проверка эта не про базу и потому не зовётся TestPg: она читает файлы
// и идёт всегда, в том числе там, где базы нет.

func TestВерсияGoВОбразеСходитсяСgomod(t *testing.T) {
	// Прежде в Dockerfile было написано, что версия берётся из go.mod, —
	// и это было неверно про ту же самую строку, что стояла ниже. Собрать
	// образ не тем компилятором можно молча: сборка пройдёт, а разойтись
	// они успеют на следующем обновлении.
	gomod := читай(t, "go.mod")
	m := regexp.MustCompile(`(?m)^go (\d+)\.(\d+)`).FindStringSubmatch(gomod)
	if m == nil {
		t.Fatal("в go.mod не нашлось строки версии Go")
	}
	хочу := m[1] + "." + m[2]

	dockerfile := читай(t, "../Dockerfile")
	d := regexp.MustCompile(`FROM golang:(\d+\.\d+)`).FindStringSubmatch(dockerfile)
	if d == nil {
		t.Fatal("в Dockerfile не нашлось строки FROM golang:")
	}
	if d[1] != хочу {
		t.Errorf("образ собирается компилятором %s, а go.mod требует %s — "+
			"сборка пройдёт, и узнают об этом не сегодня", d[1], хочу)
	}
}

func TestВерсияNodeВОбразеСходитсяСоСторожем(t *testing.T) {
	// У Node единственного места нет вовсе: её называют образ и сторож,
	// и больше назвать негде. Значит, сверяем их друг с другом.
	dockerfile := читай(t, "../Dockerfile")
	d := regexp.MustCompile(`FROM node:(\d+)`).FindStringSubmatch(dockerfile)
	if d == nil {
		t.Fatal("в Dockerfile не нашлось строки FROM node:")
	}

	workflow := читай(t, "../.github/workflows/checks.yml")
	w := regexp.MustCompile(`node-version:\s*'?(\d+)`).FindStringSubmatch(workflow)
	if w == nil {
		t.Fatal("у сторожа не нашлось строки node-version")
	}
	if d[1] != w[1] {
		t.Errorf("студию собирает Node %s, а проверяет Node %s: "+
			"проверенное и выкаченное собраны разным", d[1], w[1])
	}
}

func читай(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		// Отказ, а не пропуск: проверка, тихо пропустившая недоступный
		// файл, не сверяет ничего и выглядит при этом зелёной.
		t.Fatalf("%s не прочитан: %v", path, err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}
