package limits

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestЗаголовкиБезопасностиСтоятНаВсяком(t *testing.T) {
	// Не было ни одного, и отдано это было прокси, настройки которого нет
	// в репозитории: она не проверяется ничем и не переживает переезд.
	handler := Headers(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("готов"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	want := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, а нужно %q", name, got, value)
		}
	}
	if rec.Header().Get("Referrer-Policy") == "" {
		t.Error("Referrer-Policy не задан: чужому сайту уезжает путь с опознавателями")
	}
}

func TestПравилаЗагрузкиЗапрещаютЧужойСкрипт(t *testing.T) {
	// Студия оформляет возврат денег в один щелчок: скрипт со стороны и
	// есть то, ради чего эта строка пишется.
	handler := Headers(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	policy := rec.Header().Get("Content-Security-Policy")
	if policy == "" {
		t.Fatal("Content-Security-Policy не задан")
	}
	if !strings.Contains(policy, "script-src 'self'") {
		t.Error("скриптам не сказано ходить только со своего")
	}
	// Открытая для стилей строка не должна открыться для скриптов
	// заодно: это ровно та правка, которую делают «чтобы заработало».
	if strings.Contains(policy, "script-src 'self' 'unsafe-inline'") {
		t.Error("скриптам разрешено 'unsafe-inline': правила загрузки перестали значить что-либо")
	}
	if !strings.Contains(policy, "frame-ancestors 'none'") {
		t.Error("студию можно вложить в чужую страницу")
	}
}
