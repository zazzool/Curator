package mail

import (
	"bufio"
	"net"
	"net/smtp"
	"strings"
	"testing"
)

// поддельныйСервер отвечает на приветствие заданным списком возможностей.
//
// Настоящего почтового сервера у проверок нет, а проверить надо именно
// разговор: откажемся ли мы отправлять пароль туда, где канал не
// защищён. На живом ящике это не проверишь — он-то STARTTLS предлагает.
func поддельныйСервер(t *testing.T, возможности []string) *smtp.Client {
	t.Helper()
	наш, их := net.Pipe()
	t.Cleanup(func() { наш.Close(); их.Close() })

	go func() {
		defer их.Close()
		читатель := bufio.NewReader(их)
		их.Write([]byte("220 поддельный\r\n"))
		for {
			строка, err := читатель.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(строка, "EHLO"):
				ответ := "250-поддельный\r\n"
				for i, one := range возможности {
					дефис := "-"
					if i == len(возможности)-1 {
						дефис = " "
					}
					ответ += "250" + дефис + one + "\r\n"
				}
				if len(возможности) == 0 {
					ответ = "250 поддельный\r\n"
				}
				их.Write([]byte(ответ))
			case strings.HasPrefix(строка, "QUIT"):
				их.Write([]byte("221 пока\r\n"))
				return
			default:
				их.Write([]byte("250 ладно\r\n"))
			}
		}
	}()

	client, err := smtp.NewClient(наш, "поддельный")
	if err != nil {
		t.Fatalf("поддельный сервер не поднялся: %v", err)
	}
	return client
}

func TestБезSTARTTLSПарольНеОтправляется(t *testing.T) {
	s := &Sender{
		Host: "поддельный", Port: "587",
		User: "we@example.org", Password: "пароль", From: "we@example.org",
		dial: func(string) (*smtp.Client, error) {
			// Сервер без STARTTLS: принял бы пароль по открытому каналу.
			return поддельныйСервер(t, nil), nil
		},
	}

	err := s.Send("vrach@example.org", "Тема", "тело")
	if err == nil {
		t.Fatal("отправка пошла на сервер без STARTTLS: пароль ушёл бы открытым")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("отказ не называет причину: %v", err)
	}
}
