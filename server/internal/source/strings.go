package source

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Мелкие обёртки над strings, чтобы разбор читался в одном стиле. Отдельным
// файлом, а не в каждом месте: одно имя одного действия.

func trim(s string) string { return strings.TrimSpace(s) }

func repeat(s string, n int) string { return strings.Repeat(s, n) }

// escapeLike обезвреживает знаки, значимые для LIKE.
//
// В метке приказа подчёркивание и процент вполне возможны, а для LIKE это
// «любой знак» и «любая строка». Без экранирования срез по пути «п_1»
// захватил бы «п-1» и «п.1» — то есть отдал бы чужие единицы, и молча.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// capitalize поднимает первую букву.
//
// Отказы собираются из кусков («источник %q не заведён: …»), и наружу
// уезжает готовая фраза. С маленькой буквы она читается как обрывок
// журнала, а человеку показывают предложение.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
