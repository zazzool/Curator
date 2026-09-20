package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"curator/server/internal/casestore"
	"curator/server/internal/dbgate"
	"curator/server/internal/progress"
	"curator/server/internal/sales"
	"curator/server/internal/studio"
)

// testGate открывает дверь к проверочной базе.
//
// Без строки подключения — отказ, а не пропуск: проверка, которая молча
// пропускается и возвращает успех, выдаёт зелёное за непроверенное.
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

// дверь поднимает /v1 и заводит ключ программы.
func дверь(t *testing.T) (*httptest.Server, *dbgate.Gate, string) {
	t.Helper()
	gate := testGate(t)
	keys := NewKeys(gate)

	keyID := fmt.Sprintf("проверка-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	key, err := keys.Issue(context.Background(), keyID, "Проверка")
	if err != nil {
		t.Fatalf("ключ программы не заведён: %v", err)
	}

	door := NewDoor(keys, NewAccounts(gate))
	Routes(door, NewFeed(gate), NewAttempts(gate, progress.Default()), sales.NewAccess(gate))
	srv := httptest.NewServer(door.Handler())
	t.Cleanup(srv.Close)
	return srv, gate, key
}

// call — обращение к /v1.
func call(t *testing.T, srv *httptest.Server, method, path string, headers map[string]string, body any) (int, map[string]any, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("ответ %s %s не разобран: %s", method, path, raw)
	}
	return resp.StatusCode, parsed, string(raw)
}

// устройство заводит устройство и отдаёт его токен.
func устройство(t *testing.T, srv *httptest.Server, key string) string {
	t.Helper()
	status, body, raw := call(t, srv, "POST", "/v1/devices",
		map[string]string{"X-App-Key": key},
		map[string]any{"platform": "android", "appVersion": "1.0.0"})
	if status != http.StatusCreated {
		t.Fatalf("устройство не заведено, код %d: %s", status, raw)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("устройство заведено без токена: %s", raw)
	}
	return token
}

// задача кладёт опубликованную задачу и отдаёт её номер.
//
// Метка единицы берётся своя у каждой проверки, и это не украшение: база
// одна на весь прогон, задачи в ней копятся, и обход страниц по общему
// пути ловил бы чужие — сперва замедляясь, потом не доходя до своих вовсе.
// Проверка, дешевеющая от чужих данных, ломается не сразу и не у того, кто
// её писал.
func задача(t *testing.T, gate *dbgate.Gate) (string, int64) {
	t.Helper()
	return задачаПоПути(t, gate, fmt.Sprintf("п%d-%d", time.Now().UnixNano(), rand.Intn(100000)))
}

// задачаПоПути кладёт задачу под названной меткой верхнего уровня.
func задачаПоПути(t *testing.T, gate *dbgate.Gate, root string) (string, int64) {
	t.Helper()
	ctx := context.Background()
	slug := fmt.Sprintf("приказ-%d-%d", time.Now().UnixNano(), rand.Intn(100000))
	label := root + ".1"

	var sourceID int64
	err := gate.QueryRow(ctx,
		`INSERT INTO sources (slug, kind, title, unit_word, statement_word,
		                      purpose, hierarchy, completeness)
		 VALUES ($1, 'decree', 'Приказ', 'пункт', 'указание', 'legal', 'part-of', 'fragment')
		 RETURNING id`, slug).Scan(&sourceID)
	if err != nil {
		t.Fatalf("источник не заведён: %v", err)
	}
	_, err = gate.Exec(ctx,
		`INSERT INTO source_units (source_id, kind, label, parent_label, title, path, depth, answerable, ord)
		 VALUES ($1, 'entry', $2, $3, 'Сроки', $4, 1, TRUE, 0)`,
		sourceID, label, root, root+"/"+label)
	if err != nil {
		t.Fatalf("единица не заведена: %v", err)
	}
	_, err = gate.Exec(ctx,
		`INSERT INTO source_unit_statements (source_id, unit_label, kind, designation, body_md, place_ref, ord)
		 VALUES ($1, $2, '', 'абз. 1', 'Срок — десять дней.', 'с. 4', 0)`, sourceID, label)
	if err != nil {
		t.Fatalf("положение не заведено: %v", err)
	}

	body := casestore.Body{
		Title: "Срок рассмотрения",
		Kind:  "recognise",
		Segments: []casestore.Segment{
			{Text: "Заявление подано в понедельник.", Statements: []string{"абз. 1"}},
		},
		Options: []casestore.Option{
			{Label: label, Text: "Десять рабочих дней"},
			{Label: root + ".2", Text: "Тридцать календарных дней"},
		},
		Answer:      label,
		Explanation: "Срок назван прямо в положении.",
		Difficulty:  2,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var draftID int64
	err = gate.QueryRow(ctx,
		`INSERT INTO case_drafts (source_id, unit_label, body) VALUES ($1, $2, $3) RETURNING id`,
		sourceID, label, raw).Scan(&draftID)
	if err != nil {
		t.Fatalf("черновик не положен: %v", err)
	}

	store := casestore.NewStore(gate)
	one, err := store.FromDraft(ctx, draftID, "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, one.ID, "проверка"); err != nil {
		t.Fatal(err)
	}
	return one.ID, sourceID
}

func TestPgБезКлючаПрограммыУстройствоНеЗаводится(t *testing.T) {
	// Иначе первый же любопытный завёл бы нам миллион учётных записей, и
	// отличить их от настоящих было бы нечем.
	srv, _, key := дверь(t)

	for _, headers := range []map[string]string{
		{},
		{"X-App-Key": "придуманный.ключ"},
		{"X-App-Key": key + "мусор"},
	} {
		status, _, raw := call(t, srv, "POST", "/v1/devices", headers, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("код %d, ожидался отказ по ключу: %s", status, raw)
		}
	}

	status, _, raw := call(t, srv, "POST", "/v1/devices",
		map[string]string{"X-App-Key": key}, nil)
	if status != http.StatusCreated {
		t.Fatalf("с годным ключом устройство не завелось, код %d: %s", status, raw)
	}
}

func TestPgПервыйЗапускНичегоНеСпрашивает(t *testing.T) {
	// Спрашивать имя и почту у человека, который ещё не понял, что ему
	// предлагают, — верный способ его потерять. Тело запроса
	// необязательно целиком.
	srv, _, key := дверь(t)

	status, body, raw := call(t, srv, "POST", "/v1/devices",
		map[string]string{"X-App-Key": key}, nil)
	if status != http.StatusCreated {
		t.Fatalf("код %d: %s", status, raw)
	}
	if body["token"] == "" || body["accountId"] == nil {
		t.Fatalf("устройство заведено не до конца: %s", raw)
	}

	token, _ := body["token"].(string)
	status, me, raw := call(t, srv, "GET", "/v1/me",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	// Почта пуста, пока врач её не привязал, и это пустая строка, а не
	// null: приложение показывает её как есть.
	if me["email"] != "" {
		t.Fatalf("у новой записи почта не пуста: %s", raw)
	}
}

func TestPgНеизвестныеПоляВЗапросеНеЛомаютДверь(t *testing.T) {
	// На руках у врачей стоят сборки, которые обновятся не завтра, а
	// новая сборка, пославшая поле, которого старый сервер не знает,
	// должна работать. Это та же обратная совместимость, только с другой
	// стороны провода.
	srv, _, key := дверь(t)

	status, _, raw := call(t, srv, "POST", "/v1/devices",
		map[string]string{"X-App-Key": key},
		map[string]any{"platform": "android", "чегоМыЕщёНеЗнаем": "и не узнаем"})
	if status != http.StatusCreated {
		t.Fatalf("сборка с новым полем не завелась, код %d: %s", status, raw)
	}
}

func TestPgБезТокенаУстройстваЗакрытыеРучкиОтказывают(t *testing.T) {
	srv, _, key := дверь(t)
	token := устройство(t, srv, key)

	for _, c := range []struct{ method, path string }{
		{"GET", "/v1/me"},
		{"PUT", "/v1/me"},
		{"GET", "/v1/content/version"},
		{"GET", "/v1/cases"},
		{"DELETE", "/v1/devices/current"},
	} {
		status, _, raw := call(t, srv, c.method, c.path, nil, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s без токена: код %d: %s", c.method, c.path, status, raw)
		}
		// Ключ программы токена не заменяет: он говорит, какая сборка
		// стучится, а не кто. Пускай он по чужим данным — и ключ,
		// вынутый из сборки, открывал бы всё.
		status, _, raw = call(t, srv, c.method, c.path,
			map[string]string{"X-App-Key": key}, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s по одному ключу программы: код %d: %s", c.method, c.path, status, raw)
		}
	}

	status, _, raw := call(t, srv, "GET", "/v1/me",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusOK {
		t.Fatalf("с токеном код %d: %s", status, raw)
	}
}

func TestPgЛентаПоказываетТолькоОпубликованное(t *testing.T) {
	// Черновик, задача на выверке и снятая с раздачи для приложения не
	// существуют. Условие стоит в запросе, а не в отборе после чтения:
	// отбор после чтения однажды забудут, и узнают об этом по черновику
	// на экране у врача.
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)
	root := fmt.Sprintf("лента%d", time.Now().UnixNano())
	caseID, sourceID := задачаПоПути(t, gate, root)

	status, page, raw := call(t, srv, "GET",
		"/v1/cases?limit=100&path="+root,
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if !containsCase(page, caseID) {
		t.Fatalf("опубликованной задачи в ленте нет: %s", raw)
	}

	// Снимаем — и она обязана исчезнуть из ленты, не исчезнув из базы.
	if _, err := casestore.NewStore(gate).Withdraw(context.Background(), caseID, "проверка"); err != nil {
		t.Fatal(err)
	}
	_, page, raw = call(t, srv, "GET", "/v1/cases?limit=100&path="+root,
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if containsCase(page, caseID) {
		t.Fatalf("снятая с раздачи задача осталась в ленте: %s", raw)
	}

	status, _, raw = call(t, srv, "GET", "/v1/cases/"+caseID,
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("снятая задача отдалась поимённо, код %d: %s", status, raw)
	}
	_ = sourceID
}

func containsCase(page map[string]any, id string) bool {
	list, _ := page["cases"].([]any)
	for _, raw := range list {
		one, _ := raw.(map[string]any)
		if one["id"] == id {
			return true
		}
	}
	return false
}

func TestPgСтраницыЛентыИдутКурсоромИНеДублятЗадачи(t *testing.T) {
	// Страницы курсором, а не смещением: между двумя страницами задачу
	// могут выпустить, и при смещении она сдвинет все следующие — врач
	// получит одну задачу дважды, а соседнюю не получит вовсе.
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)

	root := fmt.Sprintf("обход%d", time.Now().UnixNano())
	want := map[string]bool{}
	for i := 0; i < 5; i++ {
		id, _ := задачаПоПути(t, gate, root)
		want[id] = true
	}

	seen, after := map[string]int{}, ""
	for step := 0; step < 50; step++ {
		path := "/v1/cases?limit=2&path=" + root
		if after != "" {
			path += "&after=" + after
		}
		status, page, raw := call(t, srv, "GET", path,
			map[string]string{"Authorization": "Bearer " + token}, nil)
		if status != http.StatusOK {
			t.Fatalf("код %d: %s", status, raw)
		}
		list, _ := page["cases"].([]any)
		for _, item := range list {
			one, _ := item.(map[string]any)
			id, _ := one["id"].(string)
			seen[id]++
		}
		next, _ := page["next"].(string)
		if next == "" {
			break
		}
		// Курсор обязан двигаться: не двинувшийся — это бесконечный
		// обход, и заметят его по счёту за трафик.
		if next == after {
			t.Fatalf("курсор не сдвинулся: %q", next)
		}
		after = next
	}

	for id := range want {
		if seen[id] != 1 {
			t.Errorf("задача %s встретилась %d раз, ожидался один", id, seen[id])
		}
	}
}

func TestPgДочитаннаяЛентаОтдаётПустойСписок_АНеNull(t *testing.T) {
	// Дочитанная до конца лента — ИСПРАВНЫЙ случай, и null уронил бы
	// приложение именно на нём.
	srv, _, key := дверь(t)
	token := устройство(t, srv, key)

	status, page, raw := call(t, srv, "GET", "/v1/cases?path=такого-пути-нет",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if page["cases"] == nil {
		t.Fatalf("лента отдана null, а не пустым списком: %s", raw)
	}
	if list, _ := page["cases"].([]any); len(list) != 0 {
		t.Fatalf("по несуществующему пути приехали задачи: %s", raw)
	}
}

func TestPgОтветыЛентыСходятсяСЭталоном(t *testing.T) {
	// Эталон — единственное, что держит формат /v1 неизменным: на руках у
	// врачей стоят сборки, которые обновятся не завтра.
	contract := loadContract(t)
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)
	root := fmt.Sprintf("эталон%d", time.Now().UnixNano())
	caseID, _ := задачаПоПути(t, gate, root)

	auth := map[string]string{"Authorization": "Bearer " + token}

	_, health, _ := call(t, srv, "GET", "/v1/health", nil, nil)
	matchShape(t, "GET /v1/health", health, contract.Responses["GET /v1/health"].Fields)

	_, me, _ := call(t, srv, "GET", "/v1/me", auth, nil)
	matchShape(t, "GET /v1/me", me, contract.Responses["GET /v1/me"].Fields)

	_, version, _ := call(t, srv, "GET", "/v1/content/version", auth, nil)
	matchShape(t, "GET /v1/content/version", version,
		contract.Responses["GET /v1/content/version"].Fields)

	_, page, raw := call(t, srv, "GET", "/v1/cases?limit=100&path="+root, auth, nil)
	matchShape(t, "GET /v1/cases", page, contract.Responses["GET /v1/cases"].Fields)
	list, _ := page["cases"].([]any)
	if len(list) == 0 {
		t.Fatalf("лента пуста, сверять нечего: %s", raw)
	}
	first, _ := list[0].(map[string]any)
	matchShape(t, "GET /v1/cases[]", first, contract.Responses["GET /v1/cases"].Each["cases"])

	_, one, _ := call(t, srv, "GET", "/v1/cases/"+caseID, auth, nil)
	matchShape(t, "GET /v1/cases/{id}", one, contract.Responses["GET /v1/cases/{id}"].Fields)

	body, _ := one["body"].(map[string]any)
	matchShape(t, "содержание задачи", body, contract.CaseBody.Fields)

	// Отказ тоже описан эталоном: его разбирает то же приложение.
	_, failure, _ := call(t, srv, "GET", "/v1/cases/такой-задачи-нет", auth, nil)
	matchShape(t, "отказ", failure, contract.Errors.Fields)
}

func TestPgЗакрытойЗаписиДверьНеОткрывается(t *testing.T) {
	// Заблокированный врач должен понять, что писать в поддержку, а не
	// переустанавливать приложение по кругу, — потому отказ отдельный.
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)

	caller, err := NewAccounts(gate).ByToken(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(context.Background(),
		`UPDATE accounts SET blocked_at = NOW() WHERE id = $1`, caller.AccountID); err != nil {
		t.Fatal(err)
	}

	status, body, raw := call(t, srv, "GET", "/v1/me",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusForbidden {
		t.Fatalf("код %d, ожидался отказ по блокировке: %s", status, raw)
	}
	if body["error"] == "" {
		t.Fatalf("отказ без объяснения: %s", raw)
	}
}

func TestPgВыходЗабываетОдноУстройство_АНеВсе(t *testing.T) {
	// Врач, вышедший на служебном планшете, не должен вылететь со своего
	// телефона.
	srv, gate, key := дверь(t)
	first := устройство(t, srv, key)

	ctx := context.Background()
	accounts := NewAccounts(gate)
	caller, err := accounts.ByToken(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	// Второе устройство той же записи: заводим напрямую, потому что
	// ручка заведения делает и новую запись тоже.
	second, err := соседнееУстройство(ctx, gate, caller.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	status, _, raw := call(t, srv, "DELETE", "/v1/devices/current",
		map[string]string{"Authorization": "Bearer " + first}, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}

	status, _, raw = call(t, srv, "GET", "/v1/me",
		map[string]string{"Authorization": "Bearer " + first}, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("забытое устройство всё ещё пускают, код %d: %s", status, raw)
	}
	status, _, raw = call(t, srv, "GET", "/v1/me",
		map[string]string{"Authorization": "Bearer " + second}, nil)
	if status != http.StatusOK {
		t.Fatalf("соседнее устройство вылетело вместе с забытым, код %d: %s", status, raw)
	}
}

func TestPgВыключенныйКлючПрограммыНеПодходит(t *testing.T) {
	// Ключ лежит в сборке, то есть в руках у всякого, кто её разобрал.
	// Выключение — единственное, чем на это отвечают.
	srv, gate, key := дверь(t)
	keys := NewKeys(gate)

	keyID := fmt.Sprintf("выключим-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	doomed, err := keys.Issue(context.Background(), keyID, "На выключение")
	if err != nil {
		t.Fatal(err)
	}
	status, _, raw := call(t, srv, "POST", "/v1/devices",
		map[string]string{"X-App-Key": doomed}, nil)
	if status != http.StatusCreated {
		t.Fatalf("свежий ключ не подошёл, код %d: %s", status, raw)
	}

	if err := keys.Disable(context.Background(), keyID); err != nil {
		t.Fatal(err)
	}
	status, _, raw = call(t, srv, "POST", "/v1/devices",
		map[string]string{"X-App-Key": doomed}, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("выключенный ключ подошёл, код %d: %s", status, raw)
	}

	// Строка ключа при этом остаётся: он стоит в журналах обращений, и
	// удалённый оставил бы их ссылающимися в никуда.
	var alive int
	if err := gate.QueryRow(context.Background(),
		`SELECT count(*) FROM app_keys WHERE key_id = $1`, keyID).Scan(&alive); err != nil {
		t.Fatal(err)
	}
	if alive != 1 {
		t.Fatal("выключенный ключ удалён из базы")
	}
	_ = key
}

func TestPgТокенУстройстваНеХранитсяТекстом(t *testing.T) {
	// Утёкшая база не должна давать входа.
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)

	var found int
	err := gate.QueryRow(context.Background(),
		`SELECT count(*) FROM devices WHERE token_hash = $1`, token).Scan(&found)
	if err != nil {
		t.Fatal(err)
	}
	if found != 0 {
		t.Fatal("токен устройства лежит в базе текстом")
	}
}

// соседнееУстройство заводит второе устройство той же учётной записи.
func соседнееУстройство(ctx context.Context, gate *dbgate.Gate, accountID int64) (string, error) {
	token := fmt.Sprintf("соседний-токен-%d-%d", time.Now().UnixNano(), rand.Intn(100000))
	_, err := gate.Exec(ctx,
		`INSERT INTO devices (account_id, token_hash, platform, last_seen)
		 VALUES ($1, $2, 'android', NOW())`, accountID, fingerprint(token))
	if err != nil {
		return "", err
	}
	return token, nil
}

// столСтудии поднимает студию с ручками ключей и заводит человека с
// названными правами.
func столСтудии(t *testing.T, perms ...studio.Permission) (*httptest.Server, string, *dbgate.Gate) {
	t.Helper()
	gate := testGate(t)
	users, sessions := studio.NewUsers(gate), studio.NewSessions(gate)
	desk := studio.NewDesk(users, sessions)
	studio.Routes(desk)
	KeyRoutes(desk, NewKeys(gate))

	ctx := context.Background()
	login := fmt.Sprintf("мастер-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	user, _, err := users.Create(ctx, login, "Мастер", perms)
	if err != nil {
		t.Fatalf("пользователь студии не заведён: %v", err)
	}
	token, err := sessions.Issue(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatalf("сессия не выдана: %v", err)
	}
	srv := httptest.NewServer(desk.Handler())
	t.Cleanup(srv.Close)
	return srv, token, gate
}

func TestPgКлючПрограммыПоказываетсяОдинРаз(t *testing.T) {
	// Дальше в базе лежит только отпечаток, и «напомнить ключ»
	// невозможно устройством дела: возможность напомнить означала бы, что
	// утёкшая база даёт ключи.
	srv, token, _ := столСтудии(t, studio.PermWorkshop)
	auth := map[string]string{"Authorization": "Bearer " + token}

	keyID := fmt.Sprintf("сборка-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	status, issued, raw := call(t, srv, "POST", "/admin/api/app-keys", auth,
		map[string]any{"keyId": keyID, "title": "Android"})
	if status != http.StatusCreated {
		t.Fatalf("код %d: %s", status, raw)
	}
	key, _ := issued["key"].(string)
	if key == "" {
		t.Fatalf("ключ не выдан: %s", raw)
	}
	// Рядом с ключом сказано, что второго раза не будет: человек,
	// закрывший окно не сохранив, иначе пойдёт искать его в студии.
	if note, _ := issued["note"].(string); note == "" {
		t.Fatalf("ключ выдан без предупреждения: %s", raw)
	}

	status, list, raw := call(t, srv, "GET", "/admin/api/app-keys", auth, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if strings.Contains(raw, key) {
		t.Fatalf("ключ виден в списке целиком: %s", raw)
	}
	if list["keys"] == nil {
		t.Fatalf("ключи отданы null, а не списком: %s", raw)
	}
}

func TestPgБезПраваМастерскойКлючиНеВидны(t *testing.T) {
	// Ключ — это настройка службы, а не работа над задачами. Составитель,
	// пишущий задачи, ключи не заводит и знать о них не должен.
	srv, token, _ := столСтудии(t, studio.PermCaseWrite)
	auth := map[string]string{"Authorization": "Bearer " + token}

	for _, c := range []struct{ method, path string }{
		{"GET", "/admin/api/app-keys"},
		{"POST", "/admin/api/app-keys"},
		{"POST", "/admin/api/app-keys/какой-нибудь/disable"},
	} {
		status, _, raw := call(t, srv, c.method, c.path, auth, nil)
		if status != http.StatusForbidden {
			t.Errorf("%s %s: код %d, ожидался отказ по праву: %s", c.method, c.path, status, raw)
		}
	}
}

func TestPgВрачВидитСвоиПраваИСрок(t *testing.T) {
	// Витрина говорила, куплен ли отдельный набор, а «что у меня вообще
	// есть и до когда» не говорил никто. Врач, заплативший за подписку,
	// видел ровно то же, что и не заплативший, и выяснять, дошли ли
	// деньги, ему приходилось у нас.
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}

	_, до, _ := call(t, srv, "GET", "/v1/me", auth, nil)
	права, ok := до["rights"].([]any)
	if !ok {
		t.Fatalf("прав нет даже пустым списком: %#v", до["rights"])
	}
	if len(права) != 0 {
		t.Fatalf("у нового врача уже %d прав", len(права))
	}

	// Право выдаётся платежом, а не заведением устройства: это и есть
	// отвязка права от способа оплаты, и проверяется оно одинаково,
	// чем бы ни было куплено.
	accountID := int64(до["accountId"].(float64))
	срок := time.Now().Add(30 * 24 * time.Hour)
	if _, err := gate.Exec(context.Background(),
		`INSERT INTO entitlements (account_id, kind, origin, expires_at)
		 VALUES ($1, 'subscription', 'subscription', $2)`, accountID, срок); err != nil {
		t.Fatal(err)
	}

	_, после, _ := call(t, srv, "GET", "/v1/me", auth, nil)
	права, _ = после["rights"].([]any)
	if len(права) != 1 {
		t.Fatalf("куплённое право до врача не доехало: %#v", после["rights"])
	}
	одно := права[0].(map[string]any)
	if одно["kind"] != "subscription" {
		t.Errorf("род права приехал как %q", одно["kind"])
	}
	if одно["expiresAt"] == "" {
		t.Error("срок подписки пуст: врач прочтёт это как «бессрочно»")
	}
}

func TestPgБессрочноеПравоОтдаётСрокПустойСтрокой(t *testing.T) {
	// Не отсутствием поля: отсутствие приложение прочло бы как старый
	// сервер и показало бы «неизвестно», а пустая строка говорит то, что
	// есть, — срока нет.
	srv, gate, key := дверь(t)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}

	_, me, _ := call(t, srv, "GET", "/v1/me", auth, nil)
	accountID := int64(me["accountId"].(float64))
	if _, err := gate.Exec(context.Background(),
		`INSERT INTO entitlements (account_id, kind, origin) VALUES ($1, 'subscription', 'grant')`,
		accountID); err != nil {
		t.Fatal(err)
	}

	_, после, _ := call(t, srv, "GET", "/v1/me", auth, nil)
	одно := после["rights"].([]any)[0].(map[string]any)
	срок, есть := одно["expiresAt"]
	if !есть {
		t.Fatal("поля срока нет вовсе")
	}
	if срок != "" {
		t.Errorf("бессрочное право назвало срок %q", срок)
	}
}
