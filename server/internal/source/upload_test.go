package source

import (
	"errors"
	"strings"
	"testing"
)

func TestPDFНеПринимаетсяИНеОбещаетПозже(t *testing.T) {
	// Перевод PDF в текст делает служба снаружи — это выбранная граница
	// проекта, а не очередь работ. Поэтому в отказе не должно быть слова
	// «позже»: обещание, которое никто не собирается исполнять, отправляет
	// человека ждать вместо того, чтобы перевести файл.
	_, err := Prepare(Upload{Filename: "приказ.pdf", Body: []byte("%PDF-1.7\nчто угодно")})
	if !errors.Is(err, ErrPDF) {
		t.Fatalf("PDF принят или отказ другой: %v", err)
	}
	for _, обещание := range []string{"позже", "пока", "скоро", "в будущем"} {
		if strings.Contains(err.Error(), обещание) {
			t.Fatalf("отказ по PDF обещает %q: %s", обещание, err)
		}
	}
}

func TestPDFУзнаётсяПоСодержимомуАНеПоИмени(t *testing.T) {
	// Переименовать файл проще, чем перевести его, и отказ по одному лишь
	// расширению обходится в два щелчка. На выходе был бы источник из
	// двоичной каши, и заметили бы это не при загрузке.
	_, err := Prepare(Upload{Filename: "приказ.txt", Body: []byte("%PDF-1.4\n1 0 obj")})
	if !errors.Is(err, ErrPDF) {
		t.Fatalf("PDF под видом текста принят: %v", err)
	}
}

func TestДвоичныйФайлПодВидомТекстаОтклоняется(t *testing.T) {
	// Иначе мусор доедет до модели, она честно найдёт в нём «положения», и
	// человек получит источник из шума.
	_, err := Prepare(Upload{Filename: "файл.txt", Body: []byte{0xFF, 0xFE, 0x00, 0x01}})
	if err == nil {
		t.Fatal("двоичный файл принят как текст")
	}
}

func TestФорматБерётсяИзИмениФайла(t *testing.T) {
	// Угадывание по содержимому не различает текст и Markdown, а человеку
	// это различие видно: он принёс .md и ждёт, что заголовки станут
	// заголовками.
	md, err := Prepare(Upload{Filename: "рекомендации.md", Body: []byte("# Заголовок\n\nтело")})
	if err != nil {
		t.Fatal(err)
	}
	if md.Format != FormatMarkdown {
		t.Fatalf("Markdown принят как %q", md.Format)
	}
	txt, err := Prepare(Upload{Filename: "рекомендации.txt", Body: []byte("# Заголовок\n\nтело")})
	if err != nil {
		t.Fatal(err)
	}
	if txt.Format != FormatText {
		t.Fatalf("текст принят как %q", txt.Format)
	}
	if txt.SHA256 != md.SHA256 {
		t.Fatal("отпечаток считается не от содержимого: у одинаковых тел он разошёлся")
	}
}

func TestНезнакомоеРасширениеОтклоняется(t *testing.T) {
	// Принимаются текст, Markdown и DOCX. Молчаливый приём .doc или .rtf
	// дал бы тот же мусор, что PDF под чужим именем.
	if _, err := Prepare(Upload{Filename: "приказ.rtf", Body: []byte("{\\rtf1")}); err == nil {
		t.Fatal("файл незнакомого формата принят")
	}
}

func TestПустойФайлОтклоняется(t *testing.T) {
	if _, err := Prepare(Upload{Filename: "пусто.txt"}); err == nil {
		t.Fatal("пустой файл принят")
	}
}
