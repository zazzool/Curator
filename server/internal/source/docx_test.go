package source

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

// docx собирает настоящий файл DOCX из тела документа.
//
// Образец в testdata не кладётся намеренно: двоичный файл в дереве нельзя
// прочитать глазами, и через полгода никто не скажет, что именно в нём
// проверяется. Здесь же условие проверки видно целиком.
func docx(t *testing.T, documentXML string, extra map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if documentXML != "" {
		write("word/document.xml", documentXML)
	}
	for name, body := range extra {
		write(name, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const docHeader = `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`
const docFooter = `</w:body></w:document>`

func TestDocxРазмечаетЗаголовки(t *testing.T) {
	data := docx(t, docHeader+
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Порядок оказания</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>Помощь оказывается в виде…</w:t></w:r></w:p>`+
		docFooter, nil)

	got, err := DocxToMarkdown(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "# Порядок оказания") {
		t.Errorf("заголовок не размечен: %q", got)
	}
	if !strings.Contains(got, "Помощь оказывается") {
		t.Errorf("текст потерян: %q", got)
	}
}

func TestDocxПониматРусскоеИмяСтиля(t *testing.T) {
	// Word называет стиль по языку сборки, и документы приходят и такие и
	// такие. Пропущенный заголовок склеил бы два раздела в один.
	data := docx(t, docHeader+
		`<w:p><w:pPr><w:pStyle w:val="Заголовок2"/></w:pPr><w:r><w:t>Раздел</w:t></w:r></w:p>`+
		docFooter, nil)
	got, err := DocxToMarkdown(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "## Раздел") {
		t.Errorf("русское имя стиля не понято: %q", got)
	}
}

func TestDocxСклеиваетКускиОдногоАбзаца(t *testing.T) {
	// Word рвёт абзац на отрезки по любому поводу — правке, проверке
	// орфографии, смене шрифта. Склеивать их обязательно: иначе одно
	// положение приедет разорванным на слова.
	data := docx(t, docHeader+
		`<w:p><w:r><w:t>Диагноз ставится </w:t></w:r><w:r><w:t>при наличии </w:t></w:r>`+
		`<w:r><w:t>двух признаков</w:t></w:r></w:p>`+
		docFooter, nil)
	got, err := DocxToMarkdown(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Диагноз ставится при наличии двух признаков") {
		t.Errorf("абзац приехал разорванным: %q", got)
	}
}

func TestDocxВыбрасываетСлужебныеПоля(t *testing.T) {
	data := docx(t, docHeader+
		`<w:p><w:r><w:instrText>PAGE \* MERGEFORMAT</w:instrText></w:r><w:r><w:t>Текст положения</w:t></w:r></w:p>`+
		docFooter, nil)
	got, err := DocxToMarkdown(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "MERGEFORMAT") {
		t.Errorf("служебное поле попало в текст: %q", got)
	}
	if !strings.Contains(got, "Текст положения") {
		t.Errorf("текст потерян: %q", got)
	}
}

func TestDocxБезДокументаОтказывает(t *testing.T) {
	// Отказ, а не пустой текст: пустоту работа примет за правду и запишет
	// её как результат разбора.
	data := docx(t, "", map[string]string{"word/settings.xml": "<settings/>"})
	if _, err := DocxToMarkdown(data); err == nil {
		t.Fatal("файл без word/document.xml принят")
	}
}

func TestDocxНеZipОтказывает(t *testing.T) {
	if _, err := DocxToMarkdown([]byte("это просто текст, а не docx")); err == nil {
		t.Fatal("не-DOCX принят")
	}
}

func TestDocxЛожитсяВРазборПоЗаголовкам(t *testing.T) {
	// Связка двух шагов: DOCX превращается в Markdown, а Markdown режется
	// на куски. Проверяется она здесь, потому что ошибка на стыке — самая
	// вероятная: каждый шаг по отдельности исправен.
	data := docx(t, docHeader+
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Первый</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>тело первого</w:t></w:r></w:p>`+
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Второй</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>тело второго</w:t></w:r></w:p>`+
		docFooter, nil)
	md, err := DocxToMarkdown(data)
	if err != nil {
		t.Fatal(err)
	}
	frags := SplitText(md)
	if len(frags) != 2 {
		t.Fatalf("кусков %d, ожидалось 2: %+v", len(frags), frags)
	}
	if frags[0].Title != "Первый" || frags[1].Title != "Второй" {
		t.Errorf("заголовки кусков: %q и %q", frags[0].Title, frags[1].Title)
	}
	if frags[1].Body != "тело второго" {
		t.Errorf("тело второго куска %q", frags[1].Body)
	}
}

func TestDocxНеБерётПробелыМеждуТегами(t *testing.T) {
	// Документ, чей XML сохранён с отступами, встречается у сторонних
	// преобразователей. Если брать текст из всякого узла абзаца, отступы
	// приедут внутрь положения и разорвут слова.
	data := docx(t, docHeader+"\n  <w:p>\n    <w:r>\n      <w:t>Диагноз</w:t>\n    </w:r>\n"+
		"    <w:r>\n      <w:t> ставится</w:t>\n    </w:r>\n  </w:p>\n"+docFooter, nil)
	got, err := DocxToMarkdown(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "Диагноз ставится" {
		t.Errorf("текст %q: между тегами просочились пробелы", got)
	}
}

func TestDocxНеБерётВычеркнутоеРецензентом(t *testing.T) {
	// delText — то, что рецензент удалил при правке с отслеживанием.
	// Текст положения — это то, что осталось, а не то, что было.
	data := docx(t, docHeader+
		`<w:p><w:r><w:delText>прежний текст</w:delText></w:r><w:r><w:t>новый текст</w:t></w:r></w:p>`+
		docFooter, nil)
	got, err := DocxToMarkdown(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "прежний") {
		t.Errorf("вычеркнутое попало в текст: %q", got)
	}
	if !strings.Contains(got, "новый текст") {
		t.Errorf("текст потерян: %q", got)
	}
}

// Развёрнутая разметка упирается в потолок.
//
// Файл ограничен MaxDocumentBytes, но это размер СЖАТЫЙ: повторяющаяся
// разметка жмётся в тысячи раз, и тридцать мегабайт разворачиваются в
// гигабайты — в контейнер, которому отведено 512. Падает при этом служба
// целиком, вместе с раздачей задач врачам, которые ничего не загружали.
//
// Бомба здесь настоящая, но потолок маленький: собирать двести мегабайт
// ради проверки устройства — это двадцать секунд у сторожа на каждый
// push. Разметка при этом правильная: на мусоре отказал бы разборщик XML,
// и проверка прошла бы на сломанном коде, ничего не проверив.
func TestDocxРазвёрнутаяРазметкаУпираетсяВПотолок(t *testing.T) {
	const потолок = 1 << 20

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	para := strings.Repeat(`<w:p><w:r><w:t>положение</w:t></w:r></w:p>`, 1<<10)
	if _, err := io.WriteString(w, docHeader); err != nil {
		t.Fatal(err)
	}
	for wrote := 0; wrote <= потолок; wrote += len(para) {
		if _, err := io.WriteString(w, para); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := io.WriteString(w, docFooter); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if buf.Len() >= потолок {
		t.Fatalf("бомба вышла %d байт при потолке %d — сжатия не случилось, "+
			"и проверка не о том", buf.Len(), потолок)
	}

	if _, err := docxToMarkdown(buf.Bytes(), потолок); err == nil {
		t.Fatal("разметка размером больше потолка разобралась без отказа")
	} else if !strings.Contains(err.Error(), "МБ") {
		t.Errorf("отказ не называет потолка: %v", err)
	}

	// И тот же файл под своим потолком разбирается: проверка сторожит
	// отказ на большом, а не отказ вообще.
	if _, err := docxToMarkdown(buf.Bytes(), maxDocumentXMLBytes); err != nil {
		t.Errorf("документ в пределах потолка не разобрался: %v", err)
	}
}
