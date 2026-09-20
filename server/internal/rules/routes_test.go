package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"curator/server/internal/studio"
)

// newDesk поднимает стол студии с ручками свода и заводит человека с
// названными правами.
//
// Права называются поимённо: «маршрута без права не бывает» проверяется
// не тем, что маршрут отвечает, а тем, что он отказывает человеку без
// права.
func newDesk(t *testing.T, perms ...studio.Permission) (*httptest.Server, string, *Store) {
	t.Helper()
	gate := testGate(t)
	users, sessions := studio.NewUsers(gate, nil), studio.NewSessions(gate)
	desk := studio.NewDesk(users, sessions)
	studio.Routes(desk)

	store := NewStore(gate)
	Routes(desk, store)

	ctx := context.Background()
	login := fmt.Sprintf("свод-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	user, _, err := users.Create(ctx, login, "Составитель", perms)
	if err != nil {
		t.Fatalf("пользователь студии не заведён: %v", err)
	}
	token, err := sessions.Issue(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatalf("сессия не выдана: %v", err)
	}

	srv := httptest.NewServer(desk.Handler())
	t.Cleanup(srv.Close)
	return srv, token, store
}

func call(t *testing.T, srv *httptest.Server, token, method, path string, body any) (int, map[string]any) {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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
	return resp.StatusCode, parsed
}

func TestPgСводЗакрытПравомЗаданий(t *testing.T) {
	// Свод уходит в задание блоком: правя правило, составитель решает,
	// какими будут все будущие задачи. Открой ручку правом заказа — и
	// тот, кому доверена одна задача, получил бы власть над всеми.
	srv, token, _ := newDesk(t, studio.PermGenerate)
	if status, _ := call(t, srv, token, "GET", "/admin/api/rules", nil); status != http.StatusForbidden {
		t.Fatalf("свод открылся по праву заказа: %d", status)
	}
	if status, _ := call(t, srv, token, "POST", "/admin/api/rules", ruleRequest{
		Title: "Чужое правило", Text: "Написано без права.", Kind: string(KindStructure),
	}); status != http.StatusForbidden {
		t.Fatalf("правило записалось по праву заказа: %d", status)
	}
}

func TestPgПравкаПравилаНеСбрасываетНакопленное(t *testing.T) {
	// Живой класс отказа: сборка правила из одного запроса обнуляет
	// счётчики. Правило, дозревавшее неделю, после первой же правки
	// формулировки вернулось бы в кандидаты — то есть перестало бы
	// уходить в задание, и заметили бы это по качеству задач, а не по
	// экрану.
	srv, token, store := newDesk(t, studio.PermPrompts)
	ctx := context.Background()

	id := свойID("накопленное")
	if _, err := store.Save(ctx, Rule{
		ID:            id,
		Title:         "Слово «явка» в условии",
		Text:          "Не пиши в условии слово «явка».",
		Kind:          KindSubstance,
		Source:        FromLint,
		Scope:         Scope{Sources: []int64{4242}},
		Confirmations: Quorum,
		SeenJobs:      []int64{900, 901, 902},
	}); err != nil {
		t.Fatal(err)
	}

	status, body := call(t, srv, token, "PUT", "/admin/api/rules/"+id, ruleRequest{
		Title: "Слово «явка» в условии",
		Text:  "Переписанная формулировка того же правила.",
		Kind:  string(KindSubstance),
		Scope: Scope{Sources: []int64{4242}},
	})
	if status != http.StatusOK {
		t.Fatalf("правка не прошла: %d %v", status, body)
	}
	if got, _ := body["confirmations"].(float64); int(got) != Quorum {
		t.Fatalf("подтверждения сброшены правкой: %v", body["confirmations"])
	}
	if body["status"] != string(Active) {
		t.Fatalf("дозревшее правило после правки стало «%v»", body["status"])
	}
	if body["text"] != "Переписанная формулировка того же правила." {
		t.Fatalf("текст не сохранился: %v", body["text"])
	}
}

func TestPgПравилоСоставителяДействуетСразуИНеПадаетВКандидаты(t *testing.T) {
	// Он и есть подтверждение. Пересчитай счётчик состояние — и ручка
	// ответила бы успехом, показав «кандидат»: составитель считал бы,
	// что правило написано и работает, а оно не уходило бы никуда.
	srv, token, _ := newDesk(t, studio.PermPrompts)

	status, body := call(t, srv, token, "POST", "/admin/api/rules", ruleRequest{
		Title: "В условии не бывает служебных обозначений",
		Text:  "Не пиши в условии служебных обозначений документа.",
		Kind:  string(KindStructure),
	})
	if status != http.StatusOK {
		t.Fatalf("правило не записалось: %d %v", status, body)
	}
	if body["status"] != string(Active) {
		t.Fatalf("написанное составителем правило заведено как «%v»", body["status"])
	}
	if body["source"] != string(FromCurator) {
		t.Fatalf("источник правила «%v» вместо составителя", body["source"])
	}
	if body["pinned"] != true {
		t.Fatal("состояние не отмечено назначенным человеком: счётчик уведёт его в кандидаты")
	}
}

func TestPgИсточникПравилаНеНазываетсяИзвне(t *testing.T) {
	// Назови правило «встроенным» — и оно получило бы чужое старшинство
	// при споре правил, то есть выиграло бы у написанного другим
	// составителем. Опознаватель «builtin:show-dont-name» сделал бы то же
	// грубее: правило встало бы на место встроенного.
	//
	// Дверей тут две, и проверяются обе. Прежде здесь проверялась одна, и
	// не та: запрос с лишними полями до присваивания не доезжает вовсе, а
	// проверка читала источник из тела ОТКАЗА и видела там пустоту.
	srv, token, _ := newDesk(t, studio.PermPrompts)

	// Первая дверь — разбор тела. Лишнее поле редакционному API отказ, а
	// не тихо отброшенное поле: отброшенное молча, оно вернуло бы
	// пославшему успех на запрос, который сервер исполнил не так.
	status, _ := call(t, srv, token, "POST", "/admin/api/rules", map[string]any{
		"title":  "Правило с чужим старшинством",
		"text":   "Текст этого правила ничем не примечателен.",
		"kind":   string(KindStructure),
		"source": string(Builtin),
		"id":     "builtin:show-dont-name",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("источник и опознаватель приняты извне: %d", status)
	}

	// Вторая — само заведение. Исправный запрос источника не называет
	// вовсе, и ставит его сервер; опознаватель он даёт свой, с приставкой
	// составителя, и встать на место встроенного правила таким нельзя.
	status, body := call(t, srv, token, "POST", "/admin/api/rules", ruleRequest{
		Title: "Правило без чужого старшинства",
		Text:  "Текст этого правила ничем не примечателен.",
		Kind:  string(KindStructure),
	})
	if status != http.StatusOK {
		t.Fatalf("правило не записалось: %d %v", status, body)
	}
	if body["source"] != string(FromCurator) {
		t.Fatalf("источник правила «%v» вместо составителя", body["source"])
	}
	id, _ := body["id"].(string)
	if !strings.HasPrefix(id, "curator:") {
		t.Fatalf("опознаватель «%s» дан не сервером", id)
	}
}

func TestPgПустойСводОтдаётсяСпискомАНеNULL(t *testing.T) {
	// Студия ходит по своду циклом и падает белым экраном ровно на
	// исправном случае — на новой установке, где правил ещё нет.
	srv, token, _ := newDesk(t, studio.PermPrompts)
	status, body := call(t, srv, token, "GET", "/admin/api/rules", nil)
	if status != http.StatusOK {
		t.Fatalf("свод не прочитан: %d %v", status, body)
	}
	if _, ok := body["rules"].([]any); !ok {
		t.Fatalf("свод отдан не списком: %#v", body["rules"])
	}
}
