package gen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"curator/server/internal/studio"
)

func TestPgСвоёЗаданиеИсточникаСтаршеОбщего(t *testing.T) {
	// Разбор приказа и разбор клинических рекомендаций читают по-разному,
	// и одно задание на оба пришлось бы писать так обще, что оно
	// перестало бы помогать обоим.
	ctx := context.Background()
	gate := testGate(t)
	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	свой := источник(t, gate)
	чужой := источник(t, gate)

	// До правки оба источника берут общее задание.
	общее, err := prompts.ForNode(ctx, NodeCompose, свой)
	if err != nil {
		t.Fatal(err)
	}
	if !общее.IsDefault || общее.SourceID != 0 {
		t.Fatalf("без своего задания взялось не общее: %+v", общее)
	}

	своё, err := prompts.Fork(ctx, свой, NodeCompose)
	if err != nil {
		t.Fatal(err)
	}
	if своё.ID == общее.ID {
		t.Fatal("своё задание источника оказалось тем же самым: правка меняла бы его всем")
	}
	if своё.IsDefault {
		t.Fatal("своё задание источника объявлено общим умолчанием")
	}
	// Копия, а не пустое: задание, написанное с нуля, модель прочтёт как
	// «делай что хочешь».
	if своё.SystemMd != общее.SystemMd || своё.UserMd != общее.UserMd {
		t.Fatalf("своё задание заведено не копией общего: %+v", своё)
	}

	// Правим своё и смотрим, кто что берёт.
	if _, err := prompts.Save(ctx, Prompt{
		ID: своё.ID, Name: своё.Name, SystemMd: "Для этого приказа особо.",
		UserMd: своё.UserMd, Model: "дешёвая/быстрая", Revision: своё.Revision,
	}, "проверка"); err != nil {
		t.Fatal(err)
	}

	взято, err := prompts.ForNode(ctx, NodeCompose, свой)
	if err != nil {
		t.Fatal(err)
	}
	if взято.ID != своё.ID || взято.SourceID != свой {
		t.Fatalf("источник со своим заданием взял чужое: %+v", взято)
	}
	if взято.Model != "дешёвая/быстрая" {
		t.Fatalf("модель своего задания потеряна: %q", взято.Model)
	}

	// А соседний источник по-прежнему на общем, и общее не тронуто.
	соседнее, err := prompts.ForNode(ctx, NodeCompose, чужой)
	if err != nil {
		t.Fatal(err)
	}
	if соседнее.ID != общее.ID {
		t.Fatalf("правка для одного источника досталась соседнему: %+v", соседнее)
	}
	if strings.Contains(соседнее.SystemMd, "особо") {
		t.Fatalf("общее задание изменено правкой для источника:\n%s", соседнее.SystemMd)
	}
	// И другие узлы того же источника остались на общем: своё заводится
	// узлу, а не конвейеру целиком.
	сверка, err := prompts.ForNode(ctx, NodeVerify, свой)
	if err != nil {
		t.Fatal(err)
	}
	if сверка.SourceID != 0 {
		t.Fatalf("своё задание одного узла досталось соседнему: %+v", сверка)
	}
}

func TestPgПовторноеЗаведениеСвоегоЗаданияНеТеряетПравку(t *testing.T) {
	// Нажали дважды — задание одно. Заводи Fork второе, и правка первого
	// пропала бы молча: сохранение отвечало бы успехом, а задачи писались
	// бы по копии, которую никто не правил.
	ctx := context.Background()
	gate := testGate(t)
	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	sourceID := источник(t, gate)

	первое, err := prompts.Fork(ctx, sourceID, NodeVerify)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prompts.Save(ctx, Prompt{
		ID: первое.ID, Name: первое.Name, SystemMd: "Правка составителя.",
		UserMd: первое.UserMd, Revision: первое.Revision,
	}, "проверка"); err != nil {
		t.Fatal(err)
	}

	второе, err := prompts.Fork(ctx, sourceID, NodeVerify)
	if err != nil {
		t.Fatal(err)
	}
	if второе.ID != первое.ID {
		t.Fatalf("повторное заведение завело второе задание: %q против %q", второе.ID, первое.ID)
	}
	if второе.SystemMd != "Правка составителя." {
		t.Fatalf("повторное заведение затёрло правку:\n%s", второе.SystemMd)
	}
}

func TestPgВозвратКОбщемуНеСтираетПравленое(t *testing.T) {
	// Правка задания — врачебная работа, и стирать её нажатием «вернуть
	// общее» нельзя: передумавший вернётся к ней, а стёртую пришлось бы
	// писать заново.
	ctx := context.Background()
	gate := testGate(t)
	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	sourceID := источник(t, gate)

	своё, err := prompts.Fork(ctx, sourceID, NodeProofread)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prompts.Save(ctx, Prompt{
		ID: своё.ID, Name: своё.Name, SystemMd: "Особая вычитка.",
		UserMd: своё.UserMd, Revision: своё.Revision,
	}, "проверка"); err != nil {
		t.Fatal(err)
	}
	if err := prompts.Unfork(ctx, sourceID, NodeProofread); err != nil {
		t.Fatal(err)
	}

	// Конвейер снова на общем.
	взято, err := prompts.ForNode(ctx, NodeProofread, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if взято.ID == своё.ID {
		t.Fatal("после возврата к общему узел по-прежнему берёт своё задание")
	}

	// А правленое цело и находится тем же нажатием.
	снова, err := prompts.Fork(ctx, sourceID, NodeProofread)
	if err != nil {
		t.Fatal(err)
	}
	if снова.ID != своё.ID || снова.SystemMd != "Особая вычитка." {
		t.Fatalf("правленое задание потеряно возвратом к общему: %+v", снова)
	}
}

func TestPgСвоёЗаданиеБезОбщегоНеЗаводится(t *testing.T) {
	// Копировать нечего, и заводить пустое нельзя: модель на пустое
	// задание ответит чем угодно, и ответ этот будет выглядеть работой.
	ctx := context.Background()
	gate := testGate(t)
	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	sourceID := источник(t, gate)
	if _, err := gate.Exec(ctx,
		`UPDATE prompts SET is_default = FALSE WHERE node = $1`, NodeSiblings); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := gate.Exec(context.Background(),
			`UPDATE prompts SET is_default = TRUE WHERE id = 'siblings-default'`); err != nil {
			t.Fatalf("умолчание различающей сверки не вернулось: %v", err)
		}
	})

	if _, err := prompts.Fork(ctx, sourceID, NodeSiblings); err == nil {
		t.Fatal("своё задание завелось от узла без общего")
	}
	// И источник не назван — тоже отказ: заводить задание некому.
	//
	// Смотрим на СЛОВА отказа, а не на сам отказ: убери сторож — и
	// привязка всё равно не ляжет, о неё споткнётся внешний ключ на
	// таблицу источников. То есть проверка на «err != nil» зеленела бы
	// на снятом стороже, а составитель получал бы вместо внятного
	// «источник не назван» жалобу базы на нарушение связи.
	_, err := prompts.Fork(ctx, 0, NodeCompose)
	if err == nil {
		t.Fatal("своё задание завелось без источника")
	}
	if !strings.Contains(err.Error(), "источник не назван") {
		t.Fatalf("отказ без источника пришёл не от сторожа: %v", err)
	}
}

func TestPgОтвязанноеЗаданиеНеПропадаетИзСписка(t *testing.T) {
	// Задание, которое не служит никому, — это строка базы, о которой
	// нельзя узнать, что она есть. Спрятанная, она всплыла бы у первого
	// же, кто завёл бы своё задание тому же узлу и увидел чужую правку
	// вместо копии общего.
	ctx := context.Background()
	gate := testGate(t)
	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	sourceID := источник(t, gate)
	своё, err := prompts.Fork(ctx, sourceID, NodeArbitrate)
	if err != nil {
		t.Fatal(err)
	}
	if err := prompts.Unfork(ctx, sourceID, NodeArbitrate); err != nil {
		t.Fatal(err)
	}

	list, err := prompts.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, one := range list {
		if one.ID != своё.ID {
			continue
		}
		if one.SourceID != 0 {
			t.Fatalf("отвязанное задание числится за источником %d", one.SourceID)
		}
		if one.IsDefault {
			t.Fatal("отвязанное задание объявлено общим умолчанием")
		}
		return
	}
	t.Fatal("отвязанное задание пропало из списка")
}

// конвейерИсточника читает конвейер студии для одного источника.
func конвейерИсточника(t *testing.T, srv *httptest.Server, token string, sourceID int64) map[string]any {
	t.Helper()
	url := srv.URL + "/admin/api/pipeline"
	if sourceID > 0 {
		url += fmt.Sprintf("?source=%d", sourceID)
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("конвейер источника отдан кодом %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// вызвать зовёт ручку и отдаёт код ВМЕСТЕ с отказом.
//
// Тело нужно проверке, а не отладке: «не 200» сходится у отказа по
// незнакомому узлу и у отказа «копировать нечего», а это разные беды и
// разные починки. Проверка, смотрящая на один код, зеленеет и тогда,
// когда сторож узла снят, — отказ приходит следующим по дороге.
func вызвать(t *testing.T, method, url, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, fmt.Sprint(body["error"])
}

func TestPgКонвейерИсточникаПоказываетЧтоСвоёАЧтоОбщее(t *testing.T) {
	// Экран и исполнитель обязаны отбирать задание одним правилом.
	// Разойдись они — составитель правил бы задание, мимо которого задачи
	// не ходят, и узнал бы об этом по качеству задач через неделю.
	srv, token := newDeskWith(t, каталогМоделей, nil, studio.PermPrompts)
	gate := testGate(t)
	sourceID := источник(t, gate)

	// Пока своего нет — все узлы общие.
	body := конвейерИсточника(t, srv, token, sourceID)
	for _, one := range body["nodes"].([]any) {
		row, _ := one.(map[string]any)
		if row["own"] != false {
			t.Fatalf("до правки узел %v объявлен своим: %v", row["node"], row)
		}
	}

	url := fmt.Sprintf("%s/admin/api/sources/%d/pipeline/%s", srv.URL, sourceID, NodeCompose)
	if code, отказ := вызвать(t, http.MethodPost, url, token); code != http.StatusOK {
		t.Fatalf("своё задание не заведено: код %d, %s", code, отказ)
	}

	body = конвейерИсточника(t, srv, token, sourceID)
	свои := 0
	for _, one := range body["nodes"].([]any) {
		row, _ := one.(map[string]any)
		if row["own"] == true {
			свои++
			if row["node"] != NodeCompose {
				t.Fatalf("своим объявлен не тот узел: %v", row)
			}
		}
	}
	if свои != 1 {
		t.Fatalf("своих узлов %d вместо одного", свои)
	}

	// Общий конвейер при этом не тронут: своё задание источника не
	// должно всплывать там, где его никто не заводил.
	общий := конвейерИсточника(t, srv, token, 0)
	for _, one := range общий["nodes"].([]any) {
		row, _ := one.(map[string]any)
		if row["own"] == true {
			t.Fatalf("в общем конвейере узел объявлен своим: %v", row)
		}
		if row["node"] == NodeCompose && strings.Contains(fmt.Sprint(row["promptId"]), "источник") {
			t.Fatalf("в общий конвейер уехало задание источника: %v", row)
		}
	}

	// Возврат к общему виден там же.
	if code, отказ := вызвать(t, http.MethodDelete, url, token); code != http.StatusOK {
		t.Fatalf("возврат к общему отказал кодом %d: %s", code, отказ)
	}
	body = конвейерИсточника(t, srv, token, sourceID)
	for _, one := range body["nodes"].([]any) {
		row, _ := one.(map[string]any)
		if row["own"] == true {
			t.Fatalf("после возврата узел остался своим: %v", row)
		}
	}
}

func TestPgСвоёЗаданиеНезнакомогоУзлаНеЗаводится(t *testing.T) {
	// Словарь узлов закрыт, и незнакомое имя — опечатка в адресе, а не
	// новый узел. Заведи мы задание по нему, оно осталось бы в базе
	// навсегда, не работая никогда, и нашлось бы при разборе чужой беды.
	srv, token := newDeskWith(t, каталогМоделей, nil, studio.PermPrompts)
	gate := testGate(t)
	sourceID := источник(t, gate)
	url := fmt.Sprintf("%s/admin/api/sources/%d/pipeline/%s", srv.URL, sourceID, "compose-опечатка")
	// Смотрим на ОТКАЗ, а не на «не 200»: сними сторож словаря — и
	// опечатка всё равно отказала бы, но уже глубже, словами «копировать
	// нечего». Проверка на один код зеленела бы на снятом стороже.
	code, отказ := вызвать(t, http.MethodPost, url, token)
	if code == http.StatusOK {
		t.Fatal("задание завелось по незнакомому узлу")
	}
	if !strings.Contains(отказ, "узла в конвейере нет") {
		t.Fatalf("опечатку в узле отклонил не сторож словаря: код %d, %s", code, отказ)
	}
}

func TestPgПравкаКонвейераЗакрытаПравомЗаданий(t *testing.T) {
	srv, token := newDeskWith(t, каталогМоделей, nil)
	gate := testGate(t)
	sourceID := источник(t, gate)
	url := fmt.Sprintf("%s/admin/api/sources/%d/pipeline/%s", srv.URL, sourceID, NodeCompose)
	if code, _ := вызвать(t, http.MethodPost, url, token); code == http.StatusOK {
		t.Fatal("своё задание завелось без права «задания»")
	}
	if code, _ := вызвать(t, http.MethodDelete, url, token); code == http.StatusOK {
		t.Fatal("возврат к общему сработал без права «задания»")
	}
}

func TestPgСвоёЗаданиеНеВытесняетсяОбщимКакБыНиЛёгСписок(t *testing.T) {
	// Старшинство «своё выше общего» решается отбором, а не порядком
	// выдачи списка. Список идёт по узлу и по идентификатору, и у
	// заведённого студией задания имя начинается с узла и слова
	// «источник» — то есть ложится ПОСЛЕ умолчания, и оберег от
	// вытеснения не срабатывает ни разу. Задание с именем поменьше
	// ложится ПЕРЕД ним, и тогда умолчание вытесняет своё: экран зовёт
	// править общее задание, а задачи идут по своему.
	//
	// Имя здесь пишется руками намеренно: через студию такого не завести,
	// а строку в базе — можно, и заводили (переименование, ввоз, правка
	// руками).
	srv, token := newDeskWith(t, каталогМоделей, nil, studio.PermPrompts)
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)

	const раньшеУмолчания = "compose-a-своё" // «a» латинская: меньше «d» из compose-default
	if _, err := gate.Exec(ctx,
		`INSERT INTO prompts (id, name, node, system_md, user_md, is_default)
		 VALUES ($1, 'Своё написание', $2, 'Своё.', 'Своё.', FALSE)
		 ON CONFLICT (id) DO NOTHING`, раньшеУмолчания, NodeCompose); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx,
		`INSERT INTO source_prompts (source_id, node, prompt_id) VALUES ($1, $2, $3)`,
		sourceID, NodeCompose, раньшеУмолчания); err != nil {
		t.Fatal(err)
	}

	// Экран обязан показать своё.
	body := конвейерИсточника(t, srv, token, sourceID)
	for _, one := range body["nodes"].([]any) {
		row, _ := one.(map[string]any)
		if row["node"] != NodeCompose {
			continue
		}
		if row["promptId"] != раньшеУмолчания || row["own"] != true {
			t.Fatalf("общее вытеснило своё задание источника: %v", row)
		}
	}
	// И исполнитель обязан взять то же самое: разойдись они — правили бы
	// одно, а задачи шли бы по другому.
	взято, err := NewPrompts(gate).ForNode(ctx, NodeCompose, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if взято.ID != раньшеУмолчания {
		t.Fatalf("исполнитель взял не своё задание источника: %+v", взято)
	}
}
