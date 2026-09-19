package analytics

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"sort"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/telemetry"
)

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

func врач(t *testing.T, gate *dbgate.Gate) int64 {
	t.Helper()
	var id int64
	if err := gate.QueryRow(context.Background(),
		`INSERT INTO accounts DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("учётная запись не заведена: %v", err)
	}
	return id
}

// попытка — одна строка в attempts, как её видит и расчёт, и проверка.
type попытка struct {
	correct bool
	answer  string
	spentMs int64
}

// положить кладёт попытки по новой задаче и отдаёт её номер.
func положить(t *testing.T, gate *dbgate.Gate, list []попытка) string {
	t.Helper()
	ctx := context.Background()
	caseID := fmt.Sprintf("c-svodka-%d-%d", time.Now().UnixNano(), rand.IntN(100000))
	id := врач(t, gate)
	now := time.Now()
	for i, one := range list {
		if _, err := gate.Exec(ctx, `
			INSERT INTO attempts (account_id, case_id, correct, answer, spent_ms, happened_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			id, caseID, one.correct, one.answer, one.spentMs,
			now.Add(-time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("попытка не легла: %v", err)
		}
	}
	return caseID
}

// посчитать — независимый расчёт той же сводки на Go.
//
// # Зачем вторая реализация
//
// Сводка считается одним запросом, и запрос этот не из простых: медиана,
// путаница вариантов и доля решивших в одном проходе. Ошибка в нём не
// падает, а возвращает правдоподобное число, и заметить её нельзя ничем,
// кроме сверки с расчётом, написанным другим способом. Поэтому здесь не
// ожидаемые числа руками, а второй счёт: руками написанное ожидание
// проверяет только тот случай, который придумал писавший.
func посчитать(list []попытка) Stats {
	out := Stats{Confusion: map[string]int64{}, Attempts: int64(len(list))}
	var times []int64
	for _, one := range list {
		if one.correct {
			out.Correct++
		} else if one.answer != "" {
			out.Confusion[one.answer]++
		}
		// Ноль значит «сборка времени не прислала», и складывать его с
		// настоящими секундами значит объявить, что задача решается
		// мгновенно.
		if one.spentMs > 0 {
			times = append(times, one.spentMs)
		}
	}
	if out.Attempts > 0 {
		out.SolveRate = float64(float32(float64(out.Correct) / float64(out.Attempts)))
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	switch n := len(times); {
	case n == 0:
		out.MedianMs = 0
	case n%2 == 1:
		out.MedianMs = times[n/2]
	default:
		// Ровно то, что делает percentile_cont(0.5): середина между двумя
		// средними, а не нижняя из них.
		out.MedianMs = int64(math.Round(float64(times[n/2-1]+times[n/2]) / 2))
	}
	out.Origin = "live"
	return out
}

func сверить(t *testing.T, got, want Stats) {
	t.Helper()
	if got.Attempts != want.Attempts {
		t.Errorf("попыток %d, посчитано %d", got.Attempts, want.Attempts)
	}
	if got.Correct != want.Correct {
		t.Errorf("верных %d, посчитано %d", got.Correct, want.Correct)
	}
	if got.SolveRate != want.SolveRate {
		t.Errorf("решаемость %v, посчитана %v", got.SolveRate, want.SolveRate)
	}
	if got.MedianMs != want.MedianMs {
		t.Errorf("медиана %d, посчитана %d", got.MedianMs, want.MedianMs)
	}
	if got.Origin != want.Origin {
		t.Errorf("происхождение %q, ожидалось %q", got.Origin, want.Origin)
	}
	if len(got.Confusion) != len(want.Confusion) {
		t.Errorf("в путанице %d вариантов, посчитано %d: %v против %v",
			len(got.Confusion), len(want.Confusion), got.Confusion, want.Confusion)
		return
	}
	for answer, n := range want.Confusion {
		if got.Confusion[answer] != n {
			t.Errorf("вариант %q выбран %d раз, посчитано %d",
				answer, got.Confusion[answer], n)
		}
	}
}

func TestPgСводкаСходитсяСНезависимымРасчётом(t *testing.T) {
	gate := testGate(t)
	rollup := NewRollup(gate)
	ctx := context.Background()

	// Попытки берутся случайными, а не подобранными: подобранные проверяют
	// ровно тот случай, который придумал писавший, а расходятся две
	// реализации на том, который он не придумал.
	list := make([]попытка, 0, 42)
	for i := 0; i < 40; i++ {
		list = append(list, попытка{
			correct: rand.IntN(3) > 0,
			answer:  fmt.Sprintf("вариант-%d", rand.IntN(4)),
			spentMs: int64(1 + rand.IntN(60000)),
		})
	}
	// Попыток со временем ровно чётное число, и это не случайность:
	// медиана чётного ряда — середина между двумя средними, а не нижняя
	// из них, и только на чётном ряду видно, что два расчёта берут её
	// одинаково. Нечётный ряд эту разницу скрывает целиком.
	list = append(list,
		попытка{correct: true, answer: "вариант-0"},
		попытка{correct: false, answer: "вариант-1"})
	caseID := положить(t, gate, list)

	if _, err := rollup.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, found, err := rollup.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("сводки по задаче нет вовсе")
	}
	сверить(t, got, посчитать(list))
}

// Медиана, попавшая ровно на половину, округляется от нуля — и здесь,
// и в Go.
//
// Проверка нарочно не случайная, в отличие от сверки выше: случайный ряд
// попадает на ровную половину изредка, и падение выглядело бы мельканием.
// Ряд из двух чисел 2 и 3 даёт 2.5, а это ровно тот случай, на котором
// round() у double precision в Postgres уходит к ЧЁТНОМУ (2), а
// math.Round в Go — от нуля (3). Расхождение поймала сверка с
// независимым расчётом, и стоило оно одной миллисекунды — но расходились
// две реализации одного правила.
func TestPgМедианаНаРовнойПоловинеОкругляетсяОтНуля(t *testing.T) {
	gate := testGate(t)
	rollup := NewRollup(gate)
	ctx := context.Background()

	caseID := положить(t, gate, []попытка{
		{correct: true, spentMs: 2},
		{correct: false, answer: "б", spentMs: 3},
	})
	if _, err := rollup.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, found, err := rollup.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("сводки по задаче нет вовсе")
	}
	if got.MedianMs != 3 {
		t.Errorf("медиана %d, а 2.5 округляется от нуля до 3", got.MedianMs)
	}
}

func TestPgПересчётИдемпотентен(t *testing.T) {
	// Нахлёст при отборе задач намеренно заставляет пересчитывать уже
	// посчитанное. Стоить это должно работы, и ничего больше: сложись
	// пересчёт с прежним числом — и решаемость удваивалась бы каждые пять
	// минут.
	gate := testGate(t)
	rollup := NewRollup(gate)
	ctx := context.Background()

	caseID := положить(t, gate, []попытка{
		{correct: true, spentMs: 1000},
		{correct: false, answer: "б", spentMs: 3000},
		{correct: true, spentMs: 2000},
	})

	if _, err := rollup.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	first, _, err := rollup.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rollup.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	second, _, err := rollup.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	сверить(t, second, first)
}

func TestPgМедианаНеСчитаетПопыткиБезВремени(t *testing.T) {
	// Ноль в spent_ms значит «сборка времени не прислала». Сложи мы его с
	// настоящими секундами — и вышло бы, что задача решается мгновенно, а
	// по медиане отбирают задачи в наборы.
	gate := testGate(t)
	rollup := NewRollup(gate)
	ctx := context.Background()

	caseID := положить(t, gate, []попытка{
		{correct: true, spentMs: 0},
		{correct: true, spentMs: 0},
		{correct: true, spentMs: 10000},
		{correct: true, spentMs: 20000},
	})
	if _, err := rollup.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _, err := rollup.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MedianMs != 15000 {
		t.Errorf("медиана %d, а по двум попыткам со временем она 15000", got.MedianMs)
	}
	if got.Attempts != 4 {
		t.Errorf("попыток в сводке %d: попытки без времени считаются, они были", got.Attempts)
	}
}

func TestPgВвезённаяСводкаУступаетМестоСвоей(t *testing.T) {
	// Ввезённое число мерило другую аудиторию, и среднее двух аудиторий не
	// описывает ни одной. Цена названа вслух: ввезённое теряется на первой
	// же нашей попытке — оно и заводилось затем, чтобы система не была
	// слепой, пока своих попыток нет.
	gate := testGate(t)
	rollup := NewRollup(gate)
	ctx := context.Background()

	caseID := положить(t, gate, []попытка{{correct: true, spentMs: 1000}})
	if _, err := gate.Exec(ctx, `
		INSERT INTO case_stats (case_id, attempts, correct, solve_rate, origin, updated_at)
		VALUES ($1, 5000, 2100, 0.42, 'imported', NOW() - interval '1 day')
		ON CONFLICT (case_id) DO UPDATE
		   SET attempts = 5000, correct = 2100, solve_rate = 0.42,
		       origin = 'imported', updated_at = NOW() - interval '1 day'`,
		caseID); err != nil {
		t.Fatal(err)
	}

	if _, err := rollup.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _, err := rollup.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != "live" {
		t.Errorf("происхождение осталось %q: ввезённое не уступило место своему", got.Origin)
	}
	if got.Attempts != 1 || got.Correct != 1 {
		t.Errorf("сводка %d/%d: ввезённое смешалось со своим", got.Correct, got.Attempts)
	}
}

func TestPgОтборНеВеритСлабоРешавшимсяЗадачам(t *testing.T) {
	// Задача, решённая один раз из одного, имеет решаемость 1.0 и встала
	// бы первой в списке «слишком лёгких». Список, который возглавляют
	// задачи, никем толком не решавшиеся, читать бесполезно.
	gate := testGate(t)
	rollup := NewRollup(gate)
	ctx := context.Background()

	редкая := положить(t, gate, []попытка{{correct: true, spentMs: 1000}})
	if _, err := rollup.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	out, err := rollup.Rank(ctx, 0.95, 0.25, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, one := range out.Easy {
		if one.CaseID == редкая {
			t.Errorf("задача с одной попыткой попала в «слишком лёгкие»")
		}
		if one.Attempts < Floor {
			t.Errorf("в отборе задача с %d попытками при пороге %d", one.Attempts, Floor)
		}
	}
}

func TestPgОтборНаходитЛёгкуюИНеразрешимую(t *testing.T) {
	gate := testGate(t)
	rollup := NewRollup(gate)
	ctx := context.Background()

	лёгкая := make([]попытка, 0, Floor)
	трудная := make([]попытка, 0, Floor)
	for i := 0; i < Floor+5; i++ {
		лёгкая = append(лёгкая, попытка{correct: true, spentMs: 1000})
		трудная = append(трудная, попытка{correct: false, answer: "б", spentMs: 1000})
	}
	первая := положить(t, gate, лёгкая)
	вторая := положить(t, gate, трудная)

	if _, err := rollup.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	out, err := rollup.Rank(ctx, 0.95, 0.25, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !содержит(out.Easy, первая) {
		t.Error("задача, решаемая всеми, не попала в «слишком лёгкие»")
	}
	if !содержит(out.Hard, вторая) {
		t.Error("задача, не решаемая никем, не попала в «неразрешимые»")
	}
	if содержит(out.Easy, вторая) || содержит(out.Hard, первая) {
		t.Error("задачи попали не в свои списки")
	}
}

func содержит(list []Stats, caseID string) bool {
	for _, one := range list {
		if one.CaseID == caseID {
			return true
		}
	}
	return false
}

func TestPgВоронкаПоказываетВесьСловарьВключаяНули(t *testing.T) {
	// Отчёт, показывающий только случившееся, не отличает «никто не
	// покупал» от «приложение не шлёт это событие», а это разные беды с
	// разным лечением.
	gate := testGate(t)
	rollup := NewRollup(gate)
	ctx := context.Background()

	id := врач(t, gate)
	now := time.Now()
	if _, err := telemetry.NewStore(gate).Record(ctx, id, []telemetry.Incoming{
		{Name: "app_opened", HappenedAt: now},
	}, now); err != nil {
		t.Fatal(err)
	}

	list, err := rollup.Funnel(ctx, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(telemetry.Catalog()) {
		t.Fatalf("в воронке %d строк, в словаре %d событий", len(list), len(telemetry.Catalog()))
	}
	for i, one := range list {
		if one.Name != telemetry.Catalog()[i].Name {
			t.Fatalf("строка %d: %q, а в словаре на этом месте %q",
				i, one.Name, telemetry.Catalog()[i].Name)
		}
	}
	if list[0].Name != "app_opened" || list[0].Count < 1 {
		t.Errorf("случившееся событие не сосчитано: %+v", list[0])
	}
}
