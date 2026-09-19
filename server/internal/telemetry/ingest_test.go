package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"curator/server/internal/dbgate"
)

func testGate(t *testing.T) *dbgate.Gate {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут. " +
			"Это отказ, а не пропуск — см. server/.env.example")
	}
	gate, err := dbgate.Open(context.Background(), dsn, 0)
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

func врач(t *testing.T, gate *dbgate.Gate) int64 {
	t.Helper()
	var id int64
	if err := gate.QueryRow(context.Background(),
		`INSERT INTO accounts DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("учётная запись не заведена: %v", err)
	}
	return id
}

func ключ() string {
	return fmt.Sprintf("с-%d-%d", time.Now().UnixNano(), rand.IntN(100000))
}

func TestPgНеизвестноеСобытиеОтбрасываетсяАПачкаПринимается(t *testing.T) {
	// Старый сервер обязан принять посылку новой сборки. Откажи он всей
	// пачке из-за одного незнакомого имени — и выкатка приложения
	// потребовала бы выкатки сервера в ту же минуту, а на руках у врачей
	// стоят сборки, которые обновятся не завтра.
	gate := testGate(t)
	store := NewStore(gate)
	id := врач(t, gate)
	now := time.Now()

	out, err := store.Record(context.Background(), id, []Incoming{
		{Name: "app_opened", IdemKey: ключ(), HappenedAt: now},
		{Name: "такого-события-нет", IdemKey: ключ(), HappenedAt: now},
		{Name: "explanation_opened", IdemKey: ключ(), HappenedAt: now},
	}, now)
	if err != nil {
		t.Fatalf("пачка отвергнута целиком: %v", err)
	}
	if out.Accepted != 2 || out.Dropped != 1 {
		t.Errorf("принято %d, отброшено %d; ожидались 2 и 1", out.Accepted, out.Dropped)
	}

	var n int
	if err := gate.QueryRow(context.Background(),
		`SELECT count(*) FROM telemetry_events WHERE account_id = $1 AND name = $2`,
		id, "такого-события-нет").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("неизвестное событие всё-таки легло в базу (%d строк)", n)
	}
}

func TestPgСвойствоНеИзСловаряОтбрасывается(t *testing.T) {
	// Свойство, приехавшее по месту, в базе выглядит данными: оно есть,
	// его видно в строке, и первый же, кто его увидит, построит по нему
	// отчёт — а шлёт его одна сборка из пяти.
	gate := testGate(t)
	store := NewStore(gate)
	id := врач(t, gate)
	key := ключ()
	now := time.Now()

	if _, err := store.Record(context.Background(), id, []Incoming{{
		Name:       "pack_opened",
		Props:      map[string]any{"pack": "cardio", "секретное": "значение"},
		IdemKey:    key,
		HappenedAt: now,
	}}, now); err != nil {
		t.Fatal(err)
	}

	var raw []byte
	if err := gate.QueryRow(context.Background(),
		`SELECT props FROM telemetry_events WHERE account_id = $1 AND idem_key = $2`,
		id, key).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		t.Fatal(err)
	}
	if props["pack"] != "cardio" {
		t.Errorf("разрешённое свойство потерялось: %s", raw)
	}
	if _, found := props["секретное"]; found {
		t.Errorf("свойство не из словаря легло в базу: %s", raw)
	}
}

func TestPgПовторнаяПосылкаНеУдваиваетСобытие(t *testing.T) {
	// Приложение повторяет посылку при обрыве. Лягни событие дважды — и
	// удвоился бы каждый отчёт, причём числа остались бы правдоподобными.
	gate := testGate(t)
	store := NewStore(gate)
	id := врач(t, gate)
	key := ключ()
	now := time.Now()
	batch := []Incoming{{Name: "purchase_started",
		Props: map[string]any{"purpose": "subscription:month"}, IdemKey: key, HappenedAt: now}}

	first, err := store.Record(context.Background(), id, batch, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Record(context.Background(), id, batch, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Accepted != 1 || second.Accepted != 0 || second.Repeated != 1 {
		t.Errorf("первая посылка %+v, вторая %+v: повтор оформился работой", first, second)
	}

	var n int
	if err := gate.QueryRow(context.Background(),
		`SELECT count(*) FROM telemetry_events WHERE account_id = $1 AND idem_key = $2`,
		id, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("в базе %d строк вместо одной", n)
	}
}

func TestPgСобытиеБезКлючаПовторностиЛожитсяКаждоеСвоей(t *testing.T) {
	// Ключ необязателен: сборка, его не шлющая, обязана работать. Без
	// ключа события ложатся каждое своей строкой — иначе указатель
	// повторности склеил бы все события врача в одно.
	gate := testGate(t)
	store := NewStore(gate)
	id := врач(t, gate)
	now := time.Now()

	out, err := store.Record(context.Background(), id, []Incoming{
		{Name: "case_shown", Props: map[string]any{"caseId": "c-1"}, HappenedAt: now},
		{Name: "case_shown", Props: map[string]any{"caseId": "c-2"}, HappenedAt: now},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.Accepted != 2 {
		t.Fatalf("принято %d событий вместо двух: %+v", out.Accepted, out)
	}
}

func TestPgВремяБеретсяУстройстваАНеДоставки(t *testing.T) {
	// Врач разбирал задачи в метро, а посылка ушла вечером. Запиши мы
	// время доставки — и отчёт сказал бы, что занимался он вечером.
	gate := testGate(t)
	store := NewStore(gate)
	id := врач(t, gate)
	key := ключ()

	вчера := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	if _, err := store.Record(context.Background(), id,
		[]Incoming{{Name: "app_opened", IdemKey: key, HappenedAt: вчера}}, time.Now()); err != nil {
		t.Fatal(err)
	}

	var got time.Time
	if err := gate.QueryRow(context.Background(),
		`SELECT happened_at FROM telemetry_events WHERE account_id = $1 AND idem_key = $2`,
		id, key).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(вчера) {
		t.Errorf("записано время %v, а событие случилось %v", got, вчера)
	}
}
