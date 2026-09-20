// Отправка писем.
//
// Письмо нужно ровно для одного: подтвердить, что почта принадлежит
// врачу, и вернуть ему доступ при смене телефона. Ничего больше отсюда не
// шлётся — ни рассылок, ни уведомлений, — и потому здесь нет ни очереди,
// ни повторов: письмо либо ушло сейчас, либо врачу сказано, что не ушло.
//
// Зависимости не добавляются: net/smtp из стандартной библиотеки умеет
// STARTTLS и PLAIN, а больше ничего и не нужно.
package mail

import (
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// Sender — настроенная отправка.
type Sender struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string

	// dial подменяется проверкой. Полем, а не вызовом по месту: проверить
	// сборку письма и разговор с сервером иначе можно только на живом
	// почтовом ящике, а его у проверок нет.
	dial func(addr string) (*smtp.Client, error)
}

// ErrNotConfigured — отправка не настроена.
//
// Отдельным отказом, а не пустым Sender, который молча ничего не делает:
// «письмо отправлено», сказанное там, где почта не настроена, отправляет
// врача ждать кода, которого не будет.
var ErrNotConfigured = errors.New("отправка писем не настроена")

// FromEnv собирает отправку из окружения.
//
// Отсутствие настроек — не отказ при старте: служба обязана подниматься и
// раздавать задачи там, где почты нет. Отказывает только сама отправка, и
// отказывает словами.
func FromEnv() *Sender {
	s := &Sender{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     os.Getenv("SMTP_PORT"),
		User:     os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
	}
	if s.Port == "" {
		// 587 — порт подачи с обязательным STARTTLS. Умолчание в 25-й увело
		// бы письмо в никуда, и отказ наступил бы не при настройке, а при
		// первой отправке, то есть у врача.
		s.Port = "587"
	}
	if s.From == "" {
		// Обратный адрес по умолчанию — имя входа: у большинства
		// поставщиков это один и тот же ящик. Разойдись они, письмо
		// отвергнет уже сервер, и отказ будет виден.
		s.From = s.User
	}
	return s
}

// Ready говорит, настроена ли отправка.
func (s *Sender) Ready() bool {
	return s != nil && s.Host != "" && s.User != "" && s.Password != "" && s.From != ""
}

// Send отправляет письмо.
func (s *Sender) Send(to, subject, body string) error {
	if !s.Ready() {
		return ErrNotConfigured
	}
	addr := net.JoinHostPort(s.Host, s.Port)

	dial := s.dial
	if dial == nil {
		dial = func(addr string) (*smtp.Client, error) {
			conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
			if err != nil {
				return nil, err
			}
			return smtp.NewClient(conn, s.Host)
		}
	}

	client, err := dial(addr)
	if err != nil {
		return fmt.Errorf("почтовый сервер недоступен: %w", err)
	}
	defer client.Close()

	// STARTTLS обязателен, а не «если предложат». Пароль по открытому
	// каналу не ушёл бы и без этой проверки — smtp.PlainAuth отказывает
	// на незащищённом соединении сам, — но отказывает он словами
	// «unencrypted connection» и уже на шаге входа. Здесь отказ наступает
	// раньше и называет причину: настройку чинит человек, и «сервер не
	// предлагает STARTTLS» говорит ему, куда смотреть.
	ok, _ := client.Extension("STARTTLS")
	if !ok {
		return errors.New("почтовый сервер не предлагает STARTTLS: пароль по открытому каналу не отправляется")
	}
	if err := client.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
		return fmt.Errorf("защищённое соединение с почтой не установлено: %w", err)
	}
	if err := client.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
		return fmt.Errorf("почта не приняла имя и пароль: %w", err)
	}
	if err := client.Mail(s.From); err != nil {
		return fmt.Errorf("обратный адрес отвергнут: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("адрес получателя отвергнут: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("письмо не принято: %w", err)
	}
	if _, err := w.Write(Build(s.From, to, subject, body)); err != nil {
		return fmt.Errorf("письмо не дописано: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("письмо не закрыто: %w", err)
	}
	return client.Quit()
}

// Build собирает письмо целиком.
//
// Отдельной функцией, потому что именно её и можно проверить: разговор с
// почтовым сервером проверяется поддельным сервером, а вид письма — вот
// здесь, построчно.
func Build(from, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + oneLine(from) + "\r\n")
	b.WriteString("To: " + oneLine(to) + "\r\n")
	// Заголовок с кириллицей обязан быть закодирован по RFC 2047. Отправь
	// его как есть — и тема приедет кракозябрами, причём отказа не будет
	// ни у нас, ни у почтового сервера: увидит это только врач.
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	// Точка в начале строки экранируется: иначе строка «.» закрывает
	// письмо посреди текста, и остаток уедет почтовому серверу как
	// команды. Отправь мы так разбор с переносом строки — и получили бы
	// оборванное письмо без всякого отказа.
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, ".") {
			line = "." + line
		}
		b.WriteString(line + "\r\n")
	}
	return []byte(b.String())
}

// oneLine вырезает из значения заголовка переводы строки.
//
// Сегодня сюда не приходит адреса с переводом строки: Looks отвергает их
// на всех трёх путях, и это проверено. Но Build — открытая функция, и
// следующий, кто её позовёт, будет в одной строке от подделки
// заголовков: адрес вида «врач@example.com\r\nBcc: все@example.com»
// вписывает в письмо ЧУЖОЙ заголовок, и почтовый сервер исполнит его как
// свой. Отсекается здесь, а не у вызывающего, потому что вызывающих
// будет больше, чем один, а Build — одна.
//
// Вырезаем, а не отказываем: Build ничего не возвращает, кроме письма, и
// заводить ей отказ ради случая, которого пока нет, значит менять её
// подпись у всех вызывающих. Вырезанный перевод строки письма не портит
// — он превращает подделку в бессмыслицу, и адрес отвергнет уже сервер.
func oneLine(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}
