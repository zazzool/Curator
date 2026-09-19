package source

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Формат принесённого файла.
//
// Словарь закрыт, и это та же закрытость, что у вида источника: формат,
// появившийся строкой по месту, обошёл бы отказ по PDF ниже.
type Format string

const (
	FormatText     Format = "text"
	FormatMarkdown Format = "markdown"
	FormatDocx     Format = "docx"
)

// MaxDocumentBytes — потолок размера документа.
//
// Потолок нужен не ради места в базе, а ради разбора: документ целиком
// поднимается в память и режется на куски, и файл в сотню мегабайт положил
// бы сервер на ровном месте. Тридцать мегабайт — это приказ на тысячу
// страниц с запасом; всё, что больше, приносят по частям.
const MaxDocumentBytes = 30 << 20

// ErrPDF — отказ по PDF.
//
// Отдельной ошибкой, а не текстом по месту: отказ по PDF — выбранная
// граница проекта, и текст у неё один. Обещания «позже» в нём нет
// намеренно: обещание, которое никто не собирается исполнять, отправляет
// человека ждать вместо того, чтобы перевести файл.
var ErrPDF = errors.New("PDF не принимается: переведите файл в текст, Markdown или DOCX и принесите снова")

// Upload — принесённый файл до разбора.
type Upload struct {
	Filename string
	Body     []byte
}

// Prepared — файл, доведённый до разбора.
type Prepared struct {
	Format   Format
	MIME     string
	SHA256   string
	Markdown string
}

// Prepare узнаёт формат файла, доводит его до Markdown и считает отпечаток.
//
// # Почему формат берётся из имени, а не из содержимого
//
// Угадывание по первым байтам различает DOCX и текст, но не различает
// текст и Markdown, а именно это различие видно человеку: он принёс .md и
// ждёт, что заголовки станут заголовками. Имя файла человек назвал сам, и
// доверять здесь его слову честнее, чем догадке. Единственное, что
// проверяется по содержимому, — PDF: его приносят с любым расширением, и
// отказ по расширению человек обходит переименованием, получая на выходе
// кашу из двоичного мусора.
func Prepare(up Upload) (Prepared, error) {
	if len(up.Body) == 0 {
		return Prepared{}, errors.New("файл пуст")
	}
	if len(up.Body) > MaxDocumentBytes {
		return Prepared{}, fmt.Errorf("файл больше %d МБ: принесите его по частям", MaxDocumentBytes>>20)
	}
	if isPDF(up.Body) || extension(up.Filename) == "pdf" {
		return Prepared{}, ErrPDF
	}

	sum := sha256.Sum256(up.Body)
	out := Prepared{SHA256: hex.EncodeToString(sum[:])}

	switch extension(up.Filename) {
	case "docx":
		md, err := DocxToMarkdown(up.Body)
		if err != nil {
			return Prepared{}, err
		}
		out.Format, out.MIME, out.Markdown = FormatDocx,
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document", md
	case "md", "markdown":
		if err := mustBeText(up.Body); err != nil {
			return Prepared{}, err
		}
		out.Format, out.MIME, out.Markdown = FormatMarkdown, "text/markdown; charset=utf-8", string(up.Body)
	case "txt", "text", "":
		if err := mustBeText(up.Body); err != nil {
			return Prepared{}, err
		}
		out.Format, out.MIME, out.Markdown = FormatText, "text/plain; charset=utf-8", string(up.Body)
	default:
		return Prepared{}, fmt.Errorf("файлы %q не принимаются: принесите текст, Markdown или DOCX", extension(up.Filename))
	}
	return out, nil
}

// mustBeText отказывает на двоичном файле, названном текстовым.
//
// Иначе двоичный мусор доедет до разбора и до модели: она честно найдёт в
// нём «положения», а человек получит источник из шума. Отказ при загрузке
// дешевле любой проверки ниже по пути.
func mustBeText(body []byte) error {
	if !utf8.Valid(body) {
		return errors.New("файл не читается как текст: сохраните его в кодировке UTF-8")
	}
	return nil
}

// isPDF смотрит на подпись файла.
func isPDF(body []byte) bool {
	return len(body) >= 5 && string(body[:5]) == "%PDF-"
}

// extension — расширение в нижнем регистре, без точки.
func extension(filename string) string {
	i := strings.LastIndex(filename, ".")
	if i < 0 || i == len(filename)-1 {
		return ""
	}
	return strings.ToLower(filename[i+1:])
}
