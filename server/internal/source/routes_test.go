package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"curator/server/internal/studio"
)

// newDesk поднимает стол студии с ручками источника и заводит человека с
// названными правами.
//
// Права называются в каждой проверке поимённо: «маршрута без права не
// бывает» проверяется не тем, что маршрут отвечает, а тем, что он
// отказывает человеку без права.
func newDesk(t *testing.T, perms ...studio.Permission) (*httptest.Server, string) {
	t.Helper()
	gate := testGate(t)
	users, sessions := studio.NewUsers(gate, nil), studio.NewSessions(gate)
	desk := studio.NewDesk(users, sessions)
	studio.Routes(desk)
	Routes(desk, NewStore(gate))

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

// call — обращение к ручке с токеном.
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

// newSourceJSON заводит источник ручкой, а не хранилищем: проверке нужен
// тот путь, которым пойдёт студия.
func newSourceJSON(t *testing.T, srv *httptest.Server, token string) int64 {
	t.Helper()
	status, body, raw := call(t, srv, token, "POST", "/admin/api/sources", map[string]any{
		"slug":          fmt.Sprintf("приказ-%d-%d", time.Now().UnixNano(), rand.Intn(1000)),
		"kind":          "decree",
		"title":         "Приказ для проверки",
		"unitWord":      "пункт",
		"statementWord": "положение",
		"purpose":       "legal",
		"hierarchy":     "part-of",
		"completeness":  "fragment",
	})
	if status != http.StatusCreated {
		t.Fatalf("источник не заведён: %d %s", status, raw)
	}
	return int64(body["id"].(float64))
}

// upload приносит файл формой — тем же способом, каким его принесёт студия.
func upload(t *testing.T, srv *httptest.Server, token string, sourceID int64, filename string, body []byte) (int, map[string]any, string) {
	t.Helper()
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/admin/api/sources/%d/documents", srv.URL, sourceID), &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("ответ загрузки не разобран: %s", raw)
	}
	return resp.StatusCode, parsed, string(raw)
}

func TestPgРучкаБезВходаНеОтвечает(t *testing.T) {
	srv, _ := newDesk(t, studio.PermSourceRead)
	status, _, _ := call(t, srv, "", "GET", "/admin/api/sources", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("ручка ответила без входа: %d", status)
	}
}

func TestPgПринятьРазборБезПраваНельзя(t *testing.T) {
	// Читать источник и принимать разбор — разные права намеренно: принять
	// разбор значит решить, что теперь считается истиной источника.
	srv, token := newDesk(t, studio.PermSourceRead)
	if status, _, _ := call(t, srv, token, "GET", "/admin/api/sources", nil); status != http.StatusOK {
		t.Fatalf("чтение отказало человеку с правом чтения: %d", status)
	}
	status, _, raw := call(t, srv, token, "POST", "/admin/api/sources", map[string]any{
		"slug": "мимо", "kind": "decree", "title": "Приказ",
		"unitWord": "пункт", "statementWord": "положение",
		"purpose": "legal", "hierarchy": "part-of", "completeness": "fragment",
	})
	if status != http.StatusForbidden {
		t.Fatalf("источник заведён человеком без права: %d %s", status, raw)
	}
}

func TestPgИсточникБезОсиНеЗаводится(t *testing.T) {
	// Ось обязана быть названа: по ней подбор решает, складываются ли два
	// источника в один список или стоят поперёк друг друга. Умолчания нет
	// намеренно — молча выбранная ось даёт молча неверный подбор.
	srv, token := newDesk(t, studio.PermSourceAccept)
	status, _, raw := call(t, srv, token, "POST", "/admin/api/sources", map[string]any{
		"slug": "без-оси", "kind": "decree", "title": "Приказ",
		"unitWord": "пункт", "statementWord": "положение",
		"hierarchy": "part-of", "completeness": "fragment",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("источник без оси заведён: %d %s", status, raw)
	}
}

func TestPgНезнакомоеПолеЗапросаОтклоняется(t *testing.T) {
	// «Непонятое не применяется»: студия, пославшая поле с опечаткой, иначе
	// получила бы успешный ответ и молча потеряла введённое человеком.
	srv, token := newDesk(t, studio.PermSourceAccept)
	status, _, raw := call(t, srv, token, "POST", "/admin/api/sources", map[string]any{
		"slug": "с-опечаткой", "kind": "decree", "title": "Приказ",
		"unitWord": "пункт", "statementWord": "положение",
		"purpose": "legal", "hierarchy": "part-of", "completeness": "fragment",
		"complitness": "complete",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("запрос с незнакомым полем принят: %d %s", status, raw)
	}
}

func TestPgЗагрузкаРежетДокументНаКуски(t *testing.T) {
	// Куски считаются при загрузке, а не по отдельной команде: документ,
	// лежащий неразобранным, выглядит загруженным и молча не работает.
	srv, token := newDesk(t, studio.PermSourceRead, studio.PermSourceAccept)
	sourceID := newSourceJSON(t, srv, token)

	doc := "# Общие положения\n\nПервый абзац.\n\n# Порядок\n\nВторой абзац.\n"
	status, body, raw := upload(t, srv, token, sourceID, "приказ.md", []byte(doc))
	if status != http.StatusCreated {
		t.Fatalf("документ не принят: %d %s", status, raw)
	}
	if got := int(body["fragments"].(float64)); got != 2 {
		t.Fatalf("документ порезан на %d кусков вместо двух", got)
	}
	docID := int64(body["id"].(float64))

	status, fragments, raw := call(t, srv, token,
		"GET", fmt.Sprintf("/admin/api/documents/%d/fragments", docID), nil)
	if status != http.StatusOK {
		t.Fatalf("куски не отданы: %d %s", status, raw)
	}
	list := fragments["fragments"].([]any)
	if len(list) != 2 {
		t.Fatalf("отдано %d кусков вместо двух: %s", len(list), raw)
	}
	first := list[0].(map[string]any)["body"].(string)
	if !strings.HasPrefix(first, "# Общие положения") {
		// Заголовок уходит вместе с телом намеренно: без него кусок теряет
		// то единственное, что говорит, о чём он.
		t.Fatalf("кусок приехал без своего заголовка: %q", first)
	}
}

func TestPgОдинИТотЖеФайлЭтоОдинДокумент(t *testing.T) {
	// Иначе разбор пойдёт по обеим копиям и даст две редакции одного
	// источника. Человек, загрузивший файл второй раз, хотел работать с
	// ним, а не читать сообщение об ошибке.
	srv, token := newDesk(t, studio.PermSourceRead, studio.PermSourceAccept)
	sourceID := newSourceJSON(t, srv, token)
	body := []byte(fmt.Sprintf("# Раз\n\nтело %d\n", time.Now().UnixNano()))

	_, first, _ := upload(t, srv, token, sourceID, "приказ.md", body)
	_, second, raw := upload(t, srv, token, sourceID, "приказ-копия.md", body)
	if first["id"] != second["id"] {
		t.Fatalf("один файл лёг двумя документами: %v и %s", first["id"], raw)
	}
}

func TestPgPDFОтклоняетсяРучкой(t *testing.T) {
	srv, token := newDesk(t, studio.PermSourceAccept)
	sourceID := newSourceJSON(t, srv, token)
	status, body, raw := upload(t, srv, token, sourceID, "приказ.pdf", []byte("%PDF-1.7\nтело"))
	if status != http.StatusUnsupportedMediaType {
		t.Fatalf("PDF принят ручкой: %d %s", status, raw)
	}
	if !strings.Contains(body["error"].(string), "PDF") {
		t.Fatalf("отказ не говорит, в чём дело: %s", raw)
	}
}

func TestPgПутьИсточникаОтКонцаДоКонца(t *testing.T) {
	// Весь этап целиком тем же путём, каким по нему пойдёт студия: завести
	// источник, принести документ, положить черновик, принять его и взять
	// срез по пути.
	srv, token := newDesk(t, studio.PermSourceRead, studio.PermSourceAccept)
	sourceID := newSourceJSON(t, srv, token)

	_, uploaded, _ := upload(t, srv, token, sourceID, "приказ.md",
		[]byte(fmt.Sprintf("# Порядок\n\nтело %d\n", time.Now().UnixNano())))
	docID := int64(uploaded["id"].(float64))

	draft := map[string]any{
		"units": []map[string]string{
			{"label": "3", "parentLabel": "", "title": "Порядок"},
			{"label": "3.2", "parentLabel": "3", "title": "Сроки"},
			{"label": "4", "parentLabel": "", "title": "Заключительные положения"},
		},
		"statements": []map[string]string{
			{"unitLabel": "3.2", "kind": "обязанность", "designation": "абз. 1",
				"body": "Срок рассмотрения — десять рабочих дней.", "placeRef": "с. 4"},
		},
	}
	status, _, raw := call(t, srv, token, "PUT", fmt.Sprintf("/admin/api/documents/%d/draft", docID), draft)
	if status != http.StatusOK {
		t.Fatalf("черновик не лёг: %d %s", status, raw)
	}

	status, shown, raw := call(t, srv, token, "GET", fmt.Sprintf("/admin/api/documents/%d/draft", docID), nil)
	if status != http.StatusOK || len(shown["units"].([]any)) != 3 {
		t.Fatalf("черновик прочитан не тем, чем лёг: %d %s", status, raw)
	}

	status, accepted, raw := call(t, srv, token, "POST", fmt.Sprintf("/admin/api/documents/%d/accept", docID), nil)
	if status != http.StatusOK {
		t.Fatalf("черновик не принят: %d %s", status, raw)
	}
	if got := int(accepted["accepted"].(float64)); got != 3 {
		t.Fatalf("принято %d единиц вместо трёх: %s", got, raw)
	}

	// Срез по пути — то самое, ради чего путь вообще считается: «всё, что
	// под пунктом 3» берётся связью с родителем, а не отрезанием знаков от
	// метки.
	status, sliced, raw := call(t, srv, token, "GET",
		fmt.Sprintf("/admin/api/sources/%d/units?path=3", sourceID), nil)
	if status != http.StatusOK {
		t.Fatalf("срез не отдан: %d %s", status, raw)
	}
	units := sliced["units"].([]any)
	if len(units) != 2 {
		t.Fatalf("в срезе под пунктом 3 оказалось %d единиц вместо двух: %s", len(units), raw)
	}
	for _, u := range units {
		if label := u.(map[string]any)["label"].(string); label == "4" {
			t.Fatalf("в срез под пунктом 3 попал пункт 4: %s", raw)
		}
	}
}

func TestPgЧерновикСПотеряннымРодителемНеПринимаетсяВовсе(t *testing.T) {
	// Отказ целиком, а не приёмка годных: источник с потерянной веткой
	// выглядит исправным, а задачи по ней не выпадают в подборе.
	srv, token := newDesk(t, studio.PermSourceRead, studio.PermSourceAccept)
	sourceID := newSourceJSON(t, srv, token)
	_, uploaded, _ := upload(t, srv, token, sourceID, "приказ.md",
		[]byte(fmt.Sprintf("# Раздел\n\nтело %d\n", time.Now().UnixNano())))
	docID := int64(uploaded["id"].(float64))

	draft := map[string]any{
		"units": []map[string]string{
			{"label": "5", "parentLabel": "", "title": "Раздел"},
			{"label": "5.1", "parentLabel": "нет-такого", "title": "Потерянный"},
		},
		"statements": []map[string]string{},
	}
	if status, _, raw := call(t, srv, token, "PUT", fmt.Sprintf("/admin/api/documents/%d/draft", docID), draft); status != http.StatusOK {
		t.Fatalf("черновик не лёг: %d %s", status, raw)
	}
	status, _, raw := call(t, srv, token, "POST", fmt.Sprintf("/admin/api/documents/%d/accept", docID), nil)
	if status != http.StatusBadRequest {
		t.Fatalf("черновик с потерянным родителем принят: %d %s", status, raw)
	}

	status, units, raw := call(t, srv, token, "GET", fmt.Sprintf("/admin/api/sources/%d/units", sourceID), nil)
	if status != http.StatusOK {
		t.Fatal(raw)
	}
	if len(units["units"].([]any)) != 0 {
		t.Fatalf("после отказа в источник попала часть черновика: %s", raw)
	}
}

func TestPgПустойСписокЕдиницЭтоСкобкиАНеNull(t *testing.T) {
	// Пустой список роняет студию белым экраном именно на исправном
	// случае — на источнике, куда ещё ничего не принято.
	srv, token := newDesk(t, studio.PermSourceRead, studio.PermSourceAccept)
	sourceID := newSourceJSON(t, srv, token)
	_, _, raw := call(t, srv, token, "GET", fmt.Sprintf("/admin/api/sources/%d/units", sourceID), nil)
	if !strings.Contains(raw, `"units":[]`) {
		t.Fatalf("пустой список единиц отдан не как []: %s", raw)
	}
	_, _, raw = call(t, srv, token, "GET", fmt.Sprintf("/admin/api/sources/%d/documents", sourceID), nil)
	if !strings.Contains(raw, `"documents":[]`) {
		t.Fatalf("пустой список документов отдан не как []: %s", raw)
	}
}

func TestPgПовторнаяПриёмкаНеУдваиваетПоложения(t *testing.T) {
	// Приёмку повторяют: разбор переделали, нашли пропущенный пункт, нажали
	// «принять» второй раз. Без уборки прежних положения легли бы вторым
	// слоем поверх первого, и врач увидел бы каждое дважды — причём у
	// единиц ничего подобного не случилось бы, там UPSERT, и расхождение
	// заметили бы не сразу.
	srv, token := newDesk(t, studio.PermSourceRead, studio.PermSourceAccept)
	sourceID := newSourceJSON(t, srv, token)
	_, uploaded, _ := upload(t, srv, token, sourceID, "приказ.md",
		[]byte(fmt.Sprintf("# Порядок\n\nтело %d\n", time.Now().UnixNano())))
	docID := int64(uploaded["id"].(float64))

	draft := map[string]any{
		"units": []map[string]string{{"label": "7", "parentLabel": "", "title": "Порядок"}},
		"statements": []map[string]string{
			{"unitLabel": "7", "kind": "обязанность", "designation": "абз. 1",
				"body": "Срок рассмотрения — десять рабочих дней.", "placeRef": "с. 4"},
		},
	}
	path := fmt.Sprintf("/admin/api/documents/%d/draft", docID)
	accept := fmt.Sprintf("/admin/api/documents/%d/accept", docID)
	for range 2 {
		if status, _, raw := call(t, srv, token, "PUT", path, draft); status != http.StatusOK {
			t.Fatalf("черновик не лёг: %d %s", status, raw)
		}
		if status, _, raw := call(t, srv, token, "POST", accept, nil); status != http.StatusOK {
			t.Fatalf("черновик не принят: %d %s", status, raw)
		}
	}

	status, body, raw := call(t, srv, token, "GET",
		fmt.Sprintf("/admin/api/sources/%d/statements?unit=7", sourceID), nil)
	if status != http.StatusOK {
		t.Fatalf("положения не отданы: %d %s", status, raw)
	}
	if got := len(body["statements"].([]any)); got != 1 {
		t.Fatalf("после двух приёмок положений стало %d вместо одного: %s", got, raw)
	}
}
