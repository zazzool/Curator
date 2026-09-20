package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestЖивостьОтвечаетБезБазы(t *testing.T) {
	// Ручка живости обязана отвечать и без базы: её спрашивает docker, и
	// ответ про процесс, а не про хранилище. Ручка, молчащая без базы,
	// заставила бы docker убивать исправный процесс.
	rec := httptest.NewRecorder()
	routes(context.Background(), nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("пустой ответ живости")
	}
}

func TestПорогМедленныхЗапросов(t *testing.T) {
	t.Setenv("CURATOR_SLOW_MS", "")
	if got := slowThreshold(); got != 500*time.Millisecond {
		t.Errorf("умолчание %v", got)
	}
	t.Setenv("CURATOR_SLOW_MS", "1200")
	if got := slowThreshold(); got != 1200*time.Millisecond {
		t.Errorf("значение %v", got)
	}
	// Мусор в переменной не роняет подъём: сервис, не поднявшийся из-за
	// опечатки в необязательной настройке, — это отказ там, где хватало
	// строки в журнале.
	t.Setenv("CURATOR_SLOW_MS", "быстро")
	if got := slowThreshold(); got != 500*time.Millisecond {
		t.Errorf("мусор дал %v вместо умолчания", got)
	}
}

func TestАдресПоУмолчанию(t *testing.T) {
	t.Setenv("CURATOR_ADDR", "")
	if got := addr(); got != ":8080" {
		t.Errorf("адрес %q", got)
	}
}

func TestГотовностьБезБазыОтказывает(t *testing.T) {
	// Ровно противоположное живости, и в этом весь смысл второй ручки.
	// Готовность без базы обязана ОТКАЗАТЬ: «жив» при отпавшей базе
	// оставлял контейнер здоровым, прокси слал в него обращения, каждое
	// отвечало пятисотым, и docker был доволен.
	rec := httptest.NewRecorder()
	routes(context.Background(), nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("код %d, ожидался 503", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("пустой ответ готовности: смотрящему нечего прочитать")
	}
}

func TestСрокЗапросаКБазе(t *testing.T) {
	t.Setenv("CURATOR_QUERY_TIMEOUT_MS", "")
	if got := queryTimeout(); got != 30*time.Second {
		t.Errorf("умолчание %v", got)
	}
	t.Setenv("CURATOR_QUERY_TIMEOUT_MS", "5000")
	if got := queryTimeout(); got != 5*time.Second {
		t.Errorf("значение %v", got)
	}
	// Ноль снимает срок совсем, и это не мусор, а нужный случай: разовая
	// работа, про которую заранее известно, что она идёт долго.
	t.Setenv("CURATOR_QUERY_TIMEOUT_MS", "0")
	if got := queryTimeout(); got != 0 {
		t.Errorf("ноль дал %v вместо снятого срока", got)
	}
	t.Setenv("CURATOR_QUERY_TIMEOUT_MS", "скоро")
	if got := queryTimeout(); got != 30*time.Second {
		t.Errorf("мусор дал %v вместо умолчания", got)
	}
}

func TestСтудияОтдаётсяНаПрямуюСсылку(t *testing.T) {
	// Прямая ссылка на источник — это адрес студии, а не файл на диске.
	// Голый файловый сервер отвечал на него 404, и «пришлите ссылку на
	// источник» упиралось в то, что ссылки не существует.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>студия"), 0o600); err != nil {
		t.Fatalf("страница студии не записана: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatalf("каталог сборки не заведён: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "studio.js"), []byte("// сборка"), 0o600); err != nil {
		t.Fatalf("файл сборки не записан: %v", err)
	}

	h := studioFiles(dir)

	for _, path := range []string{"/", "/sources", "/sources/12", "/workshop"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: код %d, ожидался 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "студия") {
			t.Errorf("%s: отдана не страница студии", path)
		}
	}

	// Существующий файл сборки отдаётся собой, а не страницей: подстановка
	// добавляет ответ там, где его не было, и не меняет раздачу сборки.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/studio.js", nil))
	if body := rec.Body.String(); body != "// сборка" {
		t.Errorf("файл сборки подменён: %q", body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("заголовок хранения у настоящего файла: %q", got)
	}

	// Ненайденный файл сборки получает страницу — и ОБЯЗАН получать её с
	// переспросом. Запомненная разметка под именем скрипта не чистится у
	// составителя ничем.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/которого-нет.js", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("подставленная страница хранится: %q", got)
	}
}
