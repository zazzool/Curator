package limits

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Тело в пределах потолка доходит до обработчика нетронутым.
func TestТелоВПределахПотолкаДоходитЦелым(t *testing.T) {
	var получено string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("тело не прочиталось: %v", err)
		}
		получено = string(body)
	})

	req := httptest.NewRequest("POST", "/v1/telemetry", strings.NewReader(`{"ok":true}`))
	Body(1<<20, next).ServeHTTP(httptest.NewRecorder(), req)

	if получено != `{"ok":true}` {
		t.Fatalf("тело приехало как %q", получено)
	}
}

// Всплеск проходит ровно на ёмкость, следующее обращение запирается.
func TestВедроПропускаетВсплескИЗапираетСледующее(t *testing.T) {
	b := NewBucket(3, 60)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	for i := range 3 {
		if !b.Allow("1.2.3.4", now) {
			t.Fatalf("обращение %d из всплеска не прошло", i+1)
		}
	}
	if b.Allow("1.2.3.4", now) {
		t.Fatal("четвёртое обращение прошло при ёмкости в три")
	}
}

// Ведро одного адреса не запирает другой.
//
// Без этого предел на заведение устройств запер бы всех врачей разом
// после одного перебирающего, и выглядело бы это как отказ службы.
func TestЗапертыйАдресНеЗапираетСоседа(t *testing.T) {
	b := NewBucket(1, 60)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	b.Allow("1.2.3.4", now)
	if b.Allow("1.2.3.4", now) {
		t.Fatal("второе обращение того же адреса прошло")
	}
	if !b.Allow("5.6.7.8", now) {
		t.Fatal("обращение соседнего адреса не прошло")
	}
}

// Жетоны возвращаются со временем и не копятся сверх ёмкости.
func TestЖетоныВозвращаютсяИНеКопятсяСверхЁмкости(t *testing.T) {
	b := NewBucket(2, 60) // один жетон в секунду
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	b.Allow("1.2.3.4", now)
	b.Allow("1.2.3.4", now)
	if b.Allow("1.2.3.4", now) {
		t.Fatal("третье обращение прошло сразу")
	}
	if !b.Allow("1.2.3.4", now.Add(time.Second)) {
		t.Fatal("через секунду жетон не вернулся")
	}

	// Час простоя не должен дать часового запаса: иначе перебирающий
	// копит жетоны молчанием и тратит их одной очередью.
	далеко := now.Add(time.Hour)
	for i := range 2 {
		if !b.Allow("1.2.3.4", далеко) {
			t.Fatalf("обращение %d после простоя не прошло", i+1)
		}
	}
	if b.Allow("1.2.3.4", далеко) {
		t.Fatal("после часа простоя прошло больше, чем ёмкость ведра")
	}
}

// Адрес берётся последней записью X-Forwarded-For, а не первой.
//
// Первая прислана клиентом и подделывается одной строкой. Возьми её — и
// предел перестанет существовать, оставаясь на вид работающим: ровно то,
// что эта проверка обязана ловить.
func TestАдресБерётсяПоследнейЗаписьюЗаголовка(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/devices", nil)
	req.RemoteAddr = "10.0.0.1:34567"
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 8.8.8.8, 203.0.113.7")

	if got := Address(req); got != "203.0.113.7" {
		t.Fatalf("адрес определён как %q, а прокси видел 203.0.113.7", got)
	}
}

// Подделанный заголовок не размножает адрес: последняя запись одна и та же.
func TestПодделанныйЗаголовокНеОбходитПредел(t *testing.T) {
	b := NewBucket(1, 60)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	позвать := func(подделка string) bool {
		req := httptest.NewRequest("POST", "/v1/devices", nil)
		req.RemoteAddr = "10.0.0.1:34567"
		req.Header.Set("X-Forwarded-For", подделка+", 203.0.113.7")
		return b.Allow(Address(req), now)
	}

	if !позвать("1.1.1.1") {
		t.Fatal("первое обращение не прошло")
	}
	if позвать("2.2.2.2") {
		t.Fatal("смена подделанной записи обошла предел")
	}
}

// Без заголовка адрес берётся из соединения, и без порта.
func TestБезЗаголовкаАдресБерётсяИзСоединения(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/devices", nil)
	req.RemoteAddr = "192.0.2.5:51234"

	if got := Address(req); got != "192.0.2.5" {
		t.Fatalf("адрес определён как %q, а соединение пришло с 192.0.2.5", got)
	}
}

// Уборка не трогает запертые вёдра.
//
// Ведро с нулём жетонов — это и есть память о перебирающем. Убери его за
// компанию с остывшими, и предел снимется ровно с того, ради кого стоит.
func TestУборкаНеСнимаетЗапертоеВедро(t *testing.T) {
	b := NewBucket(1, 60)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	b.Allow("перебирающий", now)
	if b.Allow("перебирающий", now) {
		t.Fatal("второе обращение прошло сразу")
	}

	// Столько соседей, чтобы уборка точно случилась.
	for i := range sweepFrom + 1 {
		b.Allow(strings.Repeat("x", i%7)+time.Duration(i).String(), now)
	}

	if b.Allow("перебирающий", now) {
		t.Fatal("после уборки запертый адрес прошёл")
	}
}

// Остывшее ведро уборка снимает, и карта не растёт без предела.
func TestУборкаСнимаетОстывшиеВёдра(t *testing.T) {
	b := NewBucket(1, 60)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	for i := range sweepFrom {
		b.Allow(time.Duration(i).String(), now)
	}
	if len(b.buckets) != sweepFrom {
		t.Fatalf("вёдер %d, а заводили %d", len(b.buckets), sweepFrom)
	}

	// Минута спустя каждое из них полно, то есть не помнит ничего.
	b.Allow("новичок", now.Add(time.Minute))
	if len(b.buckets) != 1 {
		t.Fatalf("после уборки осталось %d вёдер вместо одного", len(b.buckets))
	}
}

// Названная длина сверх потолка отказывает словами и до обработчика.
func TestНазваннаяДлинаСверхПотолкаОтказываетСловами(t *testing.T) {
	var дошло bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { дошло = true })

	req := httptest.NewRequest("POST", "/v1/attempts", strings.NewReader(strings.Repeat("a", 100)))
	rec := httptest.NewRecorder()
	Body(16, next).ServeHTTP(rec, req)

	if дошло {
		t.Fatal("запрос сверх потолка дошёл до обработчика")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("код %d вместо 413", rec.Code)
	}
	var тело map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &тело); err != nil {
		t.Fatalf("отказ приехал не разбираемым: %v", err)
	}
	// Врач должен понять, что делать, а не читать «тело не разобрано».
	if !strings.Contains(тело["error"], "слишком большой") {
		t.Fatalf("отказ ничего не объясняет: %q", тело["error"])
	}
}

// Запрос без названной длины потолок всё равно не обходит.
//
// ContentLength у такого запроса -1, и одна сверка длины пропустила бы
// его целиком — то есть предел снимался бы одним заголовком.
func TestЗапросБезНазваннойДлиныПотолокНеОбходит(t *testing.T) {
	var прочитано int
	var сбой error
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		прочитано, сбой = len(body), err
	})

	req := httptest.NewRequest("POST", "/v1/attempts", strings.NewReader(strings.Repeat("a", 100)))
	req.ContentLength = -1
	Body(16, next).ServeHTTP(httptest.NewRecorder(), req)

	if сбой == nil {
		t.Fatalf("тело в %d байт без названной длины прочитано целиком", прочитано)
	}
	// Поток обрывается НА потолке, а не после него: в этом и разница со
	// сверкой «сколько пришло» после разбора — та узнаёт правду, когда
	// память уже занята.
	if прочитано > 16 {
		t.Fatalf("прочитано %d байт при потолке 16", прочитано)
	}
}
