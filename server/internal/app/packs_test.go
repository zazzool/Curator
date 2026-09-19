package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/packs"
	"curator/server/internal/sales"
)

// витрина поднимает /v1 с ручками наборов и заведённым ключом подписи.
func витрина(t *testing.T) (*httptest.Server, *dbgate.Gate, string, *packs.Store, ed25519.PublicKey) {
	t.Helper()
	gate := testGate(t)
	keys := NewKeys(gate)

	keyID := fmt.Sprintf("набор-%d", time.Now().UnixNano())
	key, err := keys.Issue(context.Background(), keyID, "Проверка")
	if err != nil {
		t.Fatalf("ключ программы не заведён: %v", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store := packs.NewStore(gate, priv, "проверка")

	door := NewDoor(keys, NewAccounts(gate))
	door.Keyed("POST /v1/devices", (&routes{accounts: NewAccounts(gate)}).enroll)
	PackRoutes(door, store, sales.NewAccess(gate), sales.NewPrices(gate))

	srv := httptest.NewServer(door.Handler())
	t.Cleanup(srv.Close)
	return srv, gate, key, store, pub
}

func TestPgОписьВыпускаСходитсяСЭталономИПодписьСверяется(t *testing.T) {
	// Опись и подпись — то, по чему приложение решает, верить ли набору.
	// Формат, не записанный в эталон, держится только памятью того, кто
	// его писал, а разбирает его приложение на другом языке.
	contract := loadContract(t)
	srv, gate, key, store, pub := витрина(t)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}

	slug := fmt.Sprintf("nabor-%d", time.Now().UnixNano())
	if err := store.Create(context.Background(), slug, "Набор для проверки", "Описание"); err != nil {
		t.Fatal(err)
	}
	caseID, _ := задача(t, gate)
	if _, err := store.SetItems(context.Background(), slug, []string{caseID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(context.Background(), slug, time.Now()); err != nil {
		t.Fatal(err)
	}

	_, shelf, raw := call(t, srv, "GET", "/v1/packs", auth, nil)
	matchShape(t, "GET /v1/packs", shelf, contract.Responses["GET /v1/packs"].Fields)
	list, _ := shelf["packs"].([]any)
	if len(list) == 0 {
		t.Fatalf("витрина пуста, сверять нечего: %s", raw)
	}
	matchShape(t, "GET /v1/packs[]", list[0].(map[string]any),
		contract.Responses["GET /v1/packs"].Each["packs"])

	_, release, raw := call(t, srv, "GET", "/v1/packs/"+slug, auth, nil)
	matchShape(t, "GET /v1/packs/{slug}", release,
		contract.Responses["GET /v1/packs/{slug}"].Fields)
	items, _ := release["cases"].([]any)
	if len(items) != 1 {
		t.Fatalf("в описи %d задач, ожидалась одна: %s", len(items), raw)
	}
	matchShape(t, "GET /v1/packs/{slug}[]", items[0].(map[string]any),
		contract.Responses["GET /v1/packs/{slug}"].Each["cases"])

	// И главное: подпись, приехавшая по проводу, сходится тем же путём,
	// каким её сверит устройство.
	stored, err := store.Latest(context.Background(), slug)
	if err != nil {
		t.Fatal(err)
	}
	signature, _ := release["signature"].(string)
	if err := packs.Verify(stored.Manifest, signature, pub); err != nil {
		t.Errorf("подпись из ответа не сошлась: %v", err)
	}

	_, bodies, raw := call(t, srv, "GET", "/v1/packs/"+slug+"/cases?limit=10", auth, nil)
	matchShape(t, "GET /v1/packs/{slug}/cases", bodies,
		contract.Responses["GET /v1/packs/{slug}/cases"].Fields)
	downloaded, _ := bodies["cases"].([]any)
	if len(downloaded) != 1 {
		t.Fatalf("скачалось %d задач: %s", len(downloaded), raw)
	}
	matchShape(t, "GET /v1/packs/{slug}/cases[]", downloaded[0].(map[string]any),
		contract.Responses["GET /v1/packs/{slug}/cases"].Each["cases"])
}

func TestPgОтпечатокИзОписиСходитсяСоСкачаннойЗадачей(t *testing.T) {
	// Устройство качает задачи по одной и проверяет каждую по своему
	// отпечатку. Разойдись отпечаток и содержание — и установка встанет на
	// исправном наборе.
	srv, gate, key, store, _ := витрина(t)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}

	slug := fmt.Sprintf("nabor-%d", time.Now().UnixNano())
	if err := store.Create(context.Background(), slug, "Набор", ""); err != nil {
		t.Fatal(err)
	}
	caseID, _ := задача(t, gate)
	if _, err := store.SetItems(context.Background(), slug, []string{caseID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(context.Background(), slug, time.Now()); err != nil {
		t.Fatal(err)
	}

	_, release, _ := call(t, srv, "GET", "/v1/packs/"+slug, auth, nil)
	items, _ := release["cases"].([]any)
	want, _ := items[0].(map[string]any)["hash"].(string)

	var body []byte
	if err := gate.QueryRow(context.Background(),
		`SELECT body FROM cases WHERE id = $1`, caseID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	got, err := packs.HashBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("отпечаток в описи %s, у содержания %s", want, got)
	}
}

func TestPgНесуществующийНаборНеРассказываетОСебе(t *testing.T) {
	srv, _, key, _, _ := витрина(t)
	token := устройство(t, srv, key)
	status, body, raw := call(t, srv, "GET", "/v1/packs/nabora-net",
		map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("код %d, ожидался отказ: %s", status, raw)
	}
	if text, _ := body["error"].(string); text == "" {
		t.Error("отказ приехал без внятного текста")
	}
}

func TestPgПлатныйНаборНеОтдаётСодержаниеБезПрава(t *testing.T) {
	// Здесь сходятся две половины, написанные порознь: продажи знают про
	// право, витрина — про набор. Проверка живёт в витрине, а не в
	// продажах, потому что ошибиться можно ровно на стыке — отдать
	// содержание тому, кто его не купил, и узнать об этом по выручке.
	contract := loadContract(t)
	srv, gate, key, store, _ := витрина(t)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}
	ctx := context.Background()

	slug := fmt.Sprintf("nabor-%d", time.Now().UnixNano())
	if err := store.Create(ctx, slug, "Платный набор", "Описание"); err != nil {
		t.Fatal(err)
	}
	caseID, _ := задача(t, gate)
	if _, err := store.SetItems(ctx, slug, []string{caseID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, slug, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := sales.NewPrices(gate).Set(ctx, "pack:"+slug, 49900, true); err != nil {
		t.Fatal(err)
	}

	// Витрина показывает цену и говорит, что набор ещё не куплен. Без
	// этих двух полей врач выясняет цену покупкой.
	_, shelf, raw := call(t, srv, "GET", "/v1/packs", auth, nil)
	one := наборВВитрине(t, shelf, slug, raw)
	matchShape(t, "GET /v1/packs[]", one, contract.Responses["GET /v1/packs"].Each["packs"])
	if kopecks, _ := one["kopecks"].(float64); int64(kopecks) != 49900 {
		t.Errorf("на витрине цена %v, ожидались 49900 копеек: %s", one["kopecks"], raw)
	}
	if owned, _ := one["owned"].(bool); owned {
		t.Errorf("набор показан купленным до покупки: %s", raw)
	}

	status, body, raw := call(t, srv, "GET", "/v1/packs/"+slug+"/cases", auth, nil)
	if status != http.StatusPaymentRequired {
		t.Fatalf("закрытый набор отдался с кодом %d: %s", status, raw)
	}
	if text, _ := body["error"].(string); text == "" {
		t.Error("отказ по оплате приехал без внятного текста")
	}

	// Опись при этом открыта: по номерам и отпечаткам не позанимаешься, а
	// витрина без неё не покажет, что внутри.
	if status, _, raw := call(t, srv, "GET", "/v1/packs/"+slug, auth, nil); status != http.StatusOK {
		t.Fatalf("опись закрылась вместе с содержанием, код %d: %s", status, raw)
	}

	if _, err := sales.NewPayments(gate).Accept(ctx, sales.Income{
		AccountID: запись(t, gate, token),
		Purpose:   "pack:" + slug,
		Kopecks:   49900,
		IdemKey:   fmt.Sprintf("проверка-%d", time.Now().UnixNano()),
		By:        "проверка",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	status, bodies, raw := call(t, srv, "GET", "/v1/packs/"+slug+"/cases?limit=10", auth, nil)
	if status != http.StatusOK {
		t.Fatalf("купленный набор не отдался, код %d: %s", status, raw)
	}
	if list, _ := bodies["cases"].([]any); len(list) != 1 {
		t.Fatalf("скачалось %d задач вместо одной: %s", len(list), raw)
	}

	_, shelf, raw = call(t, srv, "GET", "/v1/packs", auth, nil)
	if owned, _ := наборВВитрине(t, shelf, slug, raw)["owned"].(bool); !owned {
		t.Errorf("после покупки витрина не показала набор купленным: %s", raw)
	}
}

// наборВВитрине достаёт из ответа витрины нужный набор.
//
// Именно нужный, а не первый: база одна на весь прогон, наборы в ней
// копятся от чужих проверок, и «первый в списке» перестал бы быть своим
// на второй проверке.
func наборВВитрине(t *testing.T, shelf map[string]any, slug, raw string) map[string]any {
	t.Helper()
	list, _ := shelf["packs"].([]any)
	for _, item := range list {
		one, _ := item.(map[string]any)
		if got, _ := one["slug"].(string); got == slug {
			return one
		}
	}
	t.Fatalf("набора %s на витрине нет: %s", slug, raw)
	return nil
}

// запись отдаёт номер учётной записи по выданному токену.
func запись(t *testing.T, gate *dbgate.Gate, token string) int64 {
	t.Helper()
	var id int64
	if err := gate.QueryRow(context.Background(),
		`SELECT account_id FROM devices WHERE token_hash = $1`,
		fingerprint(token)).Scan(&id); err != nil {
		t.Fatalf("учётная запись устройства не найдена: %v", err)
	}
	return id
}
