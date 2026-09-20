package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"curator/server/internal/limits"
)

// Исчерпанное ведро отдаёт 429 и говорит по-русски.
func TestСчётПоАдресуОтказываетСловами(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var позвали int
	h := Metered(
		limits.NewBucket(1, 60),
		func() time.Time { return now },
		"Слишком много попыток с этого адреса. Попробуйте позже",
		func(w http.ResponseWriter, r *http.Request, keyID string) {
			позвали++
			WriteJSON(w, http.StatusCreated, map[string]string{"ok": "да"})
		})

	первый := httptest.NewRecorder()
	h(первый, httptest.NewRequest("POST", "/v1/devices", nil), "ключ")
	if первый.Code != http.StatusCreated {
		t.Fatalf("первое обращение не прошло, код %d", первый.Code)
	}

	второй := httptest.NewRecorder()
	h(второй, httptest.NewRequest("POST", "/v1/devices", nil), "ключ")
	if второй.Code != http.StatusTooManyRequests {
		t.Fatalf("второе обращение дало код %d вместо 429", второй.Code)
	}
	if позвали != 1 {
		t.Fatalf("обработчик позван %d раза: запертое обращение до него дошло", позвали)
	}
	if второй.Header().Get("Retry-After") == "" {
		t.Fatal("исправному клиенту нечего прочитать: Retry-After не поставлен")
	}

	var тело map[string]string
	if err := json.Unmarshal(второй.Body.Bytes(), &тело); err != nil {
		t.Fatalf("отказ приехал не разбираемым: %v", err)
	}
	// Ни срока, ни счёта в тексте: и то, и другое — подсказка
	// перебирающему, когда возвращаться.
	if !strings.Contains(тело["error"], "Слишком много") {
		t.Fatalf("отказ говорит не по-русски и не по делу: %q", тело["error"])
	}
}

// Заведение устройства считается по адресу.
//
// Проверка стоит на маршруте, а не на Metered: Metered можно снять с
// объявления одной строкой, и ни одна проверка самого ведра этого не
// заметит — запас в двадцать заведений тихо станет бесконечным.
func TestPgЗаведениеУстройстваСчитаетсяПоАдресу(t *testing.T) {
	srv, _, key := дверь(t)
	headers := map[string]string{"X-App-Key": key}

	// Запас по объявлению маршрута — двадцать; двадцать первое подряд
	// обязано упереться.
	for i := range 20 {
		status, _, raw := call(t, srv, "POST", "/v1/devices", headers, nil)
		if status != http.StatusCreated {
			t.Fatalf("заведение %d из запаса дало код %d: %s", i+1, status, raw)
		}
	}

	status, body, raw := call(t, srv, "POST", "/v1/devices", headers, nil)
	if status != http.StatusTooManyRequests {
		t.Fatalf("заведение сверх запаса дало код %d вместо 429: %s", status, raw)
	}
	if текст, _ := body["error"].(string); !strings.Contains(текст, "Слишком много") {
		t.Fatalf("отказ ничего не объясняет врачу: %s", raw)
	}
}

// Возврат доступа считается по адресу.
//
// Письмо уходит на ЧУЖОЙ адрес, названный в запросе, а отвечает за него
// наша почтовая репутация. Промежуток между письмами на один ящик этого
// не закрывает: ящики разные, обратный адрес один.
func TestPgВозвратДоступаСчитаетсяПоАдресу(t *testing.T) {
	srv, _, key, _ := почтоваяДверь(t)
	headers := map[string]string{"X-App-Key": key}

	// Запас по объявлению маршрута — пять.
	for i := range 5 {
		status, _, raw := call(t, srv, "POST", "/v1/recovery", headers,
			map[string]any{"email": адрес()})
		if status != http.StatusAccepted {
			t.Fatalf("попытка %d из запаса дала код %d: %s", i+1, status, raw)
		}
	}

	status, _, raw := call(t, srv, "POST", "/v1/recovery", headers,
		map[string]any{"email": адрес()})
	if status != http.StatusTooManyRequests {
		t.Fatalf("попытка сверх запаса дала код %d вместо 429: %s", status, raw)
	}
}
