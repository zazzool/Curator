package main

import (
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
	routes(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
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
