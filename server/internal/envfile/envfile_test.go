package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadНеПеребиваетОкружение(t *testing.T) {
	// Ради этого правила пакет и существует: переменная контура сильнее
	// файла, иначе файл, случайно уехавший в образ, увёл бы сервис на
	// чужой адрес.
	t.Setenv("CURATOR_PUBLIC_URL", "https://контур")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("CURATOR_PUBLIC_URL=https://файл\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("CURATOR_PUBLIC_URL"); got != "https://контур" {
		t.Errorf("значение %q: файл перебил окружение", got)
	}
}

func TestLoadБезФайлаНеОтказывает(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "нет-такого")); err != nil {
		t.Errorf("отсутствие файла дало отказ: %v", err)
	}
}

func TestLoadЧитаетЗначения(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "# пояснение\n\nSMTP_HOST=\"smtp.example\"\nПУСТАЯ=\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMTP_HOST", "")
	os.Unsetenv("SMTP_HOST")
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("SMTP_HOST"); got != "smtp.example" {
		t.Errorf("SMTP_HOST = %q", got)
	}
}
