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

// TestУборщикиЗовутсяИзСлужбы.
//
// Уборщик — это метод, которого никто не ждёт: не позови его, и не упадёт
// ничего, просто начнёт копиться то, ради чего он написан. Два таких
// аудит и нашёл — `Sessions.Sweep` и `llmusage.Store.Sweep`: оба
// написаны, оба объяснены пояснением, зачем нужны, и оба звались ТОЛЬКО
// из проверок. Таблица сессий становилась журналом входов, которого никто
// не заводил, а в llm_calls вечно лежали полные тела запросов и ответов
// модели.
//
// Уборщики ищутся по объявлению, а не по списку имён: написанный завтра
// третий попадёт под проверку сам.
//
// # Почему считаются зовущие, а не «зовут ли вообще»
//
// Текстом receiver в месте зова не различить: объявлено `func (s
// *Sessions) Sweep`, а позвано `ledger.Sweep(…)` — по имени метода оба
// уборщика одинаковы, и забытый прикрывался бы позванным. Поэтому
// сверяется счёт: разных зовущих обязано быть не меньше, чем объявлений
// с этим именем. Снятый зов роняет проверку, даже когда однофамилец на
// месте.
//
// # Чего она не видит, и это надо знать
//
// Обёртку, которая зовёт уборщика и которую саму никто не зовёт, она
// пропустит: для этого нужен обход графа вызовов от main, то есть ещё
// один сторонний набор в сторож. Названо вслух затем, чтобы зелёная
// проверка не читалась как «уборка идёт»: она говорит ровно «уборщик не
// забыт».
func TestУборщикиЗовутсяИзСлужбы(t *testing.T) {
	declared := regexp.MustCompile(`^func \([^)]+\) (Sweep\w*)\(`)
	call := regexp.MustCompile(`(\w+)\.(Sweep\w*)\(`)
	sweepers := map[string][]string{}       // имя метода → где объявлен
	callers := map[string]map[string]bool{} // имя метода → кто зовёт

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			// Зов из проверки не считается: проверка и была тем
			// единственным местом, где оба уборщика звались.
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(body), "\n") {
			if m := declared.FindStringSubmatch(line); m != nil {
				sweepers[m[1]] = append(sweepers[m[1]], path)
				continue
			}
			for _, m := range call.FindAllStringSubmatch(line, -1) {
				if callers[m[2]] == nil {
					callers[m[2]] = map[string]bool{}
				}
				callers[m[2]][m[1]] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sweepers) == 0 {
		t.Fatal("не найдено ни одного уборщика: проверка перестала искать")
	}
	for name, where := range sweepers {
		if len(callers[name]) < len(where) {
			t.Errorf("объявлений %s — %d (%s), а зовущих из службы — %d: "+
				"не позванный уборщик не падает, он копит",
				name, len(where), strings.Join(where, ", "), len(callers[name]))
		}
	}
}
