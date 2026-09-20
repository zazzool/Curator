package main

import (
	"context"
	"net/http"
	"net/http/httptest"
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
