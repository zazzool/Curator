package gen

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"curator/server/internal/dbgate"
	"curator/server/internal/llmusage"
	"curator/server/internal/studio"
)

func TestМодельУзлаСтаршеУмолчанияНоМладшеЗаказа(t *testing.T) {
	// Старшинство: названная в заказе, потом модель узла, потом модель
	// поставщика. Заказ старше узла потому, что называют его руками и на
	// один раз: составитель, выбравший модель этой задаче, просит
	// сравнить — и настройка конвейера, молча переспорившая его выбор,
	// сделала бы сравнение невозможным, оставаясь на вид работающей.
	узел := Prompt{Node: NodeCompose, Model: "узловая"}
	if got := modelFor(узел, ""); got != "узловая" {
		t.Fatalf("без заказанной модели узел спросили %q", got)
	}
	if got := modelFor(узел, "заказанная"); got != "заказанная" {
		t.Fatalf("модель заказа переспорена узлом: %q", got)
	}
	// Пусто у обоих — пусто наружу: имя модели поставщика сюда не
	// переносится, оно устареет молча, когда поставщик сменит своё
	// умолчание.
	if got := modelFor(Prompt{Node: NodeVerify}, ""); got != "" {
		t.Fatalf("на пустом выборе взялась модель %q", got)
	}
	// Пробелы вокруг имени — обычная опечатка формы, и моделью они быть
	// не должны: имя с пробелом поставщик не узнает, а выглядеть это
	// будет как «модель не работает».
	if got := modelFor(Prompt{Model: "  узловая  "}, "   "); got != "узловая" {
		t.Fatalf("пробелы стали частью имени модели: %q", got)
	}
}

// модельУзла ставит узлу его модель, минуя сверку редакций: проверке нужен
// поставленный выбор, а не путь, которым его ставили.
func модельУзла(t *testing.T, gate *dbgate.Gate, node, model string) {
	t.Helper()
	if _, err := gate.Exec(context.Background(),
		`UPDATE prompts SET model = $2 WHERE node = $1 AND is_default`, node, model); err != nil {
		t.Fatalf("модель узла %q не поставлена: %v", node, err)
	}
}

func TestPgУзлыСпрашиваютсяСвоимиМоделямиИТакЖеЗаписываются(t *testing.T) {
	// Ради этого наряд и делался: написание условия — самая дорогая
	// работа конвейера, а слепая сверка отвечает одним словом из списка.
	// Одна модель на весь конвейер оставляла выбор между «дорого везде» и
	// «плохо везде».
	//
	// Вторая половина проверки — учёт, и она не придирка: расход по узлам
	// это то, по чему решают, где модель менять. Запишись туда не та,
	// менять стали бы не там, а сумма при этом сходилась бы.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	model := &подставнаяМодель{ответы: []string{
		годныйОтвет,
		ответВычитки,
		`{"answer":"3.1","why":"срок назван прямо","sure":true}`,
		ответСоседа,
		ответСоседа,
	}}
	runner, jobs, _ := конвейер(t, model, order)
	runner = runner.WithLedger(llmusage.NewStore(gate), nil)

	модельУзла(t, gate, NodeCompose, "дорогая/рассуждающая")
	модельУзла(t, gate, NodeVerify, "дешёвая/быстрая")

	result, err := прогнать(t, runner, jobs, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if model.модели[узелНаписания] != "дорогая/рассуждающая" {
		t.Fatalf("написание спрошено моделью %q", model.модели[узелНаписания])
	}
	if model.модели[узелСверки] != "дешёвая/быстрая" {
		t.Fatalf("слепая сверка спрошена моделью %q", model.модели[узелСверки])
	}
	// Вычитке модели не ставили — она идёт моделью поставщика, и это
	// пустая строка, а не имя: подставь мы имя, студия показала бы выбор
	// там, где его не делали.
	if model.модели[узелВычитки] != "" {
		t.Fatalf("узлу без своей модели подставлена %q", model.модели[узелВычитки])
	}

	// В учёте — та модель, которой спросили, узел за узлом.
	вУчёте := map[string]string{}
	rows, err := gate.Query(ctx,
		`SELECT node, model FROM llm_calls WHERE job_id = $1`, result.JobID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var node, m string
		if err := rows.Scan(&node, &m); err != nil {
			t.Fatal(err)
		}
		вУчёте[node] = m
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if вУчёте[NodeCompose] != "дорогая/рассуждающая" {
		t.Fatalf("в учёте у написания модель %q", вУчёте[NodeCompose])
	}
	if вУчёте[NodeVerify] != "дешёвая/быстрая" {
		t.Fatalf("в учёте у слепой сверки модель %q", вУчёте[NodeVerify])
	}
	if вУчёте[NodeProofread] != "" {
		t.Fatalf("в учёте у вычитки взялась модель %q", вУчёте[NodeProofread])
	}
}

func TestPgМодельЗаказаПереспариваетМодельУзла(t *testing.T) {
	// Составитель, выбравший модель этой задаче, сравнивает. Настройка
	// конвейера, молча переспорившая его выбор, сделала бы сравнение
	// невозможным — и выглядело бы это как «обе модели пишут одинаково».
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1", Model: "заказанная/на-пробу"}
	model := &подставнаяМодель{ответы: []string{
		годныйОтвет,
		ответВычитки,
		`{"answer":"3.1","why":"срок назван прямо","sure":true}`,
		ответСоседа,
		ответСоседа,
	}}
	runner, jobs, _ := конвейер(t, model, order)
	модельУзла(t, gate, NodeCompose, "узловая/обычная")

	if _, err := прогнать(t, runner, jobs, sourceID); err != nil {
		t.Fatal(err)
	}
	// Все узлы, а не только написание: заказ называет модель ЗАДАНИЮ.
	for i, m := range model.модели {
		if m != "заказанная/на-пробу" {
			t.Fatalf("узел %d спрошен моделью %q вместо заказанной", i, m)
		}
	}
}

func TestPgСнятаяМодельУзлаСохраняетсяПустой(t *testing.T) {
	// Ловушка COALESCE: имя задания сохраняется через
	// COALESCE(NULLIF($2,''), name) — пустое имя означает «не трогать».
	// Напиши мы так же модель, и снятую в студии модель было бы НЕЛЬЗЯ
	// снять: форма вернула бы пустую строку, база оставила бы прежнюю, а
	// выглядело бы это как несохранение.
	ctx := context.Background()
	gate := testGate(t)
	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	было, err := prompts.ForNode(ctx, NodeVerify, 0)
	if err != nil {
		t.Fatal(err)
	}

	сМоделью, err := prompts.Save(ctx, Prompt{
		ID: было.ID, Name: было.Name, SystemMd: было.SystemMd, UserMd: было.UserMd,
		Model: "дешёвая/быстрая", Revision: было.Revision,
	}, "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if сМоделью.Model != "дешёвая/быстрая" {
		t.Fatalf("модель узла не сохранилась: %q", сМоделью.Model)
	}

	снятая, err := prompts.Save(ctx, Prompt{
		ID: было.ID, Name: было.Name, SystemMd: было.SystemMd, UserMd: было.UserMd,
		Model: "", Revision: сМоделью.Revision,
	}, "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if снятая.Model != "" {
		t.Fatalf("снятая модель узла осталась %q: снять её нечем", снятая.Model)
	}
	// И перечитанное из базы тоже пусто: ответ ручки мог вернуть
	// записанное не туда.
	снова, err := prompts.ForNode(ctx, NodeVerify, 0)
	if err != nil {
		t.Fatal(err)
	}
	if снова.Model != "" {
		t.Fatalf("в базе у узла осталась модель %q", снова.Model)
	}
}

func TestPgСписокМоделейОтдаётсяСписком(t *testing.T) {
	// Пустой список — [], а не null: студия ходит по нему циклом, и на
	// свежей установке, где прайса ещё нет, null уронил бы экран заданий
	// целиком — то есть ровно тот экран, где модель и вписывают руками.
	srv, token := newDesk(t, studio.PermPrompts)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/api/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("список моделей отдан кодом %d", resp.StatusCode)
	}
	var body struct {
		Models *[]struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Models == nil {
		t.Fatal("список моделей отдан пустым значением, а не списком")
	}
	if len(*body.Models) != 2 {
		t.Fatalf("моделей в списке %d, ждали две", len(*body.Models))
	}
}

func TestPgСписокМоделейЗакрытПравомЗаданий(t *testing.T) {
	// Раздел мастерской закрыт правом на сервере, а не спрятан в студии:
	// скрытый раздел — подсказка, где искать, а не запрет.
	srv, token := newDesk(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/api/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("список моделей отдан без права «задания»")
	}
}

func TestPgМодельУзлаДоезжаетЧерезРучкуИОбратно(t *testing.T) {
	// Провод проверяется целиком, а не по частям: хранилище умеет
	// записать модель, а ручка могла её не принять или не вернуть — и
	// выглядело бы это как «студия не сохраняет», потому что сохранение
	// отвечало бы успехом.
	srv, token := newDesk(t, studio.PermPrompts)

	status, list, raw := call(t, srv, token, "GET", "/admin/api/prompts", nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	prompts, _ := list["prompts"].([]any)
	first, _ := prompts[0].(map[string]any)
	id, _ := first["id"].(string)
	revision, _ := first["revision"].(float64)
	// Свежая затравка модели не называет: узел идёт моделью поставщика, и
	// поле обязано приехать пустой строкой, а не пропасть из ответа —
	// пропавшее студия прочтёт как undefined и покажет как несделанный
	// выбор там, где выбор мог быть сделан.
	if got, ok := first["model"]; !ok || got != "" {
		t.Fatalf("у затравки модель пришла как %#v: %s", got, raw)
	}

	status, _, raw = call(t, srv, token, "PUT", "/admin/api/prompts/"+id,
		map[string]any{
			"name": "Правленое", "systemMd": "Задание {источник}.",
			"userMd": "Единица: {метка}.", "model": "дешёвая/быстрая",
			"revision": int(revision),
		})
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}

	status, list, raw = call(t, srv, token, "GET", "/admin/api/prompts", nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	prompts, _ = list["prompts"].([]any)
	var нашли string
	for _, one := range prompts {
		row, _ := one.(map[string]any)
		if row["id"] == id {
			нашли, _ = row["model"].(string)
		}
	}
	if нашли != "дешёвая/быстрая" {
		t.Fatalf("модель узла не вернулась из ручки: %q\n%s", нашли, raw)
	}
}

func TestPgПустойСписокМоделейЭтоСписокАНеПустоеЗначение(t *testing.T) {
	// Свежая установка: прайса ещё нет, поставщик ещё не настроен. Отдай
	// ручка null вместо списка — и экран заданий упал бы белым целиком,
	// то есть ровно тот экран, где модель и вписывают руками, когда
	// подсказок нет.
	//
	// Оба случая, а не один: «прайс пуст» и «поставщика нет вовсе» — это
	// разные ветки ручки, и пропущенная из них молчит одинаково.
	for имя, lister := range map[string]ModelLister{
		"прайс пуст":      func(context.Context) ([]ModelChoice, error) { return []ModelChoice{}, nil },
		"поставщика нет":  nil,
		"прайс отдал nil": func(context.Context) ([]ModelChoice, error) { return nil, nil },
	} {
		srv, token := newDeskModels(t, lister, studio.PermPrompts)
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/api/models", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Models *[]map[string]any `json:"models"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("%s: %v", имя, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: код %d", имя, resp.StatusCode)
		}
		if body.Models == nil {
			t.Fatalf("%s: список моделей отдан пустым значением, а не списком", имя)
		}
		if len(*body.Models) != 0 {
			t.Fatalf("%s: в пустом прайсе нашлось %d моделей", имя, len(*body.Models))
		}
	}
}
