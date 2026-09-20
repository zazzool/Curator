package gen

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"curator/server/internal/dbgate"
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

// источник заводит приказ с тремя пунктами: головным, соседом и группой.
//
// Своим кратким именем у каждой проверки: база одна на весь прогон, и две
// проверки, взявшие одно имя, ловили бы друг друга за руку через раз.
func источник(t *testing.T, gate *dbgate.Gate) int64 {
	t.Helper()
	ctx := context.Background()
	slug := fmt.Sprintf("приказ-%d-%d", time.Now().UnixNano(), rand.Intn(1000))

	var id int64
	err := gate.QueryRow(ctx,
		`INSERT INTO sources (slug, kind, title, unit_word, statement_word,
		                      purpose, hierarchy, completeness)
		 VALUES ($1, 'decree', 'Приказ для проверки', 'пункт', 'указание',
		         'legal', 'part-of', 'fragment')
		 RETURNING id`, slug).Scan(&id)
	if err != nil {
		t.Fatalf("источник не заведён: %v", err)
	}

	units := []struct {
		label, parent, title string
		answerable           bool
		ord                  int
	}{
		{"3", "", "Порядок", false, 0}, // группа: к ответу не пригодна
		{"3.1", "3", "Сроки", true, 1}, // эталон
		{"3.2", "3", "Отказ", true, 2}, // сосед
		{"4", "", "Прочее", true, 3},   // не сосед: другой родитель
	}
	for _, u := range units {
		path := u.label
		if u.parent != "" {
			path = u.parent + "/" + u.label
		}
		_, err := gate.Exec(ctx,
			`INSERT INTO source_units (source_id, kind, label, parent_label, title,
			                           path, depth, answerable, ord)
			 VALUES ($1, 'entry', $2, $3, $4, $5, $6, $7, $8)`,
			id, u.label, u.parent, u.title, path, strings.Count(path, "/"), u.answerable, u.ord)
		if err != nil {
			t.Fatalf("единица %q не заведена: %v", u.label, err)
		}
	}

	statements := []struct{ unit, designation, body, place string }{
		{"3.1", "абз. 1", "Срок рассмотрения — десять рабочих дней.", "с. 4"},
		{"3.1", "абз. 2", "Срок продлевается однократно.", "с. 4"},
		{"3.2", "абз. 1", "Отказ оформляется письменно.", "с. 5"},
	}
	for i, st := range statements {
		_, err := gate.Exec(ctx,
			`INSERT INTO source_unit_statements
			     (source_id, unit_label, kind, designation, body_md, place_ref, ord)
			 VALUES ($1, $2, '', $3, $4, $5, $6)`,
			id, st.unit, st.designation, st.body, st.place, i)
		if err != nil {
			t.Fatalf("положение не заведено: %v", err)
		}
	}
	return id
}

func TestPgПланСобираетсяИзДанныхИсточника(t *testing.T) {
	// Словарь, положения и круг различения берутся из источника, а не из
	// встроенного знания: источник любой, и условия «если это МКБ» здесь
	// быть не должно.
	gate := testGate(t)
	sourceID := источник(t, gate)

	plan, err := NewResolver(gate).Resolve(context.Background(),
		Order{SourceID: sourceID, UnitLabel: "3.1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.UnitWord != "пункт" || plan.StatementWord != "указание" {
		t.Fatalf("словарь источника потерян: %+v", plan)
	}
	if plan.Hierarchy != "part-of" {
		t.Fatalf("смысл вложенности потерян: %q", plan.Hierarchy)
	}
	if !strings.Contains(plan.StatementsMd, "десять рабочих дней") ||
		!strings.Contains(plan.StatementsMd, "абз. 2") {
		t.Fatalf("положения собраны не все: %q", plan.StatementsMd)
	}
	if plan.TaskKind != KindRecognise {
		t.Fatalf("вид задачи по умолчанию — %q", plan.TaskKind)
	}
}

func TestPgСоседБерётсяПоРодителю_АНеПоФормеМетки(t *testing.T) {
	// Соседство — это данные источника. Выводить его из формы метки
	// («3.2 похоже на 3.1») значит угадывать устройство чужого
	// справочника.
	gate := testGate(t)
	sourceID := источник(t, gate)

	plan, err := NewResolver(gate).Resolve(context.Background(),
		Order{SourceID: sourceID, UnitLabel: "3.1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Siblings) != 1 || plan.Siblings[0].Label != "3.2" {
		t.Fatalf("круг различения собран не тот: %+v", plan.Siblings)
	}
	// У кандидата неверного варианта едут его положения: условие готовой
	// задачи прогоняется и против них, чтобы поймать двойника — соседа,
	// которого условие подтверждает не хуже эталона.
	if !strings.Contains(plan.Siblings[0].StatementsMd, "письменно") {
		t.Fatalf("у соседа нет положений: %+v", plan.Siblings[0])
	}
}

func TestPgПараПутаютСДобавляетсяВКругРазличения(t *testing.T) {
	// Это единственное место, где сходство двух единиц названо прямо.
	// Пренебречь им значит подбирать неверные варианты хуже, чем мог бы
	// сам источник.
	gate := testGate(t)
	sourceID := источник(t, gate)
	if _, err := gate.Exec(context.Background(),
		`INSERT INTO source_unit_differentials (source_id, unit_label, counterpart, features_md)
		 VALUES ($1, '3.1', '4', 'путают по сроку')`, sourceID); err != nil {
		t.Fatal(err)
	}

	plan, err := NewResolver(gate).Resolve(context.Background(),
		Order{SourceID: sourceID, UnitLabel: "3.1"})
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, s := range plan.Siblings {
		labels = append(labels, s.Label)
	}
	if len(labels) != 2 {
		t.Fatalf("круг различения без названной пары: %v", labels)
	}
}

func TestPgЗаказПоГруппеОтказывает(t *testing.T) {
	// Группа — тоже единица, только к ответу не пригодная: задача по ней
	// спрашивала бы то, на что ответа нет.
	gate := testGate(t)
	sourceID := источник(t, gate)
	_, err := NewResolver(gate).Resolve(context.Background(),
		Order{SourceID: sourceID, UnitLabel: "3"})
	if err == nil {
		t.Fatal("заказ по группе прошёл")
	}
	if !strings.Contains(err.Error(), "пункт") {
		// Отказ читает составитель, и звать единицу надо словом источника.
		t.Fatalf("отказ написан не словарём источника: %v", err)
	}
}

func TestPgКарантинЗапрещаетЗаказ(t *testing.T) {
	// Карантин — редакционное решение «по этой единице писать нельзя».
	// Обойти его молча значит выпустить задачу, которую уже признали
	// негодной.
	gate := testGate(t)
	sourceID := источник(t, gate)
	if _, err := gate.Exec(context.Background(),
		`INSERT INTO source_unit_quarantine (source_id, unit_label, reason)
		 VALUES ($1, '3.1', 'текст пункта разобран неверно')`, sourceID); err != nil {
		t.Fatal(err)
	}
	_, err := NewResolver(gate).Resolve(context.Background(),
		Order{SourceID: sourceID, UnitLabel: "3.1"})
	if err == nil || !strings.Contains(err.Error(), "разобран неверно") {
		t.Fatalf("карантин обойдён: %v", err)
	}
}

func TestPgЕдиницаБезПоложенийНеЗаказывается(t *testing.T) {
	// Писать не по чему: задание без указаний вернуло бы задачу,
	// написанную моделью по памяти, а не по источнику.
	gate := testGate(t)
	sourceID := источник(t, gate)
	_, err := NewResolver(gate).Resolve(context.Background(),
		Order{SourceID: sourceID, UnitLabel: "4"})
	if err == nil || !strings.Contains(err.Error(), "положения") {
		t.Fatalf("единица без положений заказана: %v", err)
	}
}

func TestPgВидЗадачиИзСловаряИлиОтказ(t *testing.T) {
	// Непонятое не применяется: молча понятый как узнавание заказ
	// действия написал бы задачу не про то.
	gate := testGate(t)
	sourceID := источник(t, gate)
	r := NewResolver(gate)

	if _, err := r.Resolve(context.Background(),
		Order{SourceID: sourceID, UnitLabel: "3.1", Kind: "что-нибудь"}); err == nil {
		t.Fatal("вид задачи не из словаря принят")
	}

	plan, err := r.Resolve(context.Background(),
		Order{SourceID: sourceID, UnitLabel: "3.1", Kind: KindAction})
	if err != nil {
		t.Fatal(err)
	}
	// Эталоном задачи-действия идёт положение, и выбирает его заказ, а не
	// модель: отдай выбор модели, и задача ответит не на тот вопрос.
	if plan.Target == nil || plan.Target.Designation != "абз. 1" {
		t.Fatalf("эталонное положение выбрано не заказом: %+v", plan.Target)
	}

	plan, err = r.Resolve(context.Background(),
		Order{SourceID: sourceID, UnitLabel: "3.1", Kind: KindAction, TargetStatement: "абз. 2"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target.Designation != "абз. 2" {
		t.Fatalf("заказанное положение подменено: %+v", plan.Target)
	}

	if _, err := r.Resolve(context.Background(), Order{SourceID: sourceID, UnitLabel: "3.1",
		Kind: KindAction, TargetStatement: "абз. 9"}); err == nil {
		// Отказ, а не «возьмём первое»: заказали одно положение, а задача
		// вышла бы про другое.
		t.Fatal("заказ несуществующего положения прошёл")
	}
}

func TestPgПовторЗаказаНеЗаводитВторогоЗадания(t *testing.T) {
	// Составитель, нажавший «заказать» дважды, хотел одну задачу, а не
	// две, и платить за вторую незачем.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}

	jobs := NewJobs(gate)
	first, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("один заказ лёг двумя заданиями: %d и %d", first.ID, second.ID)
	}
	if second.Plan.UnitWord != "пункт" {
		t.Fatalf("план не доехал обратно: %+v", second.Plan)
	}
}

func TestPgЗаданиеБерётсяВРаботуОдинРаз(t *testing.T) {
	// Два исполнителя, читающие очередь одновременно, иначе взяли бы одно
	// и то же задание и написали бы две задачи по одному заказу — за
	// деньги.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	placed, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}

	seen := map[int64]int{}
	for {
		job, ok, err := jobs.Take(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		seen[job.ID]++
		// Чужие задания, оставшиеся от соседних проверок, закрываем, чтобы
		// очередь не крутилась вечно.
		if err := jobs.Done(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
	}
	if seen[placed.ID] != 1 {
		t.Fatalf("задание взято в работу %d раз", seen[placed.ID])
	}
}

func TestPgПустаяОчередьЭтоНеОтказ(t *testing.T) {
	// Исполнитель, принявший пустую очередь за отказ, начал бы её чинить.
	ctx := context.Background()
	jobs := NewJobs(testGate(t))
	for {
		job, ok, err := jobs.Take(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return
		}
		if err := jobs.Done(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPgЗакрытоеЗаданиеНеСнимается(t *testing.T) {
	// Снятое доделанное означало бы, что задача есть, а задание говорит,
	// что её нет.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	job, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Done(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Cancel(ctx, job.ID); err == nil {
		t.Fatal("закрытое задание снято")
	}
}

func TestPgОтказЗаданияВсегдаНазываетПричину(t *testing.T) {
	// Причина — единственный след того, почему задача не написалась, и
	// разбирать сбой будут по ней.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	job, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Fail(ctx, job.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, err := jobs.Job(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || got.Error == "" {
		t.Fatalf("отказ записан без причины: %+v", got)
	}
}

func TestPgЧерновикиЗаданияНеЗатираютДругДруга(t *testing.T) {
	// Перегенерация пишет второй черновик по тому же заданию. Затёртый
	// прежний сравнить не с чем, а сравнивать их будет составитель: он
	// для того и перегенерировал.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1"}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	job, err := jobs.Place(ctx, order, plan)
	if err != nil {
		t.Fatal(err)
	}

	first := Draft{Title: "Первый", Difficulty: 3}
	second := Draft{Title: "Второй", Difficulty: 4}
	перв, err := jobs.SaveDraft(ctx, job, first)
	if err != nil {
		t.Fatal(err)
	}
	втор, err := jobs.SaveDraft(ctx, job, second)
	if err != nil {
		t.Fatal(err)
	}

	drafts, err := jobs.Drafts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 || drafts[0].Title != "Первый" || drafts[1].Title != "Второй" {
		t.Fatalf("черновики легли не так: %+v", drafts)
	}

	// Опознаватель приезжает вместе с черновиком, и это не украшение:
	// принять черновик задачей — это обращение с его номером, и без
	// номера кнопке «Принять» не на чем стоять.
	if drafts[0].ID != перв || drafts[1].ID != втор {
		t.Fatalf("опознаватели разошлись: записаны %d и %d, прочитаны %d и %d",
			перв, втор, drafts[0].ID, drafts[1].ID)
	}

	// И в записанном теле его нет: тело — то, что написала модель, а
	// номер строки модель не пишет.
	var тело map[string]any
	var raw []byte
	if err := gate.QueryRow(ctx,
		`SELECT body FROM case_drafts WHERE id = $1`, перв).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &тело); err != nil {
		t.Fatal(err)
	}
	if _, есть := тело["id"]; есть {
		t.Fatalf("в записанном теле оказался опознаватель: %s", raw)
	}
}

func TestPgПланДоезжаетДоИсполнителяЦеликом(t *testing.T) {
	// Задание переживает перезапуск, и всё, что нужно конвейеру, обязано
	// быть в нём: второй поход в хранилище разошёлся бы с первым молча.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	order := Order{SourceID: sourceID, UnitLabel: "3.1", Kind: KindAction}
	plan, err := NewResolver(gate).Resolve(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	if _, err := jobs.Place(ctx, order, plan); err != nil {
		t.Fatal(err)
	}

	for {
		job, ok, err := jobs.Take(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("задание не нашлось в очереди")
		}
		if err := jobs.Done(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
		if job.SourceID != sourceID {
			continue // чужое задание из соседней проверки
		}
		if job.Plan.Target == nil || job.Plan.Target.Designation != "абз. 1" {
			t.Fatalf("эталонное положение не доехало: %+v", job.Plan.Target)
		}
		if len(job.Plan.Statements) != 2 || len(job.Plan.Siblings) != 1 {
			t.Fatalf("план доехал неполным: %+v", job.Plan)
		}
		if job.Plan.StatementWord != "указание" {
			t.Fatalf("словарь источника не доехал: %q", job.Plan.StatementWord)
		}
		return
	}
}

// Подстановка переменных обязана быть однозначной.
//
// Показанный аудитом дефект: цепочка замен перебирала карту, и значение
// одной переменной, содержащее имя другой, раскрывалось или нет как
// выпадет — 277 прогонов из 400 давали один текст, 123 другой. Проверка
// гоняет тот же случай: единицу в источнике назвали {положения}.
func TestПодстановкаПеременныхОднозначна(t *testing.T) {
	plan := Plan{StatementsMd: "ПОЛОЖЕНИЯ ИСТОЧНИКА"}
	plan.Unit.Title = "{положения}"

	seen := map[string]int{}
	for i := 0; i < 400; i++ {
		seen[Render("Название единицы: {название}", plan)]++
	}
	if len(seen) != 1 {
		for text, n := range seen {
			t.Logf("%3d раз из 400: %q", n, text)
		}
		t.Fatalf("один и тот же заказ дал %d разных заданий модели", len(seen))
	}
	for text := range seen {
		if strings.Contains(text, "ПОЛОЖЕНИЯ ИСТОЧНИКА") {
			t.Errorf("подставленное раскрылось второй раз: %q", text)
		}
	}
}
