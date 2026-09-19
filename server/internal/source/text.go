package source

import (
	"strings"
)

// SplitText режет текст или Markdown на куски по заголовкам.
//
// # Почему по заголовкам, а не по размеру
//
// Кусок уходит модели как единица работы, и резка по числу знаков рвёт
// положение пополам ровно там, где оно перестаёт быть понятным: половина
// критерия без второй половины выглядит законченной и толкует себя неверно.
// Заголовок же — это то место, где сам автор документа сказал «здесь новая
// мысль».
//
// У документа без заголовков весь текст — один кусок нулевого уровня. Это
// не отказ: приказ, присланный сплошняком, разбирать всё равно надо, просто
// делить его будет человек или модель, а не разметка.
//
// # Что считается заголовком
//
// Строка, начинающаяся с решёток Markdown. Подчёркнутые заголовки
// (=== и ---) не поддерживаются намеренно: их легко спутать с таблицей и с
// разделителем, а документы, которые к нам приходят, размечены решётками —
// так их отдаёт преобразователь DOCX.
func SplitText(text string) []Fragment {
	lines := strings.Split(text, "\n")

	out := []Fragment{}
	var cur *Fragment
	var body strings.Builder
	offset := 0

	flush := func(end int) {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(body.String())
		cur.CharTo = end
		// Заголовок без текста — это оглавление раздела, а не кусок:
		// отдать его модели значит попросить её писать по одному слову.
		// Но и выбрасывать его нельзя молча, если текста нет совсем, —
		// поэтому пустыми отбрасываются только куски, у которых нет ни
		// тела, ни заголовка.
		if cur.Body != "" || cur.Title != "" {
			out = append(out, *cur)
		}
		cur = nil
		body.Reset()
	}

	for _, line := range lines {
		lineStart := offset
		offset += len(line) + 1

		if level, title, ok := heading(line); ok {
			flush(lineStart)
			cur = &Fragment{Level: level, Title: title, CharFrom: lineStart}
			continue
		}
		if cur == nil {
			// Текст до первого заголовка — свой кусок: в приказах там
			// стоит преамбула, и терять её нельзя.
			cur = &Fragment{Level: 0, CharFrom: lineStart}
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	flush(len(text))
	return out
}

// heading разбирает строку заголовка Markdown.
//
// Требуется пробел после решёток: строка «#3.2 Порядок» — это не заголовок
// третьего уровня, а текст, начинающийся с номера, и принять её за
// заголовок значило бы порезать документ по нумерации пунктов.
func heading(line string) (level int, title string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	level = 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	// Шесть — предел разметки; дальше это уже не заголовок, а строка
	// решёток.
	if level > 6 || level >= len(trimmed) {
		return 0, "", false
	}
	if trimmed[level] != ' ' && trimmed[level] != '\t' {
		return 0, "", false
	}
	return level, strings.TrimSpace(trimmed[level:]), true
}
