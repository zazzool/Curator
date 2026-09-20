package app

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"curator/server/internal/dbgate"
)

// Корпус спрашивает, кто пришёл (наряд 20, СЕР-8 приговора аудита).
//
// До наряда GET /v1/cases отдавал ВЕСЬ опубликованный корпус всякому
// заведённому устройству, а устройство заводится в первую минуту после
// установки. Пакетная система при этом обходилась одним запросом: платный
// набор приезжал внутри корпуса целиком, с условием, вариантами и
// разбором, — то есть подписка не отличалась от её отсутствия ничем, что
// делает сервер.
//
// Проверяется это только на живой базе: права живут в базе, состав набора
// тоже, а в памяти зелёная проверка означала бы ровно это — что проверили
// память.
//
// Каждый опыт ниже держится на двух половинах сразу: «закрытого не видно»
// зелено и тогда, когда не видно вообще ничего, а «открытое видно» зелено
// и тогда, когда видно всё подряд.

// наборЛинейки заводит выпущенный набор названной линейки и кладёт в него
// задачи.
func наборЛинейки(t *testing.T, gate *dbgate.Gate, line string, ids ...string) int64 {
	t.Helper()
	slug := fmt.Sprintf("korpus-%d-%d", time.Now().UnixNano(), rand.Intn(100000))
	var packID int64
	if err := gate.QueryRow(context.Background(),
		`INSERT INTO packs (slug, title, status, line)
		 VALUES ($1, $2, 'published', $3) RETURNING id`,
		slug, "Набор "+slug, line).Scan(&packID); err != nil {
		t.Fatalf("набор линейки %s не заведён: %v", line, err)
	}
	for ord, id := range ids {
		if _, err := gate.Exec(context.Background(),
			`INSERT INTO pack_items (pack_id, case_id, ord) VALUES ($1, $2, $3)`,
			packID, id, ord); err != nil {
			t.Fatalf("задача %s не положена в набор: %v", id, err)
		}
	}
	return packID
}

// почта делает врача «авторизованным»: учётная запись заводится молча при
// первом запуске, и отличает назвавшегося от промолчавшего только почта.
func почта(t *testing.T, gate *dbgate.Gate, token string) {
	t.Helper()
	if _, err := gate.Exec(context.Background(), `
		UPDATE accounts SET email = 'vrach-' || id || '@example.ru'
		 WHERE id = (SELECT account_id FROM devices WHERE token_hash = $1)`,
		fingerprint(token)); err != nil {
		t.Fatalf("почта не привязана: %v", err)
	}
}

func TestPgКорпусОтдаётТолькоТоЧтоОткрытоНаборами(t *testing.T) {
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)
	root := fmt.Sprintf("корпус%d", time.Now().UnixNano())

	гостевая, _ := задачаПоПути(t, gate, root)
	платная, _ := задачаПоПути(t, gate, root)
	ничья, _ := задачаПоПути(t, gate, root)
	наборЛинейки(t, gate, "guest", гостевая)
	наборЛинейки(t, gate, "paid", платная)

	auth := map[string]string{"Authorization": "Bearer " + token}
	_, page, raw := call(t, srv, "GET", "/v1/cases?limit=100&path="+root, auth, nil)

	// Обе половины. Гостевая обязана быть: закрыв дверь, её закрыли бы
	// насовсем, и врач, впервые открывший приложение, увидел бы пустой
	// экран.
	if !containsCase(page, гостевая) {
		t.Fatalf("гость не получил гостевой линейки: %s", raw)
	}
	if containsCase(page, платная) {
		t.Error("корпус отдал задачу платного набора без единой строки прав: " +
			"пакетная система обходится одним запросом")
	}
	// Задача, не попавшая ни в один набор, — это способ раздать что угодно
	// мимо всех линеек.
	if containsCase(page, ничья) {
		t.Error("корпус отдал задачу, не входящую ни в один набор")
	}
}

func TestPgЗадачаПоНомеруЗакрытаТемЖеПравом(t *testing.T) {
	// Ручка по номеру — это тот же корпус, выданный по одной задаче.
	// Оставь её открытой — и отбор обходится перебором номеров, которые
	// приложение и так знает из своей базы.
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)
	root := fmt.Sprintf("номер%d", time.Now().UnixNano())

	гостевая, _ := задачаПоПути(t, gate, root)
	платная, _ := задачаПоПути(t, gate, root)
	наборЛинейки(t, gate, "guest", гостевая)
	наборЛинейки(t, gate, "paid", платная)

	auth := map[string]string{"Authorization": "Bearer " + token}
	if status, _, raw := call(t, srv, "GET", "/v1/cases/"+гостевая, auth, nil); status != http.StatusOK {
		t.Fatalf("гостевая задача по номеру не отдана, код %d: %s", status, raw)
	}
	status, _, raw := call(t, srv, "GET", "/v1/cases/"+платная, auth, nil)
	if status != http.StatusNotFound {
		t.Errorf("платная задача отдана по номеру, код %d: %s", status, raw)
	}
}

func TestPgПривязаннаяПочтаОткрываетБазовыйНабор(t *testing.T) {
	// Граница бесплатного: гостевой пакет — не назвавшемуся, базовый —
	// назвавшемуся. Проверяются обе стороны одним врачом: назваться
	// должно быть выгодно, и выгода должна появиться ровно в этот миг.
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)
	root := fmt.Sprintf("почта%d", time.Now().UnixNano())

	базовая, _ := задачаПоПути(t, gate, root)
	наборЛинейки(t, gate, "basic", базовая)

	auth := map[string]string{"Authorization": "Bearer " + token}
	_, page, _ := call(t, srv, "GET", "/v1/cases?limit=100&path="+root, auth, nil)
	if containsCase(page, базовая) {
		t.Fatal("базовый набор отдан тому, кто не назвался")
	}

	почта(t, gate, token)
	_, page, raw := call(t, srv, "GET", "/v1/cases?limit=100&path="+root, auth, nil)
	if !containsCase(page, базовая) {
		t.Errorf("назвавшийся не получил базового набора: %s", raw)
	}
}

func TestPgКупленныйНаборПриезжаетВКорпусе(t *testing.T) {
	// Худший исход наряда — не дыра, а вот это: врач заплатил и не
	// получил задач. Дыра закрыта, показать нечего, причины не видно
	// ниоткуда.
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)
	root := fmt.Sprintf("покупка%d", time.Now().UnixNano())

	платная, _ := задачаПоПути(t, gate, root)
	packID := наборЛинейки(t, gate, "paid", платная)

	if _, err := gate.Exec(context.Background(), `
		INSERT INTO entitlements (account_id, pack_id, kind, origin, starts_at)
		SELECT d.account_id, $2, 'pack', 'grant', NOW()
		  FROM devices d WHERE d.token_hash = $1`,
		fingerprint(token), packID); err != nil {
		t.Fatalf("право на набор не выдано: %v", err)
	}

	auth := map[string]string{"Authorization": "Bearer " + token}
	_, page, raw := call(t, srv, "GET", "/v1/cases?limit=100&path="+root, auth, nil)
	if !containsCase(page, платная) {
		t.Errorf("купленный набор не приехал в корпусе: %s", raw)
	}
}

func TestPgКорпусОбъявляетЧтоЗависитОтПредъявителя(t *testing.T) {
	// Один и тот же адрес отдаёт разное в зависимости от токена, и общий
	// кэш по дороге — наш nginx, кэш оператора связи у врача — отдал бы
	// второму то, что собрано первому. Это не отказ, а молчаливая
	// подмена состава: ни врач, ни журнал её не увидят.
	srv, _, key := дверь(t)
	token := устройство(t, srv, key)

	for _, path := range []string{"/v1/cases?limit=1", "/v1/cases/c-нет-такой"} {
		req, err := http.NewRequest("GET", srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.Header.Get("Vary") != "Authorization" {
			t.Errorf("%s: Vary = %q, а ответ зависит от предъявителя",
				path, res.Header.Get("Vary"))
		}
	}
}

func TestPgБезРезкиКорпусОтдаётсяЦеликом(t *testing.T) {
	// Единственный случай, когда корпус отдаётся целиком: у установки нет
	// ни одного выпущенного набора. Урезать его в пустоту значило бы
	// показать врачу пустое приложение там, где никто ничего не закрывал,
	// — а это ровно то, как выглядит только что поднятая установка.
	//
	// Проверяется здесь следствие, а не сам признак: на общей проверочной
	// базе выпущенные наборы всегда есть от соседних проверок, и признак
	// на ней не воспроизвести.
	gate := testGate(t)
	root := fmt.Sprintf("безрезки%d", time.Now().UnixNano())
	ничья, _ := задачаПоПути(t, gate, root)

	page, err := NewFeed(gate).Page(context.Background(),
		Cursor{Path: root, Limit: 100}, Scope{Cut: false})
	if err != nil {
		t.Fatal(err)
	}
	var нашлась bool
	for _, one := range page.Cases {
		if one.ID == ничья {
			нашлась = true
		}
	}
	if !нашлась {
		t.Error("корпус без резки потерял задачу, не входящую ни в один набор")
	}
}
