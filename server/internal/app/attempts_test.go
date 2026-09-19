package app

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/progress"
)

// дверьСЧасами поднимает /v1 с переводимыми часами.
//
// Часы полем, а не time.Now по месту: увидеть подошедший срок повторения
// иначе можно только подождав сутки, а проверка ждать не может.
func дверьСЧасами(t *testing.T) (*httptest.Server, *dbgate.Gate, string, *time.Time) {
	t.Helper()
	gate := testGate(t)
	keys := NewKeys(gate)

	keyID := fmt.Sprintf("часы-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	key, err := keys.Issue(context.Background(), keyID, "Проверка")
	if err != nil {
		t.Fatalf("ключ программы не заведён: %v", err)
	}

	clock := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	door := NewDoor(keys, NewAccounts(gate))
	r := &routes{
		accounts: door.accounts,
		feed:     NewFeed(gate),
		attempts: NewAttempts(gate, progress.Default()),
		now:      func() time.Time { return clock },
	}
	door.Open("GET /v1/health", r.health)
	door.Keyed("POST /v1/devices", r.enroll)
	door.Device("POST /v1/attempts", r.record)
	door.Device("GET /v1/progress", r.progress)
	door.Device("GET /v1/review", r.review)

	srv := httptest.NewServer(door.Handler())
	t.Cleanup(srv.Close)
	return srv, gate, key, &clock
}

func TestPgПовторнаяПосылкаПопытокНеУдваиваетОпыт(t *testing.T) {
	// Приложение работает офлайн и повторяет посылку при обрыве. Легла бы
	// попытка дважды — испортила бы и прогресс, и решаемость, причём
	// незаметно: числа остались бы правдоподобными.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	caseID, _ := задача(t, gate)
	auth := map[string]string{"Authorization": "Bearer " + token}

	batch := map[string]any{"attempts": []map[string]any{{
		"caseId": caseID, "correct": true, "answer": "3.1",
		"idemKey":    fmt.Sprintf("к-%d", time.Now().UnixNano()),
		"happenedAt": "2026-09-19T10:00:00Z",
	}}}

	status, first, raw := call(t, srv, "POST", "/v1/attempts", auth, batch)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if first["accepted"].(float64) != 1 {
		t.Fatalf("первая посылка не принята: %s", raw)
	}
	xp := first["xp"].(float64)
	if xp == 0 {
		t.Fatalf("опыт не начислен: %s", raw)
	}

	status, second, raw := call(t, srv, "POST", "/v1/attempts", auth, batch)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if second["accepted"].(float64) != 0 || second["repeated"].(float64) != 1 {
		t.Fatalf("повтор посылки принят как новая работа: %s", raw)
	}
	if second["xp"].(float64) != xp {
		t.Fatalf("повтор посылки изменил опыт: было %v, стало %v", xp, second["xp"])
	}
}

func TestPgБезКлючаПовторностиПопыткиЛожатсяКаждая(t *testing.T) {
	// Ключ выдаёт устройство. Не прислало — значит, отличить повтор от
	// новой работы нечем, и выбирать приходится между потерей разбора и
	// его удвоением. Теряем меньшее: попытка ложится, потому что
	// потерянный разбор врач заметит, а лишний — нет.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	caseID, _ := задача(t, gate)
	auth := map[string]string{"Authorization": "Bearer " + token}

	batch := map[string]any{"attempts": []map[string]any{
		{"caseId": caseID, "correct": true},
		{"caseId": caseID, "correct": false},
	}}
	status, out, raw := call(t, srv, "POST", "/v1/attempts", auth, batch)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if out["accepted"].(float64) != 2 {
		t.Fatalf("без ключа повторности попытки не легли: %s", raw)
	}
}

func TestPgПовторныйРазборТойЖеЗадачиНеДаётОпыта(t *testing.T) {
	// Иначе опыт набивается одной задачей, и уровень перестаёт
	// что-либо означать.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	caseID, _ := задача(t, gate)
	auth := map[string]string{"Authorization": "Bearer " + token}

	stamp := time.Now().UnixNano()
	_, first, _ := call(t, srv, "POST", "/v1/attempts", auth, map[string]any{
		"attempts": []map[string]any{{
			"caseId": caseID, "correct": true, "idemKey": fmt.Sprintf("п1-%d", stamp),
		}},
	})
	xp := first["xp"].(float64)

	_, second, raw := call(t, srv, "POST", "/v1/attempts", auth, map[string]any{
		"attempts": []map[string]any{{
			"caseId": caseID, "correct": true, "idemKey": fmt.Sprintf("п2-%d", stamp),
		}},
	})
	if second["accepted"].(float64) != 1 {
		t.Fatalf("вторая попытка не легла: %s", raw)
	}
	if second["xp"].(float64) != xp {
		t.Fatalf("повторный разбор дал опыт: было %v, стало %v", xp, second["xp"])
	}
}

func TestPgОшибкаДаётОпытМеньшеВерного_НоНеНоль(t *testing.T) {
	// Ноль за ошибку учит не отвечать, когда не уверен, а это ровно то
	// поведение, от которого задачи и должны отучать.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}
	wrongCase, _ := задача(t, gate)

	_, out, raw := call(t, srv, "POST", "/v1/attempts", auth, map[string]any{
		"attempts": []map[string]any{{
			"caseId": wrongCase, "correct": false,
			"idemKey": fmt.Sprintf("о-%d", time.Now().UnixNano()),
		}},
	})
	xp := out["xp"].(float64)
	if xp == 0 {
		t.Fatalf("за ошибку не дано ничего: %s", raw)
	}
	rules := progress.Default()
	if int64(xp) != rules.Wrong {
		t.Fatalf("за ошибку дано %v, эталон обещает %d", xp, rules.Wrong)
	}
}

func TestPgСрокПовторенияПодходитПоЧасам(t *testing.T) {
	// Первый верный ответ ставит срок через сутки. До них задача не
	// показывается, после — показывается: иначе повторение не повторение,
	// а тот же список.
	srv, gate, key, clock := дверьСЧасами(t)
	token := устройство(t, srv, key)
	caseID, _ := задача(t, gate)
	auth := map[string]string{"Authorization": "Bearer " + token}

	_, _, raw := call(t, srv, "POST", "/v1/attempts", auth, map[string]any{
		"attempts": []map[string]any{{
			"caseId": caseID, "correct": true,
			"idemKey":    fmt.Sprintf("с-%d", time.Now().UnixNano()),
			"happenedAt": clock.Format(time.RFC3339),
		}},
	})

	_, today, raw := call(t, srv, "GET", "/v1/review", auth, nil)
	if containsReview(today, caseID) {
		t.Fatalf("задача к повторению показана в тот же день: %s", raw)
	}

	// Двигаем часы на двое суток — срок подошёл.
	*clock = clock.AddDate(0, 0, 2)
	_, later, raw := call(t, srv, "GET", "/v1/review", auth, nil)
	if !containsReview(later, caseID) {
		t.Fatalf("задача не появилась к повторению после срока: %s", raw)
	}

	_, prog, raw := call(t, srv, "GET", "/v1/progress", auth, nil)
	if prog["due"].(float64) < 1 {
		t.Fatalf("в прогрессе нет задач к повторению: %s", raw)
	}
}

func containsReview(page map[string]any, id string) bool {
	list, _ := page["cases"].([]any)
	for _, raw := range list {
		one, _ := raw.(map[string]any)
		if one["id"] == id {
			return true
		}
	}
	return false
}

func TestPgСнятаяСРаздачиЗадачаКПовторениюНеПредлагается(t *testing.T) {
	// Состояние повторения остаётся: оно принадлежит врачу, а не задаче.
	// Но показывать её нельзя — составитель уже счёл её негодной.
	srv, gate, key, clock := дверьСЧасами(t)
	token := устройство(t, srv, key)
	caseID, _ := задача(t, gate)
	auth := map[string]string{"Authorization": "Bearer " + token}

	call(t, srv, "POST", "/v1/attempts", auth, map[string]any{
		"attempts": []map[string]any{{
			"caseId": caseID, "correct": true,
			"idemKey":    fmt.Sprintf("сн-%d", time.Now().UnixNano()),
			"happenedAt": clock.Format(time.RFC3339),
		}},
	})
	*clock = clock.AddDate(0, 0, 2)

	_, before, raw := call(t, srv, "GET", "/v1/review", auth, nil)
	if !containsReview(before, caseID) {
		t.Fatalf("задача не дождалась срока: %s", raw)
	}

	if _, err := снять(gate, caseID); err != nil {
		t.Fatal(err)
	}
	_, after, raw := call(t, srv, "GET", "/v1/review", auth, nil)
	if containsReview(after, caseID) {
		t.Fatalf("снятая задача предложена к повторению: %s", raw)
	}

	// Состояние при этом на месте: вернут задачу в раздачу — и врач
	// продолжит с накопленного, а не с нуля.
	var kept int
	err := gate.QueryRow(context.Background(),
		`SELECT count(*) FROM review_states WHERE case_id = $1`, caseID).Scan(&kept)
	if err != nil {
		t.Fatal(err)
	}
	if kept != 1 {
		t.Fatal("состояние повторения пропало вместе со снятием задачи")
	}
}

func TestPgПустойСписокКПовторениюНеNull(t *testing.T) {
	// «Сегодня нечего повторять» — исправный случай, самый частый из всех,
	// и null уронил бы приложение именно на нём.
	srv, _, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)

	status, page, raw := call(t, srv, "GET", "/v1/review",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if page["cases"] == nil {
		t.Fatalf("список к повторению отдан null: %s", raw)
	}
}

func TestPgСлишкомБольшаяПачкаОтказываетВслух(t *testing.T) {
	// Обрезанная пачка означает потерянный разбор, и врач об этом не
	// узнает. Отказ вслух он увидит.
	srv, _, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)

	batch := make([]map[string]any, 501)
	for i := range batch {
		batch[i] = map[string]any{"caseId": "c-неважно", "correct": true}
	}
	status, body, raw := call(t, srv, "POST", "/v1/attempts",
		map[string]string{"Authorization": "Bearer " + token},
		map[string]any{"attempts": batch})
	if status != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался отказ: %s", status, raw)
	}
	if body["error"] == "" {
		t.Fatalf("отказ без объяснения: %s", raw)
	}
}

func TestPgВремяРазбораБерётсяСУстройства_АНеСДоставки(t *testing.T) {
	// Врач разбирал задачу в метро, а посылка ушла вечером. «Занимался
	// вечером» — неправда, и отчёт о занятиях по времени доставки
	// показывал бы её каждый день.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	caseID, _ := задача(t, gate)

	happened := "2026-09-01T07:30:00Z"
	call(t, srv, "POST", "/v1/attempts",
		map[string]string{"Authorization": "Bearer " + token},
		map[string]any{"attempts": []map[string]any{{
			"caseId": caseID, "correct": true,
			"idemKey":    fmt.Sprintf("вр-%d", time.Now().UnixNano()),
			"happenedAt": happened,
		}}})

	var stored time.Time
	err := gate.QueryRow(context.Background(),
		`SELECT happened_at FROM attempts WHERE case_id = $1 ORDER BY id DESC LIMIT 1`,
		caseID).Scan(&stored)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := time.Parse(time.RFC3339, happened)
	if !stored.Equal(want) {
		t.Fatalf("записано время %v, а разбор был в %v", stored, want)
	}
}

// снять убирает задачу из раздачи прямым запросом: проверке нужен
// результат, а не путь к нему.
func снять(gate *dbgate.Gate, caseID string) (bool, error) {
	tag, err := gate.Exec(context.Background(),
		`UPDATE cases SET status = 'archived' WHERE id = $1`, caseID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func TestPgОтветыПовторенияСходятсяСЭталоном(t *testing.T) {
	// Три ответа появились разом, и стережёт их тот же эталон, что и
	// ленту: формат, не записанный в эталон, держится только памятью того,
	// кто его писал, — а приложение разбирает его на другом языке.
	contract := loadContract(t)
	srv, gate, key, clock := дверьСЧасами(t)
	token := устройство(t, srv, key)
	caseID, _ := задача(t, gate)
	auth := map[string]string{"Authorization": "Bearer " + token}

	status, accepted, raw := call(t, srv, "POST", "/v1/attempts", auth, map[string]any{
		"attempts": []map[string]any{{
			"caseId": caseID, "correct": true, "answer": "3.1",
			"idemKey":    fmt.Sprintf("эт-%d", time.Now().UnixNano()),
			"happenedAt": clock.Format(time.RFC3339),
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	matchShape(t, "POST /v1/attempts", accepted, contract.Responses["POST /v1/attempts"].Fields)
	after, _ := accepted["metrics"].(map[string]any)
	matchShape(t, "величины после посылки", after, contract.Metrics.Fields)

	_, progressBody, _ := call(t, srv, "GET", "/v1/progress", auth, nil)
	matchShape(t, "GET /v1/progress", progressBody, contract.Responses["GET /v1/progress"].Fields)
	shown, _ := progressBody["metrics"].(map[string]any)
	matchShape(t, "величины в прогрессе", shown, contract.Metrics.Fields)

	// Часы переводятся вперёд, иначе список к повторению пуст и сверять в
	// нём нечего: эталон описывает не только оболочку, но и задачу внутри.
	*clock = clock.Add(48 * time.Hour)

	_, review, raw := call(t, srv, "GET", "/v1/review?limit=50", auth, nil)
	matchShape(t, "GET /v1/review", review, contract.Responses["GET /v1/review"].Fields)
	list, _ := review["cases"].([]any)
	if len(list) == 0 {
		t.Fatalf("к повторению ничего не подошло, сверять нечего: %s", raw)
	}
	first, _ := list[0].(map[string]any)
	matchShape(t, "GET /v1/review[]", first, contract.Responses["GET /v1/review"].Each["cases"])

	body, _ := first["body"].(map[string]any)
	matchShape(t, "содержание задачи к повторению", body, contract.CaseBody.Fields)
}
