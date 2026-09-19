package source

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// DocxToMarkdown вынимает из файла DOCX текст, размечая заголовки решётками.
//
// # Почему свой разбор, а не зависимость
//
// Правило проекта — не добавлять зависимости без нужды, и здесь нужды нет:
// DOCX это zip с XML, а обе вещи есть в стандартной библиотеке. Берём мы
// из документа мало — абзацы, их стиль и текст, — и ради этого тащить
// библиотеку, умеющую стили, сноски и колонтитулы, значит платить за то,
// чего не спрашивали.
//
// Заодно это отвечает на вопрос, почему не PDF: там пришлось бы разбирать
// сжатые потоки и шрифтовые таблицы, и своими силами это уже не делается.
// Перевод PDF в текст делает служба снаружи — это выбранная граница.
//
// # Что теряется намеренно
//
// Таблицы приезжают строками текста, без разметки. Картинки, сноски и
// колонтитулы не приезжают вовсе. Это осознанно: разбору нужен текст
// положений, а не вид страницы, и таблица, притворившаяся разметкой,
// путает модель сильнее, чем её отсутствие.
func DocxToMarkdown(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("файл не читается как DOCX: %w", err)
	}

	var body []byte
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("word/document.xml не открывается: %w", err)
		}
		body, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return "", fmt.Errorf("word/document.xml не дочитан: %w", err)
		}
		break
	}
	if body == nil {
		// Отказ, а не пустой текст: пустой текст работа примет за правду и
		// запишет пустоту как результат разбора. Это то же правило, что
		// «непонятое не применяется».
		return "", fmt.Errorf("в файле нет word/document.xml: это не документ Word")
	}
	return paragraphsToMarkdown(body)
}

// paragraphsToMarkdown проходит XML абзац за абзацем.
//
// Разбор потоковый, а не через полную модель документа: документ бывает в
// десятки мегабайт, и держать его разом в памяти незачем, когда нужен
// только текст.
func paragraphsToMarkdown(body []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))

	var out strings.Builder
	var para strings.Builder
	level := 0
	inParagraph := false
	// Текст берётся только из <w:t>, а не из всякого текстового узла
	// абзаца. Разница видна на документе, чей XML сохранён с отступами:
	// пробелы и переводы строк между тегами приехали бы внутрь положения
	// и разорвали бы слова.
	inText := false
	// Текст поля (instrText) — служебная запись вроде «PAGE \* MERGEFORMAT»,
	// а delText — то, что вычеркнул рецензент. Ни то, ни другое не текст
	// положения.
	skipDepth := 0

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("разметка документа испорчена: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if skipDepth > 0 {
				skipDepth++
				continue
			}
			switch t.Name.Local {
			case "p":
				inParagraph = true
				para.Reset()
				level = 0
			case "pStyle":
				level = headingLevel(attr(t, "val"))
			case "t":
				inText = true
			case "instrText", "delText":
				skipDepth = 1
			case "tab":
				para.WriteString(" ")
			case "br":
				para.WriteString("\n")
			}
		case xml.EndElement:
			if skipDepth > 0 {
				skipDepth--
				continue
			}
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if inParagraph {
					writeParagraph(&out, para.String(), level)
					inParagraph = false
				}
			}
		case xml.CharData:
			if skipDepth > 0 || !inParagraph || !inText {
				continue
			}
			para.Write(t)
		}
	}
	return out.String(), nil
}

// writeParagraph кладёт абзац в текст, размечая заголовок решётками.
//
// Пустые абзацы выбрасываются: в документах Word ими задают отступы, и
// десяток подряд превратился бы в десяток пустых строк, которые уехали бы
// модели как значимые. Разделение абзацев при этом не теряется — каждый
// непустой абзац и так кончается пустой строкой.
func writeParagraph(out *strings.Builder, text string, level int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if level > 0 {
		out.WriteString(strings.Repeat("#", level))
		out.WriteString(" ")
	}
	out.WriteString(text)
	out.WriteString("\n\n")
}

// headingLevel читает уровень заголовка из имени стиля.
//
// Имена стилей у Word зависят от языка, на котором документ создан:
// «Heading2» в английской сборке и «Заголовок2» в русской — один и тот же
// стиль. Проверяются оба, потому что документы приходят и такие и такие, а
// пропущенный заголовок означает, что разбор склеит два раздела в один.
func headingLevel(style string) int {
	lower := strings.ToLower(strings.TrimSpace(style))
	for _, prefix := range []string{"heading", "заголовок"} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		digits := strings.Trim(strings.TrimPrefix(lower, prefix), " -")
		n, err := strconv.Atoi(digits)
		if err != nil || n < 1 || n > 6 {
			return 0
		}
		return n
	}
	return 0
}

// attr — значение свойства без оглядки на пространство имён.
//
// Пространство имён у DOCX не одно: файлы, собранные разными редакторами,
// объявляют w: по-разному, и сверка полного имени отбрасывала бы исправные
// документы.
func attr(el xml.StartElement, name string) string {
	for _, a := range el.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
