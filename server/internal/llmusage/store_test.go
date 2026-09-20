package llmusage

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/llm"
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
	gate, err := dbgate.Open(context.Background(), dsn, dbgate.Options{})
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

// newModel даёт имя модели, своё у каждой проверки: база одна на весь
// прогон, и две проверки, взявшие одно имя, ловили бы друг друга за руку
// через раз.
func newModel(t *testing.T) string {
	t.Helper()
	return "проверка/" + time.Now().Format("150405.000000000")
}

func TestPgЗаписываетсяКаждоеОбращение(t *testing.T) {
	ctx := context.Background()
	s := NewStore(testGate(t))
	model := newModel(t)
	prices := llm.Prices{model: {Prompt: 1000, Completion: 2000}}

	// Удачное и неудачное — оба: сбой объясняется соседями по заданию.
	if err := s.Record(ctx, Call{Provider: "шлюз", Model: model,
		Usage: llm.Usage{PromptTokens: 100, CompletionTokens: 50}}, prices); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, Call{Provider: "шлюз", Model: model,
		Err: llm.ErrTruncated, Usage: llm.Usage{PromptTokens: 10}}, prices); err != nil {
		t.Fatal(err)
	}

	lines, err := s.Summary(ctx, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var mine *Line
	for i := range lines {
		if lines[i].Model == model {
			mine = &lines[i]
		}
	}
	if mine == nil {
		t.Fatalf("обращения не попали в сводку: %v", lines)
	}
	if mine.Calls != 2 || mine.Failed != 1 {
		t.Fatalf("в сводке %d обращений и %d отказов вместо двух и одного", mine.Calls, mine.Failed)
	}
	// 100 обычных входных по 1000 и 50 выходных по 2000, плюс 10 входных
	// у оборвавшегося: оплачено и то и другое.
	if want := int64(100*1000 + 50*2000 + 10*1000); mine.CostNanoUSD != want {
		t.Fatalf("посчитано %d нанодолларов вместо %d", mine.CostNanoUSD, want)
	}
	// Ни одну цену поставщик не называл — значит вся сумма оценочная.
	if mine.ExactNanoUSD != 0 || mine.EstimatedCalls != 2 {
		t.Fatalf("оценка выдана за факт: точных %d, оценочных обращений %d",
			mine.ExactNanoUSD, mine.EstimatedCalls)
	}
}

func TestPgНазваннаяЦенаСчитаетсяОтдельноОтОценки(t *testing.T) {
	// Сводка, в которой оценка неотличима от факта, — это сводка, которой
	// нельзя пользоваться: расчёт по прайсу не знает ни скидки кэша, ни
	// наценки шлюза и ошибается в разы там, где кэш работает.
	ctx := context.Background()
	s := NewStore(testGate(t))
	model := newModel(t)

	if err := s.Record(ctx, Call{Provider: "маршрутизатор", Model: model,
		Usage: llm.Usage{PromptTokens: 10, CostUSD: 0.25, CostExact: true}}, llm.Prices{}); err != nil {
		t.Fatal(err)
	}
	lines, err := s.Summary(ctx, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if l.Model != model {
			continue
		}
		if l.ExactNanoUSD != llm.Nano/4 || l.EstimatedCalls != 0 {
			t.Fatalf("названная цена посчитана как оценка: %+v", l)
		}
		return
	}
	t.Fatal("обращение не попало в сводку")
}

func TestPgТелоОбрезаетсяПоГраницеРун(t *testing.T) {
	// Обрезанная посередине кириллическая буква делает строку
	// недействительным UTF-8, и такую строку база не примет вовсе: запись
	// упала бы из-за длины тела, а не из-за работы.
	ctx := context.Background()
	s := NewStore(testGate(t))
	long := strings.Repeat("я", TextMax) // двухбайтовая буква: обрежется посередине
	err := s.Record(ctx, Call{Provider: "шлюз", Model: newModel(t), Request: long}, llm.Prices{})
	if err != nil {
		t.Fatalf("длинное тело не записалось: %v", err)
	}
}

func TestPgСтарыеТелаУбираютсяАЧислаОстаются(t *testing.T) {
	// Тела стареют быстро: журнал нужен, пока сбой разбирают, а не вечно.
	// Числа учёта остаются навсегда — сводка расходов за всё время должна
	// оставаться правдой.
	ctx := context.Background()
	gate := testGate(t)
	s := NewStore(gate)
	model := newModel(t)
	if err := s.Record(ctx, Call{Provider: "шлюз", Model: model,
		Usage: llm.Usage{PromptTokens: 5}, Request: "спросили", Answer: "ответили"},
		llm.Prices{model: {Prompt: 100}}); err != nil {
		t.Fatal(err)
	}
	// Подвинем запись в прошлое: ждать две недели проверка не может.
	if _, err := gate.Exec(ctx,
		`UPDATE llm_calls SET created_at = NOW() - INTERVAL '30 days' WHERE model = $1`,
		model); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Sweep(ctx, 14*24*time.Hour, time.Now()); err != nil {
		t.Fatal(err)
	}

	var request *string
	var cost int64
	err := gate.QueryRow(ctx,
		`SELECT request, cost_nano_usd FROM llm_calls WHERE model = $1`, model).
		Scan(&request, &cost)
	if err != nil {
		t.Fatal(err)
	}
	if request != nil {
		t.Fatal("тело осталось после уборки")
	}
	if cost != 500 {
		t.Fatalf("уборка тел унесла и числа: цена стала %d", cost)
	}
}

func TestPgСрокХраненияОбязанБытьНазван(t *testing.T) {
	// Ноль означал бы «убрать всё»: уборка, стирающая тела сегодняшних
	// обращений, — это отказ разбирать сбой, случившийся минуту назад.
	_, err := NewStore(testGate(t)).Sweep(context.Background(), 0, time.Now())
	if err == nil {
		t.Fatal("уборка с неназванным сроком прошла")
	}
}

func TestPgПрайсКладётсяИЧитается(t *testing.T) {
	ctx := context.Background()
	s := NewStore(testGate(t))
	model := strings.ToLower(newModel(t))

	if err := s.SavePrices(ctx, "маршрутизатор",
		llm.Prices{model: {Prompt: 3000, Completion: 15000, CacheRead: 300}}); err != nil {
		t.Fatal(err)
	}
	// Повтор заменяет, а не добавляет: две строки о цене одной модели —
	// два ответа на один вопрос, из которых покажут случайный.
	if err := s.SavePrices(ctx, "маршрутизатор",
		llm.Prices{model: {Prompt: 4000, Completion: 15000}}); err != nil {
		t.Fatal(err)
	}

	prices, err := s.Prices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := prices[model]; got.Prompt != 4000 {
		t.Fatalf("цена не заменилась: %+v", got)
	}
}

func TestPgПрайсБезПоставщикаНеКладётся(t *testing.T) {
	// Прайс без имени поставщика — это цены неизвестно чьи: сложить их с
	// чужими значит считать расход по чужому прайсу.
	err := NewStore(testGate(t)).SavePrices(context.Background(), "", llm.Prices{"м": {Prompt: 1}})
	if err == nil {
		t.Fatal("прайс без поставщика лёг")
	}
}
