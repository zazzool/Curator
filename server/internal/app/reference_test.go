package app

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/progress"
	"curator/server/internal/sales"
)

// дверьСправочника поднимает /v1 вместе со справочником и заводит
// действующий источник с деревом единиц и критериями.
//
// Источник свой у каждой проверки: база одна на весь прогон, и две
// проверки, взявшие одно краткое имя, ловили бы друг друга за руку.
func дверьСправочника(t *testing.T) (*httptest.Server, *dbgate.Gate, string, string) {
	t.Helper()
	gate := testGate(t)
	keys := NewKeys(gate)

	keyID := fmt.Sprintf("справочник-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	key, err := keys.Issue(context.Background(), keyID, "Проверка справочника")
	if err != nil {
		t.Fatalf("ключ программы не заведён: %v", err)
	}

	door := NewDoor(keys, NewAccounts(gate))
	// Заведение устройства объявляет Routes, и без них токен взять негде.
	Routes(door, NewFeed(gate), NewAttempts(gate, progress.Default()), sales.NewAccess(gate))
	ReferenceRoutes(door, NewReference(gate))
	srv := httptest.NewServer(door.Handler())
	t.Cleanup(srv.Close)

	slug := fmt.Sprintf("спр-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	return srv, gate, key, slug
}

// источникСКритериями заводит действующий источник: две группы, две
// рубрики и три критерия у последней.
func источникСКритериями(t *testing.T, gate *dbgate.Gate, slug string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	err := gate.QueryRow(ctx,
		`INSERT INTO sources (slug, kind, title, unit_word, statement_word,
		                      purpose, hierarchy, completeness, status)
		 VALUES ($1, 'classification', 'МКБ-10 для проверки', 'рубрика', 'критерий',
		         'topic', 'is-a', 'complete', 'active')
		 RETURNING id`, slug).Scan(&id)
	if err != nil {
		t.Fatalf("источник не заведён: %v", err)
	}

	units := []struct {
		label, parent, title, path, kind string
		depth                            int
		answerable                       bool
	}{
		{"F30-F39", "", "Расстройства настроения", "F30-F39", "group", 0, false},
		{"F32", "F30-F39", "Депрессивный эпизод", "F30-F39/F32", "entry", 1, true},
		{"F32.1", "F32", "Депрессивный эпизод средней степени", "F30-F39/F32/F32.1", "entry", 2, true},
	}
	for i, u := range units {
		_, err := gate.Exec(ctx,
			`INSERT INTO source_units (source_id, kind, label, parent_label, title, path, depth, answerable, ord)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, u.kind, u.label, u.parent, u.title, u.path, u.depth, u.answerable, i)
		if err != nil {
			t.Fatalf("единица %q не заведена: %v", u.label, err)
		}
	}

	for i, body := range []string{
		"Два из трёх основных симптомов",
		"Не менее трёх дополнительных симптомов",
		"Длительность не менее двух недель",
	} {
		_, err := gate.Exec(ctx,
			`INSERT INTO source_unit_statements (source_id, unit_label, kind, designation, place_ref, body_md, ord)
			 VALUES ($1, 'F32.1', 'criterion', $2, 'с. 119', $3, $4)`,
			id, fmt.Sprintf("G%d", i+1), body, i)
		if err != nil {
			t.Fatalf("критерий не заведён: %v", err)
		}
	}
	return id
}

func TestPgСправочникОтдаётСвоиЕдиницыИКритерии(t *testing.T) {
	// Ради этого справочник и качается на устройство: врач в отделении без
	// сети открывает рубрику и читает её критерии.
	srv, gate, key, slug := дверьСправочника(t)
	источникСКритериями(t, gate, slug)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}

	status, body, raw := call(t, srv, "GET", "/v1/reference", auth, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	var mine map[string]any
	for _, s := range body["sources"].([]any) {
		one := s.(map[string]any)
		if one["slug"] == slug {
			mine = one
		}
	}
	if mine == nil {
		t.Fatalf("заведённого справочника нет в списке: %s", raw)
	}
	if mine["units"].(float64) != 3 || mine["statements"].(float64) != 3 {
		t.Errorf("размер закачки назван неверно: %v единиц, %v положений",
			mine["units"], mine["statements"])
	}
	if mine["unitWord"] != "рубрика" || mine["statementWord"] != "критерий" {
		t.Errorf("словарь источника не доехал: %v", mine)
	}

	status, body, raw = call(t, srv, "GET", "/v1/reference/"+slug+"/units", auth, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	units := body["units"].([]any)
	if len(units) != 3 {
		t.Fatalf("единиц %d, ожидалось 3: %s", len(units), raw)
	}
	// Группа не пригодна к ответу, и это уезжает на устройство: без
	// отличия группы от рубрики список в тысячу строк остаётся без входа.
	first := units[0].(map[string]any)
	if first["label"] != "F30-F39" || first["answerable"].(bool) {
		t.Errorf("группа доехала как пригодная к ответу: %v", first)
	}
	last := units[2].(map[string]any)
	if last["statements"].(float64) != 3 {
		t.Errorf("у рубрики не посчитаны критерии: %v", last)
	}
	if last["path"] != "F30-F39/F32/F32.1" {
		t.Errorf("путь единицы не доехал: %v", last["path"])
	}

	status, body, raw = call(t, srv, "GET", "/v1/reference/"+slug+"/statements", auth, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	statements := body["statements"].([]any)
	if len(statements) != 3 {
		t.Fatalf("критериев %d, ожидалось 3: %s", len(statements), raw)
	}
	one := statements[0].(map[string]any)
	if one["bodyMd"] != "Два из трёх основных симптомов" || one["designation"] != "G1" {
		t.Errorf("критерий доехал не целым: %v", one)
	}
	if one["placeRef"] != "с. 119" {
		t.Errorf("ссылка на место потеряна: %v", one["placeRef"])
	}
}

func TestPgСтраницыСправочникаНеЗацикливаютсяИНеТеряютЕдиниц(t *testing.T) {
	// Курсор по метке, а не по ord: ord у двух единиц совпадает сплошь и
	// рядом, и страницы на нём зацикливались бы — обход не кончался бы
	// никогда, а врач смотрел бы на бесконечную закачку.
	srv, gate, key, slug := дверьСправочника(t)
	источникСКритериями(t, gate, slug)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}

	seen := map[string]bool{}
	after := ""
	for range 10 {
		path := "/v1/reference/" + slug + "/units?limit=1"
		if after != "" {
			path += "&after=" + after
		}
		status, body, raw := call(t, srv, "GET", path, auth, nil)
		if status != http.StatusOK {
			t.Fatalf("код %d: %s", status, raw)
		}
		units := body["units"].([]any)
		for _, u := range units {
			label := u.(map[string]any)["label"].(string)
			if seen[label] {
				t.Fatalf("единица %q пришла дважды: страницы зациклились", label)
			}
			seen[label] = true
		}
		after, _ = body["next"].(string)
		if after == "" {
			break
		}
	}
	if len(seen) != 3 {
		t.Errorf("обход собрал %d единиц вместо трёх: %v", len(seen), seen)
	}
}

func TestPgЧерновойСправочникУстройствуНеВиден(t *testing.T) {
	// Черновик составителя — работа, а не материал для врача. Отличить
	// «нет такого» от «есть, но черновик» устройству нельзя: о черновиках
	// ему знать незачем.
	srv, gate, key, slug := дверьСправочника(t)
	id := источникСКритериями(t, gate, slug)
	if _, err := gate.Exec(context.Background(),
		`UPDATE sources SET status = 'draft' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}

	status, body, raw := call(t, srv, "GET", "/v1/reference", auth, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	for _, s := range body["sources"].([]any) {
		if s.(map[string]any)["slug"] == slug {
			t.Fatalf("черновой справочник виден устройству: %s", raw)
		}
	}
	status, _, _ = call(t, srv, "GET", "/v1/reference/"+slug+"/units", auth, nil)
	if status != http.StatusNotFound {
		t.Errorf("единицы чернового справочника отданы с кодом %d", status)
	}
}

func TestPgПустойСписокСправочниковЭтоСкобкиАНеNull(t *testing.T) {
	// Устройство ходит по списку циклом, и null уронил бы его на
	// исправном случае — на свежей установке без источников.
	srv, _, key, _ := дверьСправочника(t)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}

	status, _, raw := call(t, srv, "GET", "/v1/reference", auth, nil)
	if status != http.StatusOK {
		t.Fatalf("код %d: %s", status, raw)
	}
	if !списком(raw, "sources") {
		t.Errorf("список справочников отдан не списком: %s", raw)
	}
}

// списком проверяет, что поле отдано списком, а не null.
//
// Смотрим в сырой ответ, а не в разобранный: разбор превращает и null, и
// [] в пустоту Go, то есть ровно ту разницу, ради которой проверка и
// написана, он и стирает.
func списком(raw, field string) bool {
	return strings.Contains(raw, `"`+field+`":[`) || strings.Contains(raw, `"`+field+`": [`)
}
