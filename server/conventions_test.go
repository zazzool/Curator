package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Соглашения, которые компилятор не держит.
//
// Проверка обходит дерево целиком, а не перечисляет известные места:
// перечень устаревает молча, и новый файл с той же ошибкой в него не
// попадёт никогда.

// TestОтсутствиеСтрокиСверяетсяЧерезErrorsIs.
//
// `err == pgx.ErrNoRows` работает ровно до тех пор, пока никто не обернул
// ошибку по дороге. Обернёт (`fmt.Errorf("… : %w", err)`) — и ветка «строки
// нет» перестанет срабатывать МОЛЧА: «врача не нашли» превратится в отказ
// с пятисотым, «знака пока нет» — в упавшую выдачу. Отказ при этом
// случится не у того, кто оборачивал, и не в том пакете.
func TestОтсутствиеСтрокиСверяетсяЧерезErrorsIs(t *testing.T) {
	// `!errors.Is(...)` содержит `pgx.ErrNoRows` тоже, поэтому ищем именно
	// сравнение знаком равенства.
	bad := regexp.MustCompile(`[!=]= pgx\.ErrNoRows`)

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Чужой код нам не чинить, а в проверках сравнение законно:
			// там ошибка приходит из соседней строки нераспакованной.
			if d.Name() == "vendor" || d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			if bad.MatchString(line) {
				t.Errorf("%s:%d сравнивает с pgx.ErrNoRows знаком равенства: "+
					"обёрнутая ошибка перестанет распознаваться молча", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
