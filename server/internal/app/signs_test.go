package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/signs"
)

// знаки достаёт каталог знаков одного врача.
func знаки(t *testing.T, srv *httptest.Server, token string) map[string]map[string]any {
	t.Helper()
	status, body, raw := call(t, srv, "GET", "/v1/signs",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	list, ok := body["signs"].([]any)
	if !ok {
		t.Fatalf("знаки приехали не списком: %s", raw)
	}
	out := map[string]map[string]any{}
	for _, one := range list {
		row, _ := one.(map[string]any)
		slug, _ := row["slug"].(string)
		out[slug] = row
	}
	return out
}

// решить кладёт n разных решённых задач одному врачу.
func решить(t *testing.T, srv *httptest.Server, gate *dbgate.Gate, token string, n int) map[string]any {
	t.Helper()
	batch := make([]map[string]any, 0, n)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		id, _ := задача(t, gate)
		batch = append(batch, map[string]any{
			"caseId": id, "correct": true,
			"idemKey":    fmt.Sprintf("з-%d-%d", time.Now().UnixNano(), i),
			"happenedAt": base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		})
	}
	return послать(t, srv, token, batch)
}

func TestPgЗнакВыдаётсяСерверомИНесётНомер(t *testing.T) {
	// Номер, дата выдачи и тираж — сведения о том, каким врач пришёл среди
	// всех, и вывести их из его собственной истории нельзя. Потому их
	// выдаёт сервер, и потому они проверяются здесь, а не на устройстве.
	srv, gate, key, _ := дверьСЧасами(t)
	выпуски(t, gate)
	token := устройство(t, srv, key)

	body := решить(t, srv, gate, token, 10)
	awarded, ok := body["signs"].([]any)
	if !ok {
		t.Fatalf("выданные знаки приехали не списком: %v", body)
	}
	if len(awarded) == 0 {
		t.Fatalf("за десять решённых задач не выдано ничего: %v", body)
	}

	got := знаки(t, srv, token)
	first := got["first-steps"]
	if first["issued"] != true {
		t.Fatalf("«Первые шаги» не выданы: %v", first)
	}
	if serial, _ := first["serial"].(float64); serial < 1 {
		t.Errorf("знак выдан без номера: %v", first)
	}
	if at, _ := first["issuedAt"].(string); at == "" {
		t.Errorf("знак выдан без даты: %v", first)
	}
}

func TestPgОпытЗаЗнакНачисляетсяПоВыданному(t *testing.T) {
	srv, gate, key, _ := дверьСЧасами(t)
	выпуски(t, gate)
	token := устройство(t, srv, key)

	// Девять задач: до «Первых шагов» не хватает одной. Другие знаки к
	// этому мигу уже выданы (у каждой задачи проверки свой источник), и
	// это не мешает: считается прирост на десятой задаче, а не всё
	// накопленное.
	nine := решить(t, srv, gate, token, 9)
	before, _ := nine["xp"].(float64)
	for _, one := range выданные(t, nine) {
		if row, _ := one.(map[string]any); row["slug"] == "first-steps" {
			t.Fatalf("знак выдан раньше порога: %v", row)
		}
	}

	tenth := решить(t, srv, gate, token, 1)
	after, _ := tenth["xp"].(float64)

	выдано := выданные(t, tenth)
	if len(выдано) != 1 {
		t.Fatalf("десятой задачей выдано %d знаков, ожидался один: %v", len(выдано), выдано)
	}

	// 10 за верный ответ плюс 50 за знак.
	if after-before != 60 {
		t.Errorf("опыт вырос на %v, а ожидалось 10 за ответ и 50 за знак", after-before)
	}
}

func TestPgЗнакНеВыдаётсяДважды(t *testing.T) {
	// Выданный знак не отбирают, но и не выдают заново: второй такой же
	// номер обесценил бы первый.
	srv, gate, key, _ := дверьСЧасами(t)
	выпуски(t, gate)
	token := устройство(t, srv, key)

	решить(t, srv, gate, token, 10)
	again := решить(t, srv, gate, token, 5)
	for _, one := range выданные(t, again) {
		if row, _ := one.(map[string]any); row["slug"] == "first-steps" {
			t.Errorf("знак выдан повторно: %v", row)
		}
	}
}

func TestPgТиражКончаетсяИЗнакНеВыдаётся(t *testing.T) {
	// Знак с тиражом можно заслужить и не получить — и опыта за него нет:
	// он начисляется по выданному, а не по заслуженному.
	srv, gate, key, _ := дверьСЧасами(t)
	выпуски(t, gate)

	// Тираж «Первопроходца» выбирается досуха прямо в базе: сто врачей
	// проверка заводить не будет, а проверяет она поведение на исчерпании.
	// База одна на весь прогон, поэтому прежнее число возвращается на
	// место: проверка, оставляющая за собой изменённую базу, ломает
	// соседнюю — и не ту, которая виновата.
	было := роздано(t, gate, "pioneer")
	if _, err := gate.Exec(context.Background(),
		`UPDATE sign_editions SET issued_count = edition_size WHERE slug = 'pioneer'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { вернуть(gate, "pioneer", было) })

	token := устройство(t, srv, key)
	body := решить(t, srv, gate, token, 1)

	for _, one := range выданные(t, body) {
		if row, _ := one.(map[string]any); row["slug"] == "pioneer" {
			t.Fatalf("знак выдан сверх тиража: %v", row)
		}
	}
	// А опыт за ответ начислен: кончившийся тираж — не отказ.
	if xp, _ := body["xp"].(float64); xp != 10 {
		t.Errorf("опыт %v, ожидалось 10 за верный ответ", xp)
	}
}

func TestPgОрденСобираетсяИзВыданныхЧастей(t *testing.T) {
	// Собирательный знак смотрит на КОГДА-ЛИБО выданное, и выдаётся он в
	// тот же заход, что и последняя его часть: порядок обхода каталога
	// ставит орден после частей намеренно.
	srv, gate, key, _ := дверьСЧасами(t)
	выпуски(t, gate)
	token := устройство(t, srv, key)

	// Сто верных подряд: и «Сотня», и «Двадцать пять подряд».
	решить(t, srv, gate, token, 100)

	got := знаки(t, srv, token)
	for _, slug := range []string{"hundred", "streak-25", "order-of-accuracy"} {
		if got[slug]["issued"] != true {
			t.Errorf("знак %q не выдан: %v", slug, got[slug])
		}
	}
}

func TestPgПереходящийЗнакОтзываетсяАНеУдаляется(t *testing.T) {
	// Строка о выдаче не удаляется никогда: собирательные знаки смотрят на
	// когда-либо выданное, и удали мы её — орден рассыпался бы вслед за
	// переходящим знаком.
	srv, gate, key, _ := дверьСЧасами(t)
	выпуски(t, gate)

	// Переходящий знак один на всю установку, а база одна на весь прогон:
	// к этому мигу знак держит врач из соседней проверки, и новый его не
	// перебьёт. Освобождаем знак тем же способом, каким его освобождает
	// сам код, — отзывом, а не удалением строки.
	if _, err := gate.Exec(context.Background(),
		`UPDATE account_signs SET revoked_at = NOW()
		  WHERE sign_slug = 'primus' AND revoked_at IS NULL`); err != nil {
		t.Fatal(err)
	}

	первый := устройство(t, srv, key)
	решить(t, srv, gate, первый, 30)
	if знаки(t, srv, первый)["primus"]["issued"] != true {
		t.Fatal("переходящий знак не достался первому")
	}

	второй := устройство(t, srv, key)
	решить(t, srv, gate, второй, 40)

	уВторого := знаки(t, srv, второй)["primus"]
	if уВторого["issued"] != true {
		t.Fatalf("переходящий знак не перешёл к лучшему: %v", уВторого)
	}
	уПервого := знаки(t, srv, первый)["primus"]
	if уПервого["issued"] != true {
		t.Fatalf("строка о выдаче исчезла у прежнего обладателя: %v", уПервого)
	}
	if уПервого["revoked"] != true {
		t.Errorf("прежняя выдача не отмечена отозванной: %v", уПервого)
	}
}

func TestPgОрденНеРассыпаетсяПослеОтзываПереходящего(t *testing.T) {
	srv, gate, key, _ := дверьСЧасами(t)
	выпуски(t, gate)

	первый := устройство(t, srv, key)
	решить(t, srv, gate, первый, 100)
	if знаки(t, srv, первый)["order-of-accuracy"]["issued"] != true {
		t.Fatal("орден не собрался")
	}

	второй := устройство(t, srv, key)
	решить(t, srv, gate, второй, 120)

	if знаки(t, srv, первый)["order-of-accuracy"]["issued"] != true {
		t.Error("орден сорвался вслед за переходящим знаком")
	}
}

func TestPgКаталогЗнаковПриезжаетЦеликомИСДолейПути(t *testing.T) {
	// Знак, о котором врач не знает, не мотивирует никого — поэтому
	// каталог отдаётся целиком, вместе с невыданными.
	srv, gate, key, _ := дверьСЧасами(t)
	выпуски(t, gate)
	token := устройство(t, srv, key)
	решить(t, srv, gate, token, 5)

	got := знаки(t, srv, token)
	if len(got) != len(signs.Catalog()) {
		t.Fatalf("знаков в ответе %d, в каталоге %d", len(got), len(signs.Catalog()))
	}
	// Пять из десяти — половина пути к «Первым шагам».
	if share, _ := got["first-steps"]["progress"].(float64); share != 0.5 {
		t.Errorf("доля пути %v, ожидалась 0.5", share)
	}
	if got["first-steps"]["issued"] != false {
		t.Error("знак выдан раньше порога")
	}
}

func TestPgДоляОбладателейСчитаетсяСервером(t *testing.T) {
	// Из собственной истории врача она не выводится вовсе, а без неё знак
	// не говорит, каким он пришёл среди всех.
	srv, gate, key, _ := дверьСЧасами(t)
	выпуски(t, gate)
	token := устройство(t, srv, key)
	решить(t, srv, gate, token, 10)

	got := знаки(t, srv, token)["first-steps"]
	share, _ := got["holdersShare"].(float64)
	if share <= 0 || share > 1 {
		t.Errorf("доля обладателей %v — вне границ", share)
	}
	if count, _ := got["issuedCount"].(float64); count < 1 {
		t.Errorf("роздано %v, а знак только что выдан", count)
	}
}

func TestPgУменьшениеТиражаНижеВыданногоОтказывает(t *testing.T) {
	// Уменьшить тираж ниже уже выданного значит объявить часть знаков
	// несуществующими, а выданный знак не отбирают.
	gate := testGate(t)
	ctx := context.Background()
	if err := signs.Ensure(ctx, gate); err != nil {
		t.Fatal(err)
	}
	было := роздано(t, gate, "pioneer")
	if _, err := gate.Exec(ctx,
		`UPDATE sign_editions SET issued_count = 1000000 WHERE slug = 'pioneer'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { вернуть(gate, "pioneer", было) })
	if err := signs.Ensure(ctx, gate); err == nil {
		t.Error("тираж, уменьшенный ниже выданного, прошёл молча")
	}
}

// выпуски заводит знаки в проверочной базе.
func выпуски(t *testing.T, gate *dbgate.Gate) {
	t.Helper()
	if err := signs.Ensure(context.Background(), gate); err != nil {
		t.Fatalf("выпуски знаков не заведены: %v", err)
	}
}

// роздано читает нынешнее число выданных знаков выпуска.
func роздано(t *testing.T, gate *dbgate.Gate, slug string) int {
	t.Helper()
	var count int
	if err := gate.QueryRow(context.Background(),
		`SELECT issued_count FROM sign_editions WHERE slug = $1`, slug).Scan(&count); err != nil {
		t.Fatalf("выпуск %q не прочитан: %v", slug, err)
	}
	return count
}

func вернуть(gate *dbgate.Gate, slug string, count int) {
	_, _ = gate.Exec(context.Background(),
		`UPDATE sign_editions SET issued_count = $2 WHERE slug = $1`, slug, count)
}

// выданные достаёт список знаков, выданных этой пачкой.
func выданные(t *testing.T, body map[string]any) []any {
	t.Helper()
	list, ok := body["signs"].([]any)
	if !ok {
		t.Fatalf("выданные знаки приехали не списком: %v", body)
	}
	return list
}
