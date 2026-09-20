package gen

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"curator/server/internal/dbgate"
	"curator/server/internal/llm"
)

// подставнаяМодель отвечает заготовленным и запоминает, что у неё
// спросили. Сети в проверках конвейера нет и быть не должно.
type подставнаяМодель struct {
	ответы   []string
	спрошено []llm.Prompt
	отказ    error
}

func (m *подставнаяМодель) Generate(_ context.Context, p llm.Prompt, _ string) (string, llm.Usage, error) {
	m.спрошено = append(m.спрошено, p)
	if m.отказ != nil {
		return "", llm.Usage{}, m.отказ
	}
	if len(m.ответы) == 0 {
		return "", llm.Usage{}, errors.New("подставной модели не подготовили ответ")
	}
	answer := m.ответы[0]
	m.ответы = m.ответы[1:]
	return answer, llm.Usage{PromptTokens: 10, CompletionTokens: 20}, nil
}

// конвейер собирает задание в очереди и исполнителя над ним.
func конвейер(t *testing.T, model *подставнаяМодель, order Order) (*Runner, *Jobs, *dbgate.Gate) {
	t.Helper()
	ctx := context.Background()
	gate := testGate(t)

	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	if _, err := jobs.Place(ctx, order, plan); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(jobs, prompts, model)
	// Порядок вариантов закрепляем: перемешивание проверяется отдельно, а
	// здесь оно сделало бы проверку то зелёной, то красной.
	runner.shuffle = func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	return runner, jobs, gate
}

// прогнать проводит по узлам задание нашего источника, пропуская чужие.
func прогнать(t *testing.T, runner *Runner, jobs *Jobs, sourceID int64) (Result, error) {
	t.Helper()
	ctx := context.Background()
	for {
		job, ok, err := jobs.Take(ctx, KindCase)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("задание не нашлось в очереди")
		}
		if job.SourceID != sourceID {
			if err := jobs.Done(ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			continue
		}
		// Своё задание уже взято — вернём его в очередь тем, что проведём
		// сами: RunNext берёт следующее, и подменять очередь ради проверки
		// незачем.
		result, err := runner.run(ctx, job)
		if err != nil {
			if failErr := jobs.Fail(ctx, job.ID, err.Error()); failErr != nil {
				t.Fatal(failErr)
			}
			return Result{}, err
		}
		if err := jobs.Done(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
		return result, nil
	}
}

const годныйОтвет = `{"title":"Срок рассмотрения",
	"segments":[{"text":"Заявление поступило 1 марта."},
	            {"text":"Ответ отправлен на десятый рабочий день.","statements":["абз. 1"]}],
	"options":[{"label":"3.1","text":"Сроки"},{"label":"3.2","text":"Отказ"},
	           {"label":"3.2","text":"Отказ по форме"}],
	"answer":"3.1","explanationMd":"Срок считается рабочими днями.","difficulty":3}`

func TestPgКонвейерПишетЧерновикИСверяетЕго(t *testing.T) {
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	model := &подставнаяМодель{ответы: []string{
		годныйОтвет,
		`{"answer":"3.1","why":"срок назван прямо","sure":true}`,
	}}
	runner, jobs, _ := конвейер(t, model, order)

	result, err := прогнать(t, runner, jobs, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Draft.Title != "Срок рассмотрения" || result.DraftID == 0 {
		t.Fatalf("черновик не сохранён: %+v", result)
	}
	if result.Verdict == nil || !result.Verdict.Agrees {
		t.Fatalf("сверка не сошлась с заказанным: %+v", result.Verdict)
	}
}

func TestPgСлепаяСверкаОстаётсяСлепой(t *testing.T) {
	// Сверке не говорится, какой вариант верен, не даются положения
	// источника и не называется заказанная единица. Подскажи ей хоть
	// чем-нибудь, и сверка выродится в самоподтверждение.
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	model := &подставнаяМодель{ответы: []string{
		годныйОтвет,
		`{"answer":"3.2","why":"не уверен","sure":false}`,
	}}
	runner, jobs, _ := конвейер(t, model, order)
	if _, err := прогнать(t, runner, jobs, sourceID); err != nil {
		t.Fatal(err)
	}

	if len(model.спрошено) != 2 {
		t.Fatalf("узлов прошло %d вместо двух", len(model.спрошено))
	}
	сверка := model.спрошено[1].System + "\n" + model.спрошено[1].User
	for _, подсказка := range []string{
		"десять рабочих дней", // положение источника
		"продлевается",        // второе положение
		"заказан",             // заказанная единица
	} {
		if strings.Contains(сверка, подсказка) {
			t.Fatalf("в слепую сверку уехала подсказка %q:\n%s", подсказка, сверка)
		}
	}
	if !strings.Contains(сверка, "Ответ отправлен на десятый рабочий день") {
		t.Fatal("в сверку не уехало само условие")
	}
	// Варианты сверка видит — их видит и обучающийся. Слепота в том, что
	// не сказано, какой из них верен.
	if !strings.Contains(сверка, "3.2") {
		t.Fatal("в сверку не уехали варианты")
	}
}

func TestPgНесогласиеСверкиВидноНоЗадачуНеРоняет(t *testing.T) {
	// Несогласие означает работу составителю, а не потерю написанного:
	// выбросить задачу дороже, чем показать её с пометкой.
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	model := &подставнаяМодель{ответы: []string{
		годныйОтвет,
		`{"answer":"3.2","why":"условие подходит и соседу","sure":true}`,
	}}
	runner, jobs, _ := конвейер(t, model, order)

	result, err := прогнать(t, runner, jobs, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict == nil || result.Verdict.Agrees {
		t.Fatalf("несогласие сверки потеряно: %+v", result.Verdict)
	}
	if result.DraftID == 0 {
		t.Fatal("черновик выброшен из-за несогласия сверки")
	}
}

func TestPgНесостоявшаясяСверкаНеПутаетсяСНесогласием(t *testing.T) {
	// Несогласие означает работу, а несостоявшаяся сверка — что задачу
	// никто не проверял. Смешать их значит выдать непроверенное за
	// проверенное.
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	model := &подставнаяМодель{ответы: []string{годныйОтвет, "не json вовсе"}}
	runner, jobs, _ := конвейер(t, model, order)

	result, err := прогнать(t, runner, jobs, sourceID)
	if err != nil {
		t.Fatalf("задание упало из-за несостоявшейся сверки: %v", err)
	}
	if result.Verdict != nil {
		t.Fatalf("несостоявшаяся сверка выдана за вердикт: %+v", result.Verdict)
	}
	if result.DraftID == 0 {
		t.Fatal("черновик потерян")
	}
}

func TestPgОтбитыйЧерновикРонитЗаданиеСПричиной(t *testing.T) {
	// Причина — единственный след того, почему задача не написалась.
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	model := &подставнаяМодель{ответы: []string{
		// Ссылка на положение, которого у единицы нет.
		strings.Replace(годныйОтвет, `"абз. 1"`, `"абз. 9"`, 1),
	}}
	runner, jobs, _ := конвейер(t, model, order)

	_, err := прогнать(t, runner, jobs, sourceID)
	if err == nil || !strings.Contains(err.Error(), "абз. 9") {
		t.Fatalf("выдумка модели прошла: %v", err)
	}
}

func TestPgЗаданиеУзлаБерётсяИзБазыИПравится(t *testing.T) {
	// Формулировка задания — врачебная работа, и правленое затравкой не
	// затирается: потерять правку значит потерять неделю настройки.
	ctx := context.Background()
	gate := testGate(t)
	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx,
		`UPDATE prompts SET system_md = system_md || E'\nПравка составителя.'
		  WHERE id = 'compose-default'`); err != nil {
		t.Fatal(err)
	}
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	prompt, err := prompts.ForNode(ctx, NodeCompose)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt.SystemMd, "Правка составителя") {
		t.Fatal("затравка затёрла правку составителя")
	}
}

func TestПеременныеЗаданияНазваныПоРусски(t *testing.T) {
	// Задание правит составитель, а не программист: {единица} он
	// прочтёт, а {{.UnitWord}} — нет.
	plan := планЗаказа(KindRecognise)
	got := Render("В источнике «{источник}» {единица} {метка} зовётся «{название}». {вложенность}.", plan)
	for _, want := range []string{"Приказ", "пункт", "3.1", "Сроки", "части вышестоящего"} {
		if !strings.Contains(got, want) {
			t.Fatalf("переменная не подставлена (%s): %s", want, got)
		}
	}
	// Незнакомая переменная остаётся как есть: молча вычищенная, она
	// превратила бы опечатку в задании в тихую потерю смысла.
	if got := Render("{опечатка}", plan); got != "{опечатка}" {
		t.Fatalf("незнакомая переменная вычищена молча: %q", got)
	}
}

func TestВердиктСчитаетСогласиеСамАНеВеритМодели(t *testing.T) {
	// Модель не знает, что заказывали, и знать не должна.
	plan := планЗаказа(KindRecognise)
	draft := годныйЧерновик()
	if !agrees(plan, draft, "3.1") {
		t.Fatal("согласие по метке не опознано")
	}
	// Сверка могла назвать вариант текстом: требовать от неё формы
	// ответа строже, чем от обучающегося, незачем.
	if !agrees(plan, draft, "Сроки") {
		t.Fatal("согласие по тексту варианта не опознано")
	}
	if agrees(plan, draft, "Отказ") {
		t.Fatal("несогласие принято за согласие")
	}
}

func TestРазборВердиктаСнимаетОграду(t *testing.T) {
	var verdict Verdict
	text := unfence("```json\n{\"answer\":\"3.1\",\"sure\":true}\n```")
	if err := json.Unmarshal([]byte(text), &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.Answer != "3.1" || !verdict.Sure {
		t.Fatalf("вердикт разобран не тот: %+v", verdict)
	}
}

func TestPgСлепотаНеЗависитОтТекстаЗадания(t *testing.T) {
	// Правка задания в студии не должна уметь сделать сверку зрячей:
	// вписанное {положения} превратило бы её в самоподтверждение,
	// выглядящее работой. Поэтому задание сверки собирается по
	// ослеплённому плану, и подставлять в него просто нечего.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}

	model := &подставнаяМодель{ответы: []string{
		годныйОтвет,
		`{"answer":"3.1","why":"срок назван","sure":true}`,
	}}
	runner, jobs, _ := конвейер(t, model, order)

	// Составитель вписал в задание сверки всё, что мог.
	if _, err := gate.Exec(ctx,
		`UPDATE prompts
		    SET user_md = user_md || E'\n\nЗаказан {метка} — {название}.\nПоложения:\n{положения}'
		  WHERE id = 'verify-default'`); err != nil {
		t.Fatal(err)
	}

	if _, err := прогнать(t, runner, jobs, sourceID); err != nil {
		t.Fatal(err)
	}
	сверка := model.спрошено[1].System + "\n" + model.спрошено[1].User
	for _, подсказка := range []string{"десять рабочих дней", "продлевается"} {
		if strings.Contains(сверка, подсказка) {
			t.Fatalf("правка задания сделала сверку зрячей (%q):\n%s", подсказка, сверка)
		}
	}
	// Названия единиц сверка видит — они и есть варианты ответа, их видит
	// и обучающийся. Зрячей её сделало бы указание, КАКАЯ из них
	// заказана, и вписанные составителем поля остались пустыми.
	if strings.Contains(сверка, "Заказан 3.1") {
		t.Fatalf("сверке сказали, что заказано:\n%s", сверка)
	}
}
