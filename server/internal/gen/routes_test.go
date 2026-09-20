package gen

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

// newDesk поднимает стол студии с ручками генерации и заводит человека с
// названными правами.
//
// Права называются поимённо в каждой проверке: «маршрута без права не
// бывает» проверяется не тем, что маршрут отвечает, а тем, что он
// отказывает человеку без права.
// каталогМоделей — прайс проверочной установки.
//
// Две строки, а не пустой список: пустой прошёл бы и на ручке, которая
// список теряет, и отличить «моделей нет» от «список потерян» было бы
// нечем.
func каталогМоделей(context.Context) ([]ModelChoice, error) {
	return []ModelChoice{
		{Provider: "openrouter", Model: "дорогая/рассуждающая", PromptNanoUSD: 3000, CompletionNanoUSD: 15000},
		{Provider: "openrouter", Model: "дешёвая/быстрая", PromptNanoUSD: 100, CompletionNanoUSD: 400},
	}, nil
}

func newDesk(t *testing.T, perms ...studio.Permission) (*httptest.Server, string) {
	t.Helper()
	return newDeskModels(t, каталогМоделей, perms...)
}

// newDeskModels — та же студия, но со своим списком моделей.
//
// Нужна там, где предмет проверки — САМ список: пустой и потерянный
// выглядят одинаково у ручки, которая вместо списка отдаёт пустое
// значение, и отличить их можно только прогнав оба.
func newDeskModels(t *testing.T, models ModelLister, perms ...studio.Permission) (*httptest.Server, string) {
	t.Helper()
	return newDeskWith(t, models, nil, perms...)
}

// newDeskWith — та же студия со своим списком моделей и своим учётом.
//
// Учёт отдельным доводом потому, что пустой учёт — законное состояние
// свежей установки, и экран конвейера обязан работать без него: узлы
// настраивают до того, как по ним что-то прошло.
func newDeskWith(t *testing.T, models ModelLister, stats NodeStats,
	perms ...studio.Permission) (*httptest.Server, string) {
	t.Helper()
	gate := testGate(t)
	users, sessions := studio.NewUsers(gate, nil), studio.NewSessions(gate)
	desk := studio.NewDesk(users, sessions)
	studio.Routes(desk)

	prompts := NewPrompts(gate)
	if err := prompts.Seed(context.Background()); err != nil {
		t.Fatalf("затравки заданий не положены: %v", err)
	}
	Routes(desk, NewJobs(gate), NewResolver(gate), prompts, models, stats)

	ctx := context.Background()
	login := fmt.Sprintf("проверка-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
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
	return srv, token
}

func call(t *testing.T, srv *httptest.Server, token, method, path string, body any) (int, map[string]any, string) {
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
	return resp.StatusCode, parsed, string(raw)
}

func TestPgЗаказСтавитсяВОчередьИВиденВСписке(t *testing.T) {
	srv, token := newDesk(t, studio.PermGenerate)
	sourceID := источник(t, testGate(t))

	status, body, raw := call(t, srv, token, "POST",
		fmt.Sprintf("/admin/api/sources/%d/orders", sourceID),
		map[string]any{"unitLabel": "3.1", "kind": "recognise"})
	if status != http.StatusCreated {
		t.Fatalf("код %d: %s", status, raw)
	}
	if body["status"] != StatusQueued {
		t.Fatalf("заказ встал не в очередь: %s", raw)
	}
	// Словарь источника уезжает в ответ вместе с заданием: студия рисует
	// очередь словами источника («пункт 3.1»), а не своими.
	if body["unitWord"] != "пункт" || body["unitTitle"] != "Сроки" {
		t.Fatalf("словарь источника до студии не доехал: %s", raw)
	}

	status, list, raw := call(t, srv, token, "GET",
		fmt.Sprintf("/admin/api/sources/%d/jobs", sourceID), nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	jobs, ok := list["jobs"].([]any)
	if !ok || len(jobs) != 1 {
		t.Fatalf("в списке не одно задание: %s", raw)
	}
}

func TestPgЗаказПоГруппеОтказываетСразу(t *testing.T) {
	// Разрешение идёт при заказе, а не у исполнителя: составитель должен
	// узнать «по этой единице писать нельзя» сразу, а не через час
	// отказом задания, когда он уже занят другим.
	srv, token := newDesk(t, studio.PermGenerate)
	sourceID := источник(t, testGate(t))

	status, body, raw := call(t, srv, token, "POST",
		fmt.Sprintf("/admin/api/sources/%d/orders", sourceID),
		map[string]any{"unitLabel": "3"})
	if status != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался отказ: %s", status, raw)
	}
	// Текст отказа уезжает как есть и говорит, что не так, — и с большой
	// буквы: наружу едет предложение, а не обрывок журнала.
	msg, _ := body["error"].(string)
	if msg == "" || !strings.ContainsRune("АБВГДЕЁЖЗИКЛМНОПРСТУФХЦЧШЩЭЮЯ", []rune(msg)[0]) {
		t.Fatalf("отказ нечитаем: %s", raw)
	}
}

func TestPgБезПраваГенерацииРучкиОтказывают(t *testing.T) {
	// Раздел закрыт правом на сервере, а не спрятан в студии: скрытая
	// вкладка при открытой ручке — подсказка, где искать, а не запрет.
	srv, token := newDesk(t, studio.PermSourceRead)
	sourceID := источник(t, testGate(t))

	for _, c := range []struct{ method, path string }{
		{"POST", fmt.Sprintf("/admin/api/sources/%d/orders", sourceID)},
		{"GET", fmt.Sprintf("/admin/api/sources/%d/jobs", sourceID)},
		{"GET", "/admin/api/jobs/1"},
		{"POST", "/admin/api/jobs/1/cancel"},
		{"GET", "/admin/api/prompts"},
		{"PUT", "/admin/api/prompts/compose-default"},
	} {
		status, _, raw := call(t, srv, token, c.method, c.path, nil)
		if status != http.StatusForbidden {
			t.Errorf("%s %s: код %d, ожидался отказ по праву: %s",
				c.method, c.path, status, raw)
		}
	}
}

func TestPgПравоНаЗаказНеДаётПравкиЗаданийМоделям(t *testing.T) {
	// Заказать одну задачу и переписать задание модели — разные права:
	// правка задания решает, какими будут ВСЕ будущие задачи.
	srv, token := newDesk(t, studio.PermGenerate)

	status, _, raw := call(t, srv, token, "GET", "/admin/api/prompts", nil)
	if status != http.StatusForbidden {
		t.Fatalf("код %d, ожидался отказ по праву: %s", status, raw)
	}
}

func TestPgЗаданиеМоделиПравитсяСоСверкойРедакции(t *testing.T) {
	srv, token := newDesk(t, studio.PermPrompts)

	status, list, raw := call(t, srv, token, "GET", "/admin/api/prompts", nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	prompts, _ := list["prompts"].([]any)
	if len(prompts) < len(Nodes) {
		t.Fatalf("затравок меньше, чем узлов: %s", raw)
	}
	first, _ := prompts[0].(map[string]any)
	id, _ := first["id"].(string)
	revision, _ := first["revision"].(float64)
	// Узел уезжает и меткой, и словом: составитель читает «написание», а
	// студия сортирует по метке.
	if first["nodeWord"] == "" || first["nodeWord"] == first["node"] {
		t.Fatalf("узел без русского имени: %s", raw)
	}

	status, saved, raw := call(t, srv, token, "PUT", "/admin/api/prompts/"+id,
		map[string]any{
			"name": "Правленое", "systemMd": "Новое задание {источник}.",
			"userMd": "Единица: {метка}.", "revision": int(revision),
		})
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if saved["revision"].(float64) != revision+1 {
		t.Fatalf("редакция не выросла: %s", raw)
	}

	// Вторая правка той же редакцией — это второй составитель, открывший
	// задание до первого. Молча затерев его, студия отдала бы пропажу
	// правки на разбор тому, чья правка пропала, через неделю по качеству
	// задач.
	status, _, raw = call(t, srv, token, "PUT", "/admin/api/prompts/"+id,
		map[string]any{
			"name": "Ещё раз", "systemMd": "Третье задание.",
			"userMd": "Единица: {метка}.", "revision": int(revision),
		})
	if status != http.StatusConflict {
		t.Fatalf("код %d, ожидался отказ по редакции: %s", status, raw)
	}
}

func TestPgПрежняяРедакцияЗаданияОстаётсяВИстории(t *testing.T) {
	// Сравнить «до» и «после» нужно ровно тогда, когда качество задач
	// поехало, а помнить, что меняли неделю назад, уже некому.
	gate := testGate(t)
	prompts := NewPrompts(gate)
	ctx := context.Background()
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := prompts.ForNode(ctx, NodeCompose, 0)
	if err != nil {
		t.Fatal(err)
	}

	after, err := prompts.Save(ctx, Prompt{
		ID: before.ID, Name: before.Name,
		SystemMd: "Переписано.", UserMd: before.UserMd, Revision: before.Revision,
	}, "составитель")
	if err != nil {
		t.Fatal(err)
	}
	if after.SystemMd != "Переписано." || after.Revision != before.Revision+1 {
		t.Fatalf("правка не сохранена: %+v", after)
	}
	// Узел при правке не меняется: он задан устройством конвейера, а не
	// тем, что прислала студия.
	if after.Node != before.Node {
		t.Fatalf("узел задания подменён: %q вместо %q", after.Node, before.Node)
	}

	var kept string
	err = gate.QueryRow(ctx,
		`SELECT system_md FROM prompt_revisions WHERE prompt_id = $1 AND revision = $2`,
		before.ID, before.Revision).Scan(&kept)
	if err != nil {
		t.Fatalf("прежняя редакция не найдена: %v", err)
	}
	if kept != before.SystemMd {
		t.Fatalf("в историю ушёл не прежний текст:\n%s", kept)
	}

	// Возврат к прежнему тексту восстанавливаем через ForNode: умолчание
	// узла остаётся одно, и следующая проверка не должна получить
	// «Переписано.» вместо затравки.
	if _, err := prompts.Save(ctx, Prompt{
		ID: before.ID, Name: before.Name,
		SystemMd: before.SystemMd, UserMd: before.UserMd, Revision: after.Revision,
	}, "составитель"); err != nil {
		t.Fatal(err)
	}
}

func TestPgПустоеЗаданиеНеСохраняется(t *testing.T) {
	// Модель, получившая пустое задание, ответит чем угодно, и ответ этот
	// будет выглядеть работой.
	gate := testGate(t)
	prompts := NewPrompts(gate)
	ctx := context.Background()
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := prompts.ForNode(ctx, NodeVerify, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prompts.Save(ctx, Prompt{
		ID: before.ID, SystemMd: "   ", UserMd: "\n", Revision: before.Revision,
	}, "составитель"); err == nil {
		t.Fatal("пустое задание сохранилось")
	}
}

func TestPgЧерновикиОтдаютсяПустымСписком_АНеNull(t *testing.T) {
	// Пустой список — [], а не null: студия ходит по нему циклом, и null
	// роняет её на ИСПРАВНОМ случае — на задании, которое ещё в очереди.
	srv, token := newDesk(t, studio.PermGenerate)
	sourceID := источник(t, testGate(t))

	_, placed, raw := call(t, srv, token, "POST",
		fmt.Sprintf("/admin/api/sources/%d/orders", sourceID),
		map[string]any{"unitLabel": "3.1"})
	id, ok := placed["id"].(float64)
	if !ok {
		t.Fatalf("задание без номера: %s", raw)
	}

	status, _, raw := call(t, srv, token, "GET",
		fmt.Sprintf("/admin/api/jobs/%d", int64(id)), nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if !strings.Contains(raw, `"drafts":[]`) {
		t.Fatalf("черновики отданы не пустым списком: %s", raw)
	}
}

func TestPgОтменённоеЗаданиеИсполнителюНеДостаётся(t *testing.T) {
	// Отмена — это то, чем составитель останавливает ошибочный заказ.
	// Задание, отменённое и всё же взятое исполнителем, стоит денег и
	// пишет задачу, которую никто не ждёт.
	gate := testGate(t)
	srv, token := newDesk(t, studio.PermGenerate)
	sourceID := источник(t, gate)

	_, placed, raw := call(t, srv, token, "POST",
		fmt.Sprintf("/admin/api/sources/%d/orders", sourceID),
		map[string]any{"unitLabel": "3.1"})
	id, ok := placed["id"].(float64)
	if !ok {
		t.Fatalf("задание без номера: %s", raw)
	}

	status, _, raw := call(t, srv, token, "POST",
		fmt.Sprintf("/admin/api/jobs/%d/cancel", int64(id)), nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}

	for {
		job, took, err := NewJobs(gate).Take(context.Background(), KindCase)
		if err != nil {
			t.Fatal(err)
		}
		if !took {
			break
		}
		if job.ID == int64(id) {
			t.Fatal("отменённое задание досталось исполнителю")
		}
	}
}
