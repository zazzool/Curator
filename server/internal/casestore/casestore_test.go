package casestore

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
	gate, err := dbgate.Open(context.Background(), dsn, 0)
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

// источник заводит приказ с единицей, у которой два положения.
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
		label, parent, path string
		depth               int
	}{
		{"3", "", "3", 0},
		{"3.1", "3", "3/3.1", 1},
	}
	for i, u := range units {
		_, err := gate.Exec(ctx,
			`INSERT INTO source_units (source_id, kind, label, parent_label, title,
			                           path, depth, answerable, ord)
			 VALUES ($1, 'entry', $2, $3, $4, $5, $6, TRUE, $7)`,
			id, u.label, u.parent, "Название "+u.label, u.path, u.depth, i)
		if err != nil {
			t.Fatalf("единица %q не заведена: %v", u.label, err)
		}
	}

	for i, st := range []struct{ designation, body string }{
		{"абз. 1", "Срок рассмотрения — десять рабочих дней."},
		{"абз. 2", "Срок продлевается однократно."},
	} {
		_, err := gate.Exec(ctx,
			`INSERT INTO source_unit_statements
			     (source_id, unit_label, kind, designation, body_md, place_ref, ord)
			 VALUES ($1, '3.1', '', $2, $3, 'с. 4', $4)`,
			id, st.designation, st.body, i)
		if err != nil {
			t.Fatalf("положение не заведено: %v", err)
		}
	}
	return id
}

// годноеТело — задача, которую можно раздавать.
func годноеТело() Body {
	return Body{
		Title: "Срок рассмотрения",
		Kind:  "recognise",
		Segments: []Segment{
			{Text: "Заявление подано в понедельник.", Statements: []string{"абз. 1"}},
			{Text: "Заявитель ждёт ответа."},
		},
		Options: []Option{
			{Label: "3.1", Text: "Десять рабочих дней"},
			{Label: "3.2", Text: "Тридцать календарных дней"},
		},
		Answer:      "3.1",
		Explanation: "Срок назван прямо в положении.",
		Difficulty:  2,
	}
}

// черновик кладёт черновик конвейера и отдаёт его номер.
func черновик(t *testing.T, gate *dbgate.Gate, sourceID int64, body Body) int64 {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	err = gate.QueryRow(context.Background(),
		`INSERT INTO case_drafts (source_id, unit_label, body)
		 VALUES ($1, '3.1', $2) RETURNING id`, sourceID, raw).Scan(&id)
	if err != nil {
		t.Fatalf("черновик не положен: %v", err)
	}
	return id
}

func TestPgЗадачаИзЧерновикаНесётПутьЕдиницы(t *testing.T) {
	// Путь копируется при заведении и одним запросом даёт подбор по
	// срезу, без соединения с деревом источника.
	gate := testGate(t)
	store := NewStore(gate)
	sourceID := источник(t, gate)

	one, err := store.FromDraft(context.Background(),
		черновик(t, gate, sourceID, годноеТело()), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if one.UnitPath != "3/3.1" {
		t.Fatalf("путь единицы не скопирован: %q", one.UnitPath)
	}
	if one.Status != StatusDraft {
		t.Fatalf("новая задача не черновик: %q", one.Status)
	}
	if !strings.HasPrefix(one.ID, "c-") {
		t.Fatalf("номер задачи не тот: %q", one.ID)
	}
}

func TestPgПубликацияРаскладываетРазметкуПоНомерамПоложений(t *testing.T) {
	// Обозначение («абз. 1») осмысленно всегда, номер положения — только
	// пока положение живо. Потому разложение делается при публикации, а
	// не при написании.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, годноеТело()), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.Publish(ctx, one.ID, "составитель")
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != StatusPublished || published.PublishedAt == nil {
		t.Fatalf("задача не выпущена: %+v", published)
	}

	var withStatement, background int
	err = gate.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE statement_id IS NOT NULL),
		        count(*) FILTER (WHERE statement_id IS NULL)
		   FROM case_chunks WHERE case_id = $1`, one.ID).Scan(&withStatement, &background)
	if err != nil {
		t.Fatal(err)
	}
	if withStatement != 1 || background != 1 {
		t.Fatalf("разметка разложена не так: подтверждающих %d, фона %d", withStatement, background)
	}
}

func TestPgФрагментНаДваПоложенияНеТеряетВторое(t *testing.T) {
	// Разметка хранится по фрагменту, и два положения у одного фрагмента
	// занимают две строки. Взять порядковый номер из номера фрагмента —
	// значит положить вторую строку под тем же ord, а база её не примет:
	// потеря вышла бы не молчаливой, но объяснял бы её потом не тот, кто
	// писал.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	body := годноеТело()
	body.Segments[0].Statements = []string{"абз. 1", "абз. 2"}
	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, body), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, one.ID, "составитель"); err != nil {
		t.Fatal(err)
	}

	var marked int
	err = gate.QueryRow(ctx,
		`SELECT count(DISTINCT statement_id) FROM case_chunks
		  WHERE case_id = $1 AND statement_id IS NOT NULL`, one.ID).Scan(&marked)
	if err != nil {
		t.Fatal(err)
	}
	if marked != 2 {
		t.Fatalf("положений в разметке %d, ожидалось 2", marked)
	}
}

func TestPgПубликацияНазываетВсеБедыРазом(t *testing.T) {
	// Составитель правит задачу в один заход, и отказ, называющий одну
	// беду из четырёх, заставляет его ходить по кругу ровно четыре раза.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	body := годноеТело()
	body.Title = ""
	body.Explanation = ""
	body.Answer = "такого варианта нет"
	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, body), "проверка")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Publish(ctx, one.ID, "составитель")
	faults, ok := err.(Faults)
	if !ok {
		t.Fatalf("отказ не списком замечаний: %v", err)
	}
	if len(faults) < 3 {
		t.Fatalf("названо бед %d, ожидалось не меньше трёх: %v", len(faults), faults)
	}
}

func TestPgСсылкаНаНесуществующееПоложениеНеРаздаётся(t *testing.T) {
	// Ссылка показывается обучающемуся как обоснование, и проверять её он
	// пойдёт в первоисточник, где ничего не найдёт.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	body := годноеТело()
	body.Segments[0].Statements = []string{"абз. 7"}
	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, body), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, one.ID, "составитель"); err == nil {
		t.Fatal("задача со ссылкой в никуда уехала в раздачу")
	}
}

func TestPgМеткаЕдиницыВУсловииНеРаздаётся(t *testing.T) {
	// Метка в условии превращает задачу в проверку внимательности.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	body := годноеТело()
	body.Segments[0].Text = "По пункту 3.1 заявление подано в понедельник."
	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, body), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, one.ID, "составитель"); err == nil {
		t.Fatal("задача с подсказкой уехала в раздачу")
	}
}

func TestPgВерсияСодержанияРастётНаВыпускеИНаСнятии(t *testing.T) {
	// Устройство сравнивает версию со своей и качает только при
	// расхождении. Не выросшая на снятии версия оставила бы на устройстве
	// задачу, которую составитель уже счёл негодной.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	before, err := store.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, годноеТело()), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, one.ID, "составитель"); err != nil {
		t.Fatal(err)
	}
	afterPublish, err := store.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterPublish <= before {
		t.Fatalf("версия не выросла на выпуске: было %d, стало %d", before, afterPublish)
	}

	if _, err := store.Withdraw(ctx, one.ID, "составитель"); err != nil {
		t.Fatal(err)
	}
	afterWithdraw, err := store.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterWithdraw <= afterPublish {
		t.Fatalf("версия не выросла на снятии: было %d, стало %d", afterPublish, afterWithdraw)
	}
}

func TestPgСнятаяЗадачаНеУдаляется(t *testing.T) {
	// На устройствах она уже стоит, попытки по ней уже записаны, и
	// задача, исчезнувшая из базы, оставила бы попытки, ссылающиеся в
	// никуда, — то есть испортила бы отчёты задним числом.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, годноеТело()), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, one.ID, "составитель"); err != nil {
		t.Fatal(err)
	}
	withdrawn, err := store.Withdraw(ctx, one.ID, "составитель")
	if err != nil {
		t.Fatal(err)
	}
	if withdrawn.Status != StatusArchived {
		t.Fatalf("задача не снята: %q", withdrawn.Status)
	}
	if _, err := store.Case(ctx, one.ID); err != nil {
		t.Fatalf("снятая задача пропала из базы: %v", err)
	}
}

func TestPgРаздаваемаяЗадачаНеПравится(t *testing.T) {
	// Правка раздаваемой задачи молча меняет то, что уже видят на
	// устройствах, и расходится с попытками, записанными по прежнему
	// тексту.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, годноеТело()), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.Publish(ctx, one.ID, "составитель")
	if err != nil {
		t.Fatal(err)
	}
	body := годноеТело()
	body.Title = "Переписано"
	if _, err := store.Save(ctx, one.ID, body, published.Revision, "составитель"); err == nil {
		t.Fatal("раздаваемая задача была переписана")
	}
}

func TestPgПравкаСверяетРедакцию(t *testing.T) {
	// Два составителя, открывшие одну задачу, иначе затирают друг друга
	// молча.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, годноеТело()), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	body := годноеТело()
	body.Title = "Первая правка"
	if _, err := store.Save(ctx, one.ID, body, one.Revision, "первый"); err != nil {
		t.Fatal(err)
	}
	body.Title = "Вторая правка той же редакцией"
	if _, err := store.Save(ctx, one.ID, body, one.Revision, "второй"); err == nil {
		t.Fatal("вторая правка устаревшей редакцией прошла")
	}

	// Прежний текст остаётся в истории: неудачную редактуру врачебного
	// текста надо уметь вернуть, а не переписывать по памяти.
	var kept string
	err = gate.QueryRow(ctx,
		`SELECT body->>'title' FROM case_revisions WHERE case_id = $1 AND revision = $2`,
		one.ID, one.Revision).Scan(&kept)
	if err != nil {
		t.Fatalf("прежняя редакция не найдена: %v", err)
	}
	if kept != годноеТело().Title {
		t.Fatalf("в историю ушёл не прежний текст: %q", kept)
	}
}

func TestPgСрезПоПутиБерётЗадачиПоддерева(t *testing.T) {
	// «Всё, что под 3» — это данные источника, а не догадка о том, как
	// устроен чужой справочник.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, годноеТело()), "проверка")
	if err != nil {
		t.Fatal(err)
	}

	found, err := store.Cases(ctx, Filter{SourceID: sourceID, Path: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != one.ID {
		t.Fatalf("срез по пути отдал не то: %+v", found)
	}

	none, err := store.Cases(ctx, Filter{SourceID: sourceID, Path: "4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("срез по чужому пути отдал задачи: %+v", none)
	}
}

func TestPgЗадачиОтдаютсяПустымСписком_АНеNil(t *testing.T) {
	// Пустой список — [], а не nil: студия ходит по нему циклом, и на
	// источнике без задач nil уехал бы наружу как null.
	gate := testGate(t)
	found, err := NewStore(gate).Cases(context.Background(),
		Filter{SourceID: источник(t, gate)})
	if err != nil {
		t.Fatal(err)
	}
	if found == nil {
		t.Fatal("задачи отданы nil, а не пустым списком")
	}
}

func TestPgПовторнаяПубликацияНеОтказывает(t *testing.T) {
	// Составитель нажал дважды, и он хотел раздачи.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	sourceID := источник(t, gate)

	one, err := store.FromDraft(ctx, черновик(t, gate, sourceID, годноеТело()), "проверка")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, one.ID, "составитель"); err != nil {
		t.Fatal(err)
	}
	again, err := store.Publish(ctx, one.ID, "составитель")
	if err != nil {
		t.Fatalf("повторная публикация отказала: %v", err)
	}
	if again.Status != StatusPublished {
		t.Fatalf("после повторной публикации состояние %q", again.Status)
	}
}

func TestPgНеразобравшийсяЧерновикНеСтановитсяЗадачей(t *testing.T) {
	// Непонятое не применяется: черновик, записанный прежней выкаткой и
	// не разобравшийся нынешней, — не черновик с пробелом.
	gate := testGate(t)
	ctx := context.Background()
	sourceID := источник(t, gate)

	var draftID int64
	err := gate.QueryRow(ctx,
		`INSERT INTO case_drafts (source_id, unit_label, body)
		 VALUES ($1, '3.1', '"не объект"'::jsonb) RETURNING id`, sourceID).Scan(&draftID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(gate).FromDraft(ctx, draftID, "проверка"); err == nil {
		t.Fatal("из неразобравшегося черновика завелась задача")
	}
}
