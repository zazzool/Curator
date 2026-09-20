package gen

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"curator/server/internal/studio"
)

// числаУзлов — учёт проверочной установки.
func числаУзлов(m map[string]NodeUsage) NodeStats {
	return func(context.Context, time.Time, time.Time) (map[string]NodeUsage, error) {
		return m, nil
	}
}

func конвейерСтудии(t *testing.T, stats NodeStats) map[string]any {
	t.Helper()
	srv, token := newDeskWith(t, каталогМоделей, stats, studio.PermPrompts)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/api/pipeline", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("конвейер отдан кодом %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestPgКонвейерОтдаётсяВсемиУзламиПоПорядку(t *testing.T) {
	// Узлы перечисляет сервер, и студия их не повторяет: повторённый
	// список разошёлся бы молча — ровно на том узле, который добавили
	// последним. Экран показал бы конвейер короче настоящего, и выглядело
	// бы это не поломкой, а конвейером покороче; узел, которого не видно,
	// не настраивают.
	//
	// Порядок — тот, в каком задача проходит узлы, и он тоже сведение:
	// вычитка стоит между написанием и сверками не по алфавиту, а потому
	// что сверки обязаны мерить то, что уйдёт обучающемуся.
	body := конвейерСтудии(t, nil)
	nodes, _ := body["nodes"].([]any)
	if len(nodes) != len(Nodes) {
		t.Fatalf("узлов в ответе %d, а в конвейере %d", len(nodes), len(Nodes))
	}
	for i, want := range Nodes {
		row, _ := nodes[i].(map[string]any)
		if row["node"] != want {
			t.Fatalf("на месте %d узел %v, ждали %q", i, row["node"], want)
		}
		// Слово, а не только метка: «arbitrate» составителю ничего не
		// говорит, а решать по этой строке ему.
		word, _ := row["word"].(string)
		if word == "" || word == want {
			t.Fatalf("узел %q без русского имени: %v", want, row["word"])
		}
		// Задание узла названо: без него узел не работает вовсе, и это
		// то, что надо увидеть, а не вывести.
		if row["promptId"] == "" {
			t.Fatalf("у узла %q не нашлось задания: затравки не положены?", want)
		}
	}
	if days, _ := body["days"].(float64); days != pipelineDays {
		t.Fatalf("срок уехал как %v вместо %d", body["days"], pipelineDays)
	}
}

func TestPgУзелБезОбращенийПоказываетсяСНулями(t *testing.T) {
	// Свежая установка: не писали ещё ни одной задачи. Пропусти мы такие
	// узлы, экран настройки был бы пуст ровно тогда, когда настраивать и
	// приходится — до первой задачи.
	body := конвейерСтудии(t, числаУзлов(map[string]NodeUsage{
		NodeCompose: {Calls: 10, Failed: 1, MedianNanoUSD: 3_200_000, MedianMs: 4200},
	}))
	nodes, _ := body["nodes"].([]any)
	видели := map[string]map[string]any{}
	for _, one := range nodes {
		row, _ := one.(map[string]any)
		node, _ := row["node"].(string)
		видели[node] = row
	}
	if видели[NodeCompose]["calls"].(float64) != 10 {
		t.Fatalf("числа написания потеряны: %v", видели[NodeCompose])
	}
	if видели[NodeCompose]["medianNanoUsd"].(float64) != 3_200_000 {
		t.Fatalf("медианная цена потеряна: %v", видели[NodeCompose])
	}
	// Узел, о котором учёт молчит, всё равно в списке, и числа у него
	// нули — а не отсутствующие поля: пропавшее поле студия прочтёт как
	// undefined и покажет пустым местом, которое читается как «не
	// прочиталось».
	сверка := видели[NodeVerify]
	if сверка == nil {
		t.Fatal("узел без обращений выпал из конвейера")
	}
	for _, поле := range []string{"calls", "failed", "medianNanoUsd", "medianMs", "estimated"} {
		got, ok := сверка[поле]
		if !ok {
			t.Fatalf("у узла без обращений пропало поле %q: %v", поле, сверка)
		}
		if got.(float64) != 0 {
			t.Fatalf("у узла без обращений поле %q равно %v", поле, got)
		}
	}
}

func TestPgКонвейерРаботаетБезУчёта(t *testing.T) {
	// Учёта может не быть вовсе — поставщик не настроен, прайса нет.
	// Настройку узлов это не отменяет: их настраивают ДО того, как по ним
	// что-то прошло, и экран, отказавший без учёта, отнял бы и настройку.
	body := конвейерСтудии(t, nil)
	nodes, _ := body["nodes"].([]any)
	if len(nodes) != len(Nodes) {
		t.Fatalf("без учёта узлов в ответе %d", len(nodes))
	}
	first, _ := nodes[0].(map[string]any)
	if first["calls"].(float64) != 0 {
		t.Fatalf("без учёта взялись обращения: %v", first)
	}
}

func TestPgКонвейерЗакрытПравомЗаданий(t *testing.T) {
	// Раздел закрыт правом на сервере, а не спрятан в студии: конвейер
	// показывает, каким моделям уходят деньги.
	srv, token := newDeskWith(t, каталогМоделей, nil)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/api/pipeline", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("конвейер отдан без права «задания»")
	}
}

func TestPgКонвейерПоказываетУмолчаниеУзлаАНеПервоеПопавшееся(t *testing.T) {
	// Работает именно умолчание узла (ForNode берёт is_default), и
	// показать вместо него запасное задание значило бы показать
	// настройку, которой конвейер не пользуется: составитель поправил бы
	// модель у задания, мимо которого задачи и не ходят.
	ctx := context.Background()
	gate := testGate(t)
	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	// Запасное задание того же узла, не умолчание, и стоящее в выдаче
	// ВЫШЕ умолчания: All сортирует по узлу и id, а 'a…' идёт раньше
	// 'compose-default'.
	if _, err := gate.Exec(ctx,
		`INSERT INTO prompts (id, name, node, system_md, user_md, model, is_default)
		 VALUES ('a-запасное', 'Запасное', $1, 'x', 'y', 'запасная/модель', FALSE)
		 ON CONFLICT (id) DO NOTHING`, NodeCompose); err != nil {
		t.Fatal(err)
	}
	// Умолчанию — своя, узнаваемая модель.
	if _, err := gate.Exec(ctx,
		`UPDATE prompts SET model = 'рабочая/модель' WHERE node = $1 AND is_default`,
		NodeCompose); err != nil {
		t.Fatal(err)
	}

	body := конвейерСтудии(t, nil)
	nodes, _ := body["nodes"].([]any)
	for _, one := range nodes {
		row, _ := one.(map[string]any)
		if row["node"] != NodeCompose {
			continue
		}
		if row["model"] != "рабочая/модель" {
			t.Fatalf("конвейер показал не умолчание узла: %v", row)
		}
		return
	}
	t.Fatal("узел написания не нашёлся в конвейере")
}

func TestPgУзелБезЗаданияОстаётсяВКонвейере(t *testing.T) {
	// Перечень узлов берётся из кода (Nodes), а не из того, что нашлось в
	// базе. Разница видна ровно здесь: у узла нет умолчания — он не
	// работает вовсе, и это самая громкая новость про конвейер. Собери мы
	// список по базе, узел просто исчез бы с экрана, и пропажа задания
	// выглядела бы как пропажа узла — искать её пошли бы в коде.
	ctx := context.Background()
	gate := testGate(t)
	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	// Снимаем умолчание, а не удаляем задание: у задания есть история
	// правок, и удалять её ради проверки незачем.
	if _, err := gate.Exec(ctx,
		`UPDATE prompts SET is_default = FALSE WHERE node = $1`, NodeArbitrate); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := gate.Exec(context.Background(),
			`UPDATE prompts SET is_default = TRUE WHERE id = 'arbitrate-default'`); err != nil {
			t.Fatalf("умолчание разбора не вернулось: %v", err)
		}
	})

	body := конвейерСтудии(t, nil)
	nodes, _ := body["nodes"].([]any)
	if len(nodes) != len(Nodes) {
		t.Fatalf("узлов в ответе %d, а в конвейере %d: узел без задания выпал", len(nodes), len(Nodes))
	}
	for _, one := range nodes {
		row, _ := one.(map[string]any)
		if row["node"] != NodeArbitrate {
			continue
		}
		if row["promptId"] != "" {
			t.Fatalf("у узла без умолчания взялось задание %v", row["promptId"])
		}
		// Слово при этом на месте: строка обязана читаться, а не быть
		// пустой — пустая читается как «не прочиталось».
		if row["word"] == "" {
			t.Fatalf("узел без задания приехал безымянным: %v", row)
		}
		return
	}
	t.Fatalf("узел разбора выпал из конвейера: %v", body["nodes"])
}
