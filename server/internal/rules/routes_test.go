package rules

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

// newDesk поднимает стол студии с ручками свода и заводит человека с
// названными правами.
//
// Права называются поимённо: «маршрута без права не бывает» проверяется
// не тем, что маршрут отвечает, а тем, что он отказывает человеку без
// права.
func newDesk(t *testing.T, perms ...studio.Permission) (*httptest.Server, string, *Store) {
	t.Helper()
	srv, token, store, _ := newDeskWith(t, nil, perms...)
	return srv, token, store
}

// newDeskWith — то же, но с подставной моделью для уплотнения свода.
func newDeskWith(t *testing.T, talker Talker, perms ...studio.Permission) (*httptest.Server, string, *Store, *studio.Desk) {
	t.Helper()
	gate := testGate(t)
	users, sessions := studio.NewUsers(gate, nil), studio.NewSessions(gate)
	desk := studio.NewDesk(users, sessions)
	studio.Routes(desk)

	store := NewStore(gate)
	Routes(desk, store, talker)

	ctx := context.Background()
	login := fmt.Sprintf("свод-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
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
	return srv, token, store, desk
}

func call(t *testing.T, srv *httptest.Server, token, method, path string, body any) (int, map[string]any) {
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
	return resp.StatusCode, parsed
}

func TestPgСводЗакрытПравомЗаданий(t *testing.T) {
	// Свод уходит в задание блоком: правя правило, составитель решает,
	// какими будут все будущие задачи. Открой ручку правом заказа — и
	// тот, кому доверена одна задача, получил бы власть над всеми.
	srv, token, _ := newDesk(t, studio.PermGenerate)
	if status, _ := call(t, srv, token, "GET", "/admin/api/rules", nil); status != http.StatusForbidden {
		t.Fatalf("свод открылся по праву заказа: %d", status)
	}
	if status, _ := call(t, srv, token, "POST", "/admin/api/rules", ruleRequest{
		Title: "Чужое правило", Text: "Написано без права.", Kind: string(KindStructure),
	}); status != http.StatusForbidden {
		t.Fatalf("правило записалось по праву заказа: %d", status)
	}
}

func TestPgПравкаПравилаНеСбрасываетНакопленное(t *testing.T) {
	// Живой класс отказа: сборка правила из одного запроса обнуляет
	// счётчики. Правило, дозревавшее неделю, после первой же правки
	// формулировки вернулось бы в кандидаты — то есть перестало бы
	// уходить в задание, и заметили бы это по качеству задач, а не по
	// экрану.
	srv, token, store := newDesk(t, studio.PermPrompts)
	ctx := context.Background()

	id := свойID("накопленное")
	if _, err := store.Save(ctx, Rule{
		ID:            id,
		Title:         "Слово «явка» в условии",
		Text:          "Не пиши в условии слово «явка».",
		Kind:          KindSubstance,
		Source:        FromLint,
		Scope:         Scope{Sources: []int64{4242}},
		Confirmations: Quorum,
		SeenJobs:      []int64{900, 901, 902},
	}); err != nil {
		t.Fatal(err)
	}

	status, body := call(t, srv, token, "PUT", "/admin/api/rules/"+id, ruleRequest{
		Title: "Слово «явка» в условии",
		Text:  "Переписанная формулировка того же правила.",
		Kind:  string(KindSubstance),
		Scope: Scope{Sources: []int64{4242}},
	})
	if status != http.StatusOK {
		t.Fatalf("правка не прошла: %d %v", status, body)
	}
	if got, _ := body["confirmations"].(float64); int(got) != Quorum {
		t.Fatalf("подтверждения сброшены правкой: %v", body["confirmations"])
	}
	if body["status"] != string(Active) {
		t.Fatalf("дозревшее правило после правки стало «%v»", body["status"])
	}
	if body["text"] != "Переписанная формулировка того же правила." {
		t.Fatalf("текст не сохранился: %v", body["text"])
	}
}

func TestPgПравилоСоставителяДействуетСразуИНеПадаетВКандидаты(t *testing.T) {
	// Он и есть подтверждение. Пересчитай счётчик состояние — и ручка
	// ответила бы успехом, показав «кандидат»: составитель считал бы,
	// что правило написано и работает, а оно не уходило бы никуда.
	srv, token, _ := newDesk(t, studio.PermPrompts)

	status, body := call(t, srv, token, "POST", "/admin/api/rules", ruleRequest{
		Title: "В условии не бывает служебных обозначений",
		Text:  "Не пиши в условии служебных обозначений документа.",
		Kind:  string(KindStructure),
	})
	if status != http.StatusOK {
		t.Fatalf("правило не записалось: %d %v", status, body)
	}
	if body["status"] != string(Active) {
		t.Fatalf("написанное составителем правило заведено как «%v»", body["status"])
	}
	if body["source"] != string(FromCurator) {
		t.Fatalf("источник правила «%v» вместо составителя", body["source"])
	}
	if body["pinned"] != true {
		t.Fatal("состояние не отмечено назначенным человеком: счётчик уведёт его в кандидаты")
	}
}

func TestPgИсточникПравилаНеНазываетсяИзвне(t *testing.T) {
	// Назови правило «встроенным» — и оно получило бы чужое старшинство
	// при споре правил, то есть выиграло бы у написанного другим
	// составителем. Опознаватель «builtin:show-dont-name» сделал бы то же
	// грубее: правило встало бы на место встроенного.
	//
	// Дверей тут две, и проверяются обе. Прежде здесь проверялась одна, и
	// не та: запрос с лишними полями до присваивания не доезжает вовсе, а
	// проверка читала источник из тела ОТКАЗА и видела там пустоту.
	srv, token, _ := newDesk(t, studio.PermPrompts)

	// Первая дверь — разбор тела. Лишнее поле редакционному API отказ, а
	// не тихо отброшенное поле: отброшенное молча, оно вернуло бы
	// пославшему успех на запрос, который сервер исполнил не так.
	status, _ := call(t, srv, token, "POST", "/admin/api/rules", map[string]any{
		"title":  "Правило с чужим старшинством",
		"text":   "Текст этого правила ничем не примечателен.",
		"kind":   string(KindStructure),
		"source": string(Builtin),
		"id":     "builtin:show-dont-name",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("источник и опознаватель приняты извне: %d", status)
	}

	// Вторая — само заведение. Исправный запрос источника не называет
	// вовсе, и ставит его сервер; опознаватель он даёт свой, с приставкой
	// составителя, и встать на место встроенного правила таким нельзя.
	status, body := call(t, srv, token, "POST", "/admin/api/rules", ruleRequest{
		Title: "Правило без чужого старшинства",
		Text:  "Текст этого правила ничем не примечателен.",
		Kind:  string(KindStructure),
	})
	if status != http.StatusOK {
		t.Fatalf("правило не записалось: %d %v", status, body)
	}
	if body["source"] != string(FromCurator) {
		t.Fatalf("источник правила «%v» вместо составителя", body["source"])
	}
	id, _ := body["id"].(string)
	if !strings.HasPrefix(id, "curator:") {
		t.Fatalf("опознаватель «%s» дан не сервером", id)
	}
}

func TestPgПустойСводОтдаётсяСпискомАНеNULL(t *testing.T) {
	// Студия ходит по своду циклом и падает белым экраном ровно на
	// исправном случае — на новой установке, где правил ещё нет.
	srv, token, _ := newDesk(t, studio.PermPrompts)
	status, body := call(t, srv, token, "GET", "/admin/api/rules", nil)
	if status != http.StatusOK {
		t.Fatalf("свод не прочитан: %d %v", status, body)
	}
	if _, ok := body["rules"].([]any); !ok {
		t.Fatalf("свод отдан не списком: %#v", body["rules"])
	}
}

func TestPgКаталогПредикатовПриходитСоСловарями(t *testing.T) {
	// Форма проверки строится по каталогу, и повторять его в студии
	// нельзя: повторённый разойдётся молча — ровно на том предикате,
	// который добавили последним, — и составитель выберет то, чего
	// исполнитель не знает.
	srv, token, _ := newDesk(t, studio.PermPrompts)
	status, body := call(t, srv, token, "GET", "/admin/api/rules/checks", nil)
	if status != http.StatusOK {
		t.Fatalf("каталог предикатов отдан кодом %d", status)
	}
	список, _ := body["checks"].([]any)
	if len(список) != len(Catalog()) {
		t.Fatalf("предикатов в ответе %d, а в каталоге %d", len(список), len(Catalog()))
	}
	имена := map[string]bool{}
	for _, one := range список {
		row, _ := one.(map[string]any)
		if fmt.Sprint(row["title"]) == "" || fmt.Sprint(row["about"]) == "" {
			t.Fatalf("предикат приехал без русского описания: %v", row)
		}
		if params, _ := row["params"].([]any); len(params) == 0 && row["type"] != string(CheckSpouseGender) {
			t.Fatalf("у предиката не названо ни одного довода: %v", row)
		}
		имена[fmt.Sprint(row["type"])] = true
	}
	for _, spec := range Catalog() {
		if !имена[string(spec.Type)] {
			t.Fatalf("предикат %q не уехал в студию", spec.Type)
		}
	}

	// Словари закрыты и приезжают вместе с предикатами.
	for _, ключ := range []string{"where", "what", "gender"} {
		словарь, _ := body[ключ].([]any)
		if len(словарь) == 0 {
			t.Fatalf("словарь %q не приехал", ключ)
		}
		for _, one := range словарь {
			row, _ := one.(map[string]any)
			if fmt.Sprint(row["value"]) == "" || fmt.Sprint(row["word"]) == "" {
				t.Fatalf("в словаре %q запись без значения или без слова: %v", ключ, row)
			}
		}
	}
	// «Считать» приходит теми же словами, какими исполнитель их называет:
	// разойдись они — составитель выбрал бы «фрагменты», а померилось бы
	// другое.
	что, _ := body["what"].([]any)
	for _, one := range что {
		row, _ := one.(map[string]any)
		if fmt.Sprint(row["word"]) != CountTitle(fmt.Sprint(row["value"])) {
			t.Fatalf("слово «считать» разошлось с исполнителем: %v", row)
		}
	}
}

func TestPgКаталогПредикатовЗакрытПравомЗаданий(t *testing.T) {
	srv, token, _ := newDesk(t, studio.PermGenerate)
	if status, _ := call(t, srv, token, "GET", "/admin/api/rules/checks", nil); status != http.StatusForbidden {
		t.Fatalf("каталог предикатов открылся по праву заказа: %d", status)
	}
}

func TestPgПроверкаЗаписываетсяФормойИЧитаетсяСловами(t *testing.T) {
	srv, token, _ := newDesk(t, studio.PermPrompts)
	status, body := call(t, srv, token, "POST", "/admin/api/rules", map[string]any{
		"title": "Алкоголь в условии", "text": "Не пиши про алкоголь.",
		"why": "Протекало.", "kind": string(KindSubstance),
		"check": map[string]any{
			"type": string(CheckForbidWords), "words": []string{"Алкоголь", "спиртное"},
			"where": []string{FieldSegments, FieldTitle},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("правило с проверкой не записалось: %d, %v", status, body)
	}
	// Проверка уезжает и записью, и фразой: список читается фразой, а
	// форма открывается записью.
	check, _ := body["check"].(map[string]any)
	if check == nil {
		t.Fatalf("проверка не вернулась записью: %v", body)
	}
	if fmt.Sprint(body["checkWords"]) == "" {
		t.Fatalf("проверка вернулась без фразы: %v", body)
	}
	// Слова причёсаны: набранное с большой буквы — то же слово.
	words, _ := check["words"].([]any)
	if len(words) != 2 || fmt.Sprint(words[0]) != "алкоголь" {
		t.Fatalf("слова проверки не причёсаны: %v", words)
	}
}

func TestPgНегоднаяПроверкаОтказываетсяСловами(t *testing.T) {
	// Живой класс отказа: свод выбрасывает негодную проверку молча
	// (Rule.Normalize), и для проверки из ввоза это верно. Составителю
	// же тот же выброс ответил бы успехом — правило осталось бы без
	// проверки, а в списке стояло бы «проверяется машинно» ровно до
	// перечитывания страницы.
	srv, token, _ := newDesk(t, studio.PermPrompts)
	случаи := []struct {
		имя   string
		check map[string]any
		слово string
	}{
		{"запрет без слов", map[string]any{"type": string(CheckForbidWords)}, "ни одного слова"},
		{"несовместимое без условия", map[string]any{
			"type": string(CheckForbidWhen), "words": []string{"жена"}}, "условие"},
		{"длина без пределов", map[string]any{"type": string(CheckLength)}, "предел"},
		{"пределы наизнанку", map[string]any{
			"type": string(CheckLength), "min": 900, "max": 100}, "больше верхнего"},
		{"негодное выражение", map[string]any{
			"type": string(CheckPattern), "pattern": "("}, "не собирается"},
		{"предиката нет", map[string]any{"type": "выдуманный"}, "в каталоге нет"},
		{"незнакомое место", map[string]any{
			"type": string(CheckForbidWords), "words": []string{"жена"},
			"where": []string{"options"}}, "проверка не умеет"},
		{"незнакомый счёт", map[string]any{
			"type": string(CheckCount), "min": 2, "what": "statements"}, "проверка не умеет"},
		{"незнакомый пол", map[string]any{
			"type": string(CheckForbidWords), "words": []string{"жена"},
			"gender": "ж"}, "мужской или женский"},
	}
	for _, случай := range случаи {
		status, body := call(t, srv, token, "POST", "/admin/api/rules", map[string]any{
			"title": "Правило " + случай.имя, "text": "Текст.", "kind": string(KindStructure),
			"check": случай.check,
		})
		if status != http.StatusBadRequest {
			t.Fatalf("%s: правило записалось кодом %d, %v", случай.имя, status, body)
		}
		if !strings.Contains(fmt.Sprint(body["error"]), случай.слово) {
			t.Fatalf("%s: отказ не называет, чего не хватает: %v", случай.имя, body["error"])
		}
	}
}

func TestPgПравкаТекстаНеСнимаетПроверку(t *testing.T) {
	// Три состояния поля, и различать их обязательно: поля нет — не
	// трогали, null — снимают, запись — ставят. Разобранная в указатель,
	// «нет поля» и «null» стали бы одним nil, и правка формулировки
	// молча снимала бы проверку — правило осталось бы текстом, а
	// составитель считал бы, что оно меряется.
	srv, token, _ := newDesk(t, studio.PermPrompts)
	_, созданное := call(t, srv, token, "POST", "/admin/api/rules", map[string]any{
		"title": "Длина условия", "text": "Условие не должно быть простынёй.",
		"kind": string(KindStructure),
		"check": map[string]any{
			"type": string(CheckLength), "max": 900,
		},
	})
	id := fmt.Sprint(созданное["id"])

	// Правка текста без поля check.
	status, правленое := call(t, srv, token, "PUT", "/admin/api/rules/"+id, map[string]any{
		"title": "Длина условия", "text": "Условие короче простыни.",
		"kind": string(KindStructure),
	})
	if status != http.StatusOK {
		t.Fatalf("правка текста отказала: %d, %v", status, правленое)
	}
	check, _ := правленое["check"].(map[string]any)
	if check == nil || fmt.Sprint(check["max"]) != "900" {
		t.Fatalf("правка текста сняла проверку: %v", правленое["check"])
	}

	// А null снимает её намеренно, и правило остаётся текстом.
	status, снятое := call(t, srv, token, "PUT", "/admin/api/rules/"+id, map[string]any{
		"title": "Длина условия", "text": "Условие короче простыни.",
		"kind": string(KindStructure), "check": nil,
	})
	if status != http.StatusOK {
		t.Fatalf("снятие проверки отказало: %d, %v", status, снятое)
	}
	if снятое["check"] != nil {
		t.Fatalf("проверка не снялась: %v", снятое["check"])
	}
	if fmt.Sprint(снятое["checkWords"]) != "" {
		t.Fatalf("фраза проверки осталась у правила без проверки: %v", снятое["checkWords"])
	}
	if fmt.Sprint(снятое["text"]) != "Условие короче простыни." {
		t.Fatalf("снятие проверки потеряло текст правила: %v", снятое)
	}
}
