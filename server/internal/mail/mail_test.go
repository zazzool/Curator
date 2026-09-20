package mail

import (
	"mime"
	"strings"
	"testing"
)

func TestТемаСКириллицейКодируется(t *testing.T) {
	// Тема как есть приезжает кракозябрами, и отказа при этом нет ни у
	// нас, ни у почтового сервера: увидит это только врач.
	raw := Build("we@example.org", "vrach@example.org", "Код для входа", "тело")
	head := string(raw)

	if strings.Contains(head, "Subject: Код для входа") {
		t.Fatal("тема ушла незакодированной")
	}
	subject := ""
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(line, "Subject: ") {
			subject = strings.TrimPrefix(line, "Subject: ")
		}
	}
	// Сверяем не строку кодировки, а то, что из неё получается: сравнение
	// с ожидаемой строкой проверяло бы нашу же запись, а не читаемость.
	back, err := new(mime.WordDecoder).DecodeHeader(subject)
	if err != nil {
		t.Fatalf("тема не разбирается обратно: %v", err)
	}
	if back != "Код для входа" {
		t.Errorf("тема приехала как %q", back)
	}
}

func TestТелоОтделеноИКодировкаНазвана(t *testing.T) {
	raw := string(Build("we@example.org", "vrach@example.org", "Тема", "первая\nвторая"))

	head, body, found := strings.Cut(raw, "\r\n\r\n")
	if !found {
		t.Fatal("заголовки не отделены от тела пустой строкой")
	}
	if !strings.Contains(head, "charset=utf-8") {
		t.Error("кодировка тела не названа: письмо по-русски приедет мусором")
	}
	if body != "первая\r\nвторая\r\n" {
		t.Errorf("тело приехало как %q", body)
	}
}

func TestТочкаВНачалеСтрокиЭкранируется(t *testing.T) {
	// Строка «.» закрывает письмо посреди текста, и остаток уезжает
	// почтовому серверу как команды — без всякого отказа.
	raw := string(Build("we@example.org", "vrach@example.org", "Тема", "текст\n.\nещё"))
	_, body, _ := strings.Cut(raw, "\r\n\r\n")
	if body != "текст\r\n..\r\nещё\r\n" {
		t.Errorf("тело приехало как %q", body)
	}
}

func TestНенастроеннаяОтправкаОтказываетСловами(t *testing.T) {
	// Пустой отправитель, молча делающий вид, что письмо ушло, отправляет
	// врача ждать кода, которого не будет.
	var s Sender
	if s.Ready() {
		t.Fatal("пустая настройка считается готовой")
	}
	if err := s.Send("vrach@example.org", "Тема", "тело"); err != ErrNotConfigured {
		t.Fatalf("отказ %v, ожидался ErrNotConfigured", err)
	}
}

func TestПортИОбратныйАдресИмеютУмолчания(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.org")
	t.Setenv("SMTP_USER", "we@example.org")
	t.Setenv("SMTP_PASSWORD", "пароль")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_FROM", "")

	s := FromEnv()
	// 25-й порт увёл бы письмо в никуда, и отказ наступил бы у врача.
	if s.Port != "587" {
		t.Errorf("порт по умолчанию %q, ожидался 587", s.Port)
	}
	if s.From != "we@example.org" {
		t.Errorf("обратный адрес по умолчанию %q", s.From)
	}
	if !s.Ready() {
		t.Error("настроенная отправка считается неготовой")
	}
}
