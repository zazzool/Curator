package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// величины достаёт то, что отдала посылка разборов.
func величины(t *testing.T, body map[string]any) map[string]float64 {
	t.Helper()
	raw, ok := body["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("величины не приехали: %v", body)
	}
	out := map[string]float64{}
	for key, value := range raw {
		number, ok := value.(float64)
		if !ok {
			t.Fatalf("величина %q приехала как %T", key, value)
		}
		out[key] = number
	}
	return out
}

// послать отправляет пачку разборов и отдаёт ответ.
func послать(t *testing.T, srv *httptest.Server, token string, attempts []map[string]any) map[string]any {
	t.Helper()
	status, body, raw := call(t, srv, "POST", "/v1/attempts",
		map[string]string{"Authorization": "Bearer " + token},
		map[string]any{"attempts": attempts})
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	return body
}

func разбор(caseID string, correct bool, when string) map[string]any {
	return map[string]any{
		"caseId": caseID, "correct": correct, "answer": "3.1",
		"idemKey":    fmt.Sprintf("%s/%s/%d", caseID, when, time.Now().UnixNano()),
		"happenedAt": when,
	}
}

func TestPgВеличиныПриезжаютПолнымНаборомДажеУНовичка(t *testing.T) {
	// Пустой объект заставил бы приложение помнить, что ключа может не
	// быть, — и оно забудет на исправном случае: на новом враче, который
	// ещё ничего не решал.
	srv, _, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)

	_, body, raw := call(t, srv, "GET", "/v1/progress",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	got := величины(t, body)
	if len(got) != 5 {
		t.Fatalf("величин %d, а каталог из пяти: %s", len(got), raw)
	}
	for key, value := range got {
		if value != 0 {
			t.Errorf("у новичка величина %q равна %v", key, value)
		}
	}
}

func TestPgСерияСчитаетсяПоВремениРазбора_АНеПоПорядкуДоставки(t *testing.T) {
	// Та самая причина, по которой величины пересчитываются, а не
	// накапливаются. Пачка за понедельник приезжает ПОСЛЕ вторничной:
	// счётчик, растущий в порядке прихода, увидел бы ошибку в середине
	// серии и оборвал её. Порядок занятий врача — верно, верно, верно.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	a, _ := задача(t, gate)
	b, _ := задача(t, gate)
	c, _ := задача(t, gate)

	// Сперва доезжает среда, и в ней ошибка.
	послать(t, srv, token, []map[string]any{
		разбор(c, false, "2026-09-16T10:00:00Z"),
	})
	// Потом — понедельник и вторник, оба верные.
	body := послать(t, srv, token, []map[string]any{
		разбор(a, true, "2026-09-14T10:00:00Z"),
		разбор(b, true, "2026-09-15T10:00:00Z"),
	})

	got := величины(t, body)
	if got["correctStreak"] != 2 {
		t.Errorf("лучшая серия %v, а по времени разбора их две подряд до ошибки",
			got["correctStreak"])
	}
	if got["daysActive"] != 3 {
		t.Errorf("дней занятий %v, ожидалось 3", got["daysActive"])
	}
}

func TestPgЛучшаяСерияНеПадаетПослеОшибки(t *testing.T) {
	// Величина, способная уменьшиться, знак не выдерживает: сегодня он
	// выдан, завтра выдавать его не за что, а сорвать нельзя — выданный
	// знак не отбирают.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	a, _ := задача(t, gate)
	b, _ := задача(t, gate)
	c, _ := задача(t, gate)

	body := послать(t, srv, token, []map[string]any{
		разбор(a, true, "2026-09-14T10:00:00Z"),
		разбор(b, true, "2026-09-14T11:00:00Z"),
		разбор(c, true, "2026-09-14T12:00:00Z"),
	})
	if got := величины(t, body)["correctStreak"]; got != 3 {
		t.Fatalf("серия %v, ожидалось 3", got)
	}

	body = послать(t, srv, token, []map[string]any{
		разбор(a, false, "2026-09-15T10:00:00Z"),
	})
	if got := величины(t, body)["correctStreak"]; got != 3 {
		t.Errorf("после ошибки лучшая серия стала %v: величина убывает, "+
			"и знак по ней выдавать нельзя", got)
	}
}

func TestPgРешёнозадачСчитаетРазныеЗадачи_АНеПопытки(t *testing.T) {
	// Иначе величина набивается одной задачей, и знак по ней ничего не
	// говорит о враче.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	a, _ := задача(t, gate)

	body := послать(t, srv, token, []map[string]any{
		разбор(a, true, "2026-09-14T10:00:00Z"),
		разбор(a, true, "2026-09-14T11:00:00Z"),
		разбор(a, true, "2026-09-14T12:00:00Z"),
	})
	if got := величины(t, body)["casesSolved"]; got != 1 {
		t.Errorf("решено задач %v, а задача была одна", got)
	}
}

func TestPgИсточникиИПовторенияСчитаютсяОтдельно(t *testing.T) {
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	a, первый := задача(t, gate)
	b, второй := задача(t, gate)
	if первый == второй {
		t.Fatal("проверке нужны задачи из разных источников")
	}

	body := послать(t, srv, token, []map[string]any{
		разбор(a, true, "2026-09-14T10:00:00Z"),
		{"caseId": b, "correct": true, "mode": ModeReview,
			"idemKey":    fmt.Sprintf("повтор-%d", time.Now().UnixNano()),
			"happenedAt": "2026-09-14T11:00:00Z"},
	})
	got := величины(t, body)
	if got["sourcesTouched"] != 2 {
		t.Errorf("источников затронуто %v, ожидалось 2", got["sourcesTouched"])
	}
	if got["reviewsDone"] != 1 {
		t.Errorf("повторений сделано %v, ожидалось 1", got["reviewsDone"])
	}
}

func TestPgПовторнаяПосылкаВеличиныНеРастит(t *testing.T) {
	// Пересчёт идёт по таблице попыток, и отброшенная по ключу
	// повторности попытка в неё не легла. Вырасти величина от повторной
	// посылки — и знак выдался бы за обрыв связи.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	a, _ := задача(t, gate)

	batch := []map[string]any{{
		"caseId": a, "correct": true,
		"idemKey":    fmt.Sprintf("один-%d", time.Now().UnixNano()),
		"happenedAt": "2026-09-14T10:00:00Z",
	}}
	first := величины(t, послать(t, srv, token, batch))
	second := величины(t, послать(t, srv, token, batch))

	for key, value := range first {
		if second[key] != value {
			t.Errorf("величина %q выросла от повторной посылки: %v → %v",
				key, value, second[key])
		}
	}
}

func TestPgИспорченныйСнимокВеличинНеЛомаетПрогресс(t *testing.T) {
	// Непонятое не применяется: испорченный снимок отбрасывается целиком,
	// не роняя остального. Врач увидит нули и вернёт правду первой же
	// пачкой — это лучше белого экрана.
	srv, gate, key, _ := дверьСЧасами(t)
	token := устройство(t, srv, key)
	a, _ := задача(t, gate)
	послать(t, srv, token, []map[string]any{разбор(a, true, "2026-09-14T10:00:00Z")})

	caller, err := NewAccounts(gate).ByToken(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(t.Context(),
		`UPDATE account_progress SET metrics = '"не объект"'::jsonb WHERE account_id = $1`,
		caller.AccountID); err != nil {
		t.Fatal(err)
	}

	status, body, raw := call(t, srv, "GET", "/v1/progress",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusOK {
		t.Fatalf("испорченный снимок уронил прогресс, код %d: %s", status, raw)
	}
	if len(величины(t, body)) != 5 {
		t.Errorf("каталог приехал неполным: %s", raw)
	}
}
