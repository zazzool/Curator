package studio

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Проверки печенья сессии идут без базы: что за печенье выдаётся и какой
// токен считается предъявленным — работа самого стола. Сквозной вход по
// печенью проверяется отдельно, на живой базе.
//
// Значения здесь латиницей не для красоты: net/http выбрасывает из печенья
// байты вне ASCII и говорит об этом только в журнал. Настоящий токен —
// base64url, то есть ASCII всегда; кириллица в проверке ломала бы её, не
// говоря ничего о коде.

func TestПеченьеСессииНеВидноСценариюИНеУезжаетНаЧужойСайт(t *testing.T) {
	// Все четыре признака куплены доводом, и потеря любого — тихая:
	// печенье продолжит работать, а защиты у него не станет.
	desk := NewDesk(nil, nil)
	desk.SetSecureCookies(true)

	rec := httptest.NewRecorder()
	desk.setSession(rec, "Tok3n-VALUE_xyz")

	jar := onlyCookie(t, rec)
	if jar.Name != sessionCookie || jar.Value != "Tok3n-VALUE_xyz" {
		t.Fatalf("выдано печенье %q со значением %q", jar.Name, jar.Value)
	}
	if !jar.HttpOnly {
		t.Error("печенье без HttpOnly: токен читается сценарием на странице и уезжает наружу")
	}
	if !jar.Secure {
		t.Error("печенье без Secure на https-контуре: уедет по открытой связи")
	}
	if jar.SameSite != http.SameSiteStrictMode {
		t.Error("печенье не Strict: чужой сайт сможет ходить им от имени вошедшего")
	}
	if jar.Path != "/admin" {
		t.Errorf("путь печенья %q: оно ходит туда, где не нужно", jar.Path)
	}
	if jar.MaxAge != int(SessionTTL.Seconds()) {
		t.Errorf("печенье живёт %d с, а сессия %v: браузер забудет вход раньше сервера",
			jar.MaxAge, SessionTTL)
	}
}

func TestБезHttpsПеченьеНеТребуетSecure(t *testing.T) {
	// Secure-печенье по http браузер выбрасывает молча, и студия на своей
	// машине переставала бы помнить вход без единого сообщения.
	desk := NewDesk(nil, nil)

	rec := httptest.NewRecorder()
	desk.setSession(rec, "Tok3n-VALUE_xyz")
	if onlyCookie(t, rec).Secure {
		t.Error("Secure выставлен на контуре без https")
	}
}

func TestГашениеПеченьяВелитЗабытьЕго(t *testing.T) {
	// Пустое значение браузер хранит дальше и шлёт обратно: выход выглядел
	// бы состоявшимся ровно до первой перезагрузки.
	desk := NewDesk(nil, nil)

	rec := httptest.NewRecorder()
	desk.dropSession(rec)

	jar := onlyCookie(t, rec)
	if jar.MaxAge >= 0 {
		t.Errorf("MaxAge %d: печенье не гасится, а просто пустеет", jar.MaxAge)
	}
	if jar.Path != "/admin" {
		t.Errorf("путь %q не тот, что при выдаче: браузер погасит не то печенье", jar.Path)
	}
}

func TestЗаголовокСтаршеПеченья(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/api/me", nil)
	req.Header.Set("Authorization", "Bearer from-header")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "from-cookie"})
	if got := presented(req); got != "from-header" {
		t.Errorf("предъявлено %q, а названное вслух старше того, что браузер шлёт сам", got)
	}
}

func TestКривойЗаголовокНеОтменяетПеченья(t *testing.T) {
	// Иначе одна испорченная строка Authorization выбрасывала бы из студии
	// человека с живой сессией.
	for _, head := range []string{"", "Bearer ", "Basic что-то"} {
		req := httptest.NewRequest(http.MethodGet, "/admin/api/me", nil)
		if head != "" {
			req.Header.Set("Authorization", head)
		}
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "from-cookie"})
		if got := presented(req); got != "from-cookie" {
			t.Errorf("при заголовке %q предъявлено %q, а ожидалось печенье", head, got)
		}
	}
}

// onlyCookie достаёт единственное печенье ответа.
func onlyCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	jars := rec.Result().Cookies()
	if len(jars) != 1 {
		t.Fatalf("печений в ответе %d, а ожидалось одно", len(jars))
	}
	return jars[0]
}
