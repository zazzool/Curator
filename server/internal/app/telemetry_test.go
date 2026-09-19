package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"curator/server/internal/telemetry"
)

// дверьСобытий поднимает /v1 с ручкой телеметрии.
func дверьСобытий(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	gate := testGate(t)
	keys := NewKeys(gate)

	key, err := keys.Issue(context.Background(), fmt.Sprintf("события-%d", time.Now().UnixNano()), "Проверка")
	if err != nil {
		t.Fatalf("ключ программы не заведён: %v", err)
	}
	door := NewDoor(keys, NewAccounts(gate))
	door.Keyed("POST /v1/devices", (&routes{accounts: NewAccounts(gate)}).enroll)
	TelemetryRoutes(door, telemetry.NewStore(gate))

	srv := httptest.NewServer(door.Handler())
	t.Cleanup(srv.Close)
	return srv, key
}

func TestPgОтветНаПачкуСобытийСходитсяСЭталоном(t *testing.T) {
	// Пачка с незнакомым именем принимается целиком, а отброшенное
	// возвращается числом: старый сервер обязан принять посылку новой
	// сборки, иначе выкатка приложения потребовала бы выкатки сервера в ту
	// же минуту.
	contract := loadContract(t)
	srv, key := дверьСобытий(t)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}

	status, body, raw := call(t, srv, "POST", "/v1/telemetry", auth, map[string]any{
		"events": []map[string]any{
			{"name": "app_opened", "props": map[string]any{"cold": true},
				"idemKey": fmt.Sprintf("с-%d-1", time.Now().UnixNano())},
			{"name": "имени-такого-нет",
				"idemKey": fmt.Sprintf("с-%d-2", time.Now().UnixNano())},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("пачка отвергнута, код %d: %s", status, raw)
	}
	matchShape(t, "POST /v1/telemetry", body, contract.Responses["POST /v1/telemetry"].Fields)

	if accepted, _ := body["accepted"].(float64); accepted != 1 {
		t.Errorf("принято %v событий вместо одного: %s", body["accepted"], raw)
	}
	if dropped, _ := body["dropped"].(float64); dropped != 1 {
		t.Errorf("отброшено %v событий вместо одного: %s", body["dropped"], raw)
	}
}

func TestPgСлишкомБольшаяПачкаСобытийОтказываетСловами(t *testing.T) {
	srv, key := дверьСобытий(t)
	token := устройство(t, srv, key)

	events := make([]map[string]any, 0, 1001)
	for i := 0; i < 1001; i++ {
		events = append(events, map[string]any{"name": "app_opened"})
	}
	status, body, raw := call(t, srv, "POST", "/v1/telemetry",
		map[string]string{"Authorization": "Bearer " + token},
		map[string]any{"events": events})
	if status != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался отказ: %s", status, raw)
	}
	if text, _ := body["error"].(string); text == "" {
		t.Error("отказ приехал без внятного текста")
	}
}
