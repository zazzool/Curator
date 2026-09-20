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

// почтальон — поддельная отправка.
//
// Живого ящика у проверок нет, и проверять «письмо ушло» по отсутствию
// отказа нельзя: так проходит и ручка, которая не отправляет ничего.
// Поэтому письма складываются сюда и читаются построчно.
type почтальон struct {
	готов  bool
	падает bool
	письма []письмо
}

type письмо struct {
	кому, тема, текст string
}

func (p *почтальон) Ready() bool { return p.готов }

func (p *почтальон) Send(to, subject, body string) error {
	if p.падает {
		return fmt.Errorf("почтовый сервер недоступен")
	}
	p.письма = append(p.письма, письмо{кому: to, тема: subject, текст: body})
	return nil
}

// код достаёт шестизначный код из последнего письма.
func (p *почтальон) код(t *testing.T) string {
	t.Helper()
	if len(p.письма) == 0 {
		t.Fatal("писем не отправлено: кода взять неоткуда")
	}
	text := p.письма[len(p.письма)-1].текст
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, ".,;:")
		if len(field) != 6 {
			continue
		}
		if strings.IndexFunc(field, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
			return field
		}
	}
	t.Fatalf("в письме нет шестизначного кода: %s", text)
	return ""
}

// почтоваяДверь поднимает /v1 вместе с ручками почты.
func почтоваяДверь(t *testing.T) (*httptest.Server, *dbgate.Gate, string, *почтальон) {
	t.Helper()
	gate := testGate(t)
	keys := NewKeys(gate)

	keyID := fmt.Sprintf("почта-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	key, err := keys.Issue(context.Background(), keyID, "Проверка почты")
	if err != nil {
		t.Fatalf("ключ программы не заведён: %v", err)
	}

	accounts := NewAccounts(gate)
	door := NewDoor(keys, accounts)
	Routes(door, NewFeed(gate), NewAttempts(gate, progress.Default()), sales.NewAccess(gate))

	post := &почтальон{готов: true}
	EmailRoutes(door, NewEmails(accounts), post, time.Now)

	srv := httptest.NewServer(door.Handler())
	t.Cleanup(srv.Close)
	return srv, gate, key, post
}

// адрес выдаёт свой адрес каждой проверке: база одна на весь прогон, и
// общий адрес связал бы проверки через account_identities.
func адрес() string {
	return fmt.Sprintf("врач-%d-%d@пример.рф", time.Now().UnixNano(), rand.Intn(100000))
}

func TestPgПочтаПривязываетсяКодомИзПисьма(t *testing.T) {
	srv, _, key, post := почтоваяДверь(t)
	token := устройство(t, srv, key)
	auth := map[string]string{"Authorization": "Bearer " + token}
	mail := адрес()

	status, _, raw := call(t, srv, "POST", "/v1/me/email", auth,
		map[string]any{"email": mail})
	if status != http.StatusAccepted {
		t.Fatalf("код не запрошен, ответ %d: %s", status, raw)
	}
	if len(post.письма) != 1 || post.письма[0].кому != mail {
		t.Fatalf("письмо ушло не тому: %+v", post.письма)
	}

	status, body, raw := call(t, srv, "POST", "/v1/me/email/confirm", auth,
		map[string]any{"code": post.код(t)})
	if status != http.StatusOK {
		t.Fatalf("код не принят, ответ %d: %s", status, raw)
	}
	if body["email"] != mail {
		t.Fatalf("привязан не тот адрес: %s", raw)
	}

	// Привязка видна там же, где врач её ищет.
	status, body, raw = call(t, srv, "GET", "/v1/me", auth, nil)
	if status != http.StatusOK || body["email"] != mail {
		t.Fatalf("привязанной почты нет в /v1/me, ответ %d: %s", status, raw)
	}
}

func TestPgЧужойКодНеПодходит(t *testing.T) {
	srv, _, key, post := почтоваяДверь(t)
	auth := map[string]string{"Authorization": "Bearer " + устройство(t, srv, key)}

	call(t, srv, "POST", "/v1/me/email", auth, map[string]any{"email": адрес()})
	свой := post.код(t)

	// Подбираем заведомо другой код той же длины.
	чужой := "000000"
	if свой == чужой {
		чужой = "111111"
	}
	status, _, raw := call(t, srv, "POST", "/v1/me/email/confirm", auth,
		map[string]any{"code": чужой})
	if status != http.StatusBadRequest {
		t.Fatalf("чужой код принят, ответ %d: %s", status, raw)
	}
}

func TestPgКодСгораетПослеПятойПопытки(t *testing.T) {
	srv, _, key, post := почтоваяДверь(t)
	auth := map[string]string{"Authorization": "Bearer " + устройство(t, srv, key)}

	call(t, srv, "POST", "/v1/me/email", auth, map[string]any{"email": адрес()})
	верный := post.код(t)

	неверный := "000000"
	if верный == неверный {
		неверный = "111111"
	}
	for i := 0; i < codeAttempts; i++ {
		call(t, srv, "POST", "/v1/me/email/confirm", auth,
			map[string]any{"code": неверный})
	}

	// Потолок выбран, и верный код уже не спасает: иначе перебор стоил бы
	// ровно столько же, сколько и без потолка.
	status, _, raw := call(t, srv, "POST", "/v1/me/email/confirm", auth,
		map[string]any{"code": верный})
	if status != http.StatusBadRequest {
		t.Fatalf("сгоревший код принят, ответ %d: %s", status, raw)
	}
}

func TestPgЗанятыйАдресНеВыдаётСебяОтветом(t *testing.T) {
	srv, _, key, post := почтоваяДверь(t)
	mail := адрес()

	// Первый врач привязал адрес.
	первый := map[string]string{"Authorization": "Bearer " + устройство(t, srv, key)}
	call(t, srv, "POST", "/v1/me/email", первый, map[string]any{"email": mail})
	status, _, raw := call(t, srv, "POST", "/v1/me/email/confirm", первый,
		map[string]any{"code": post.код(t)})
	if status != http.StatusOK {
		t.Fatalf("первая привязка не прошла, ответ %d: %s", status, raw)
	}
	отправлено := len(post.письма)

	// Второй просит тот же адрес. Ответ обязан быть тем же самым, иначе
	// ручка превращается в способ узнать, заведён ли такой врач.
	второй := map[string]string{"Authorization": "Bearer " + устройство(t, srv, key)}
	status, _, raw = call(t, srv, "POST", "/v1/me/email", второй,
		map[string]any{"email": mail})
	if status != http.StatusAccepted {
		t.Fatalf("занятый адрес ответил иначе, код %d: %s", status, raw)
	}
	if len(post.письма) != отправлено {
		t.Fatalf("на занятый адрес ушло письмо: %+v", post.письма[отправлено:])
	}
}

func TestPgВозвратДоступаОтдаётТуЖеЗапись(t *testing.T) {
	srv, _, key, post := почтоваяДверь(t)
	auth := map[string]string{"Authorization": "Bearer " + устройство(t, srv, key)}
	mail := адрес()

	_, было, raw := call(t, srv, "GET", "/v1/me", auth, nil)
	старая, ok := было["accountId"].(float64)
	if !ok {
		t.Fatalf("в /v1/me нет номера записи: %s", raw)
	}

	call(t, srv, "POST", "/v1/me/email", auth, map[string]any{"email": mail})
	call(t, srv, "POST", "/v1/me/email/confirm", auth,
		map[string]any{"code": post.код(t)})

	// Телефон сменился: токена нет, есть только ключ программы.
	программа := map[string]string{"X-App-Key": key}
	status, _, raw := call(t, srv, "POST", "/v1/recovery", программа,
		map[string]any{"email": mail})
	if status != http.StatusAccepted {
		t.Fatalf("возврат доступа не начат, ответ %d: %s", status, raw)
	}

	status, body, raw := call(t, srv, "POST", "/v1/recovery/confirm", программа,
		map[string]any{"email": mail, "code": post.код(t), "platform": "android"})
	if status != http.StatusCreated {
		t.Fatalf("возврат доступа не завершён, ответ %d: %s", status, raw)
	}
	новый, _ := body["token"].(string)
	if новый == "" {
		t.Fatalf("возврат доступа без токена: %s", raw)
	}

	_, стало, raw := call(t, srv, "GET", "/v1/me",
		map[string]string{"Authorization": "Bearer " + новый}, nil)
	if стало["accountId"] != старая {
		t.Fatalf("возврат доступа завёл новую запись вместо прежней: %s", raw)
	}

	// Прежнее устройство не выброшено: врач, восстановившийся на планшете,
	// не должен обнаружить, что телефон разлогинился.
	status, _, raw = call(t, srv, "GET", "/v1/me", auth, nil)
	if status != http.StatusOK {
		t.Fatalf("прежнее устройство выброшено, ответ %d: %s", status, raw)
	}
}

func TestPgНезнакомаяПочтаОтвечаетТемЖеСамым(t *testing.T) {
	srv, _, key, post := почтоваяДверь(t)
	программа := map[string]string{"X-App-Key": key}

	status, знакомый, _ := call(t, srv, "POST", "/v1/recovery", программа,
		map[string]any{"email": адрес()})
	if status != http.StatusAccepted {
		t.Fatalf("незнакомая почта ответила отказом, код %d", status)
	}
	if len(post.письма) != 0 {
		t.Fatalf("на незнакомую почту ушло письмо: %+v", post.письма)
	}

	// И даже строка, адресом не являющаяся, отвечает тем же: разбор ответа
	// не должен рассказывать, дошло ли дело до поиска.
	status, мусор, _ := call(t, srv, "POST", "/v1/recovery", программа,
		map[string]any{"email": "не адрес вовсе"})
	if status != http.StatusAccepted || fmt.Sprint(мусор) != fmt.Sprint(знакомый) {
		t.Fatalf("негодный адрес ответил иначе: %d %v против %v", status, мусор, знакомый)
	}
}

func TestPgКодПривязкиНеОткрываетВозвратДоступа(t *testing.T) {
	srv, _, key, post := почтоваяДверь(t)
	auth := map[string]string{"Authorization": "Bearer " + устройство(t, srv, key)}
	mail := адрес()

	// Код привязки уходит на адрес, который ещё никому не принадлежит.
	// Открой он возврат доступа — и любой, кто получил письмо привязки,
	// заводил бы устройство на чужой записи.
	call(t, srv, "POST", "/v1/me/email", auth, map[string]any{"email": mail})

	status, _, raw := call(t, srv, "POST", "/v1/recovery/confirm",
		map[string]string{"X-App-Key": key},
		map[string]any{"email": mail, "code": post.код(t)})
	if status != http.StatusBadRequest {
		t.Fatalf("код привязки открыл возврат доступа, ответ %d: %s", status, raw)
	}
}

func TestPgБезНастроеннойПочтыРучкаОтказываетСловами(t *testing.T) {
	srv, gate, key, post := почтоваяДверь(t)
	post.готов = false
	_ = gate

	auth := map[string]string{"Authorization": "Bearer " + устройство(t, srv, key)}
	status, body, raw := call(t, srv, "POST", "/v1/me/email", auth,
		map[string]any{"email": адрес()})
	// Не 200 с обещанием письма: врач, которому сказали «код отправлен»
	// там, где почты нет, будет ждать его до вечера.
	if status != http.StatusServiceUnavailable {
		t.Fatalf("ненастроенная почта обещала письмо, ответ %d: %s", status, raw)
	}
	if text, _ := body["error"].(string); !strings.Contains(text, "не настроена") {
		t.Fatalf("отказ не называет причину: %s", raw)
	}
}

func TestPgНеушедшееПисьмоНеВыдаётсяЗаОтправленное(t *testing.T) {
	srv, _, key, post := почтоваяДверь(t)
	post.падает = true

	auth := map[string]string{"Authorization": "Bearer " + устройство(t, srv, key)}
	status, _, raw := call(t, srv, "POST", "/v1/me/email", auth,
		map[string]any{"email": адрес()})
	if status != http.StatusBadGateway {
		t.Fatalf("отказ отправки выдан за успех, ответ %d: %s", status, raw)
	}
}

func TestПочтаСводитсяКОдномуВиду(t *testing.T) {
	// Регистр и пробелы снимаются, всё прочее — нет: точки в имени ящика
	// и «плюс-адреса» значимы не у всех поставщиков.
	if got := Normalize("  Doctor@Example.COM "); got != "doctor@example.com" {
		t.Fatalf("адрес сведён неверно: %q", got)
	}
	if got := Normalize("d.o.c+курс@example.com"); got != "d.o.c+курс@example.com" {
		t.Fatalf("адрес изменён сверх регистра: %q", got)
	}

	for _, ok := range []string{"a@b.ru", "врач@пример.рф", "d.o.c+1@mail.example.com"} {
		if !Looks(ok) {
			t.Fatalf("настоящий адрес отвергнут: %q", ok)
		}
	}
	for _, bad := range []string{"", "@b.ru", "a@", "a@b", "a b@c.ru", "a@b.ru\nBcc: x@y.ru"} {
		if Looks(bad) {
			t.Fatalf("негодный адрес принят: %q", bad)
		}
	}
}

// Ответы ручек почты сверяются с записанным эталоном.
//
// Отдельной проверкой, а не глазами при правке: эталон — единственное, что
// держит формат /v1 неизменным, и новая ручка, в него не вписанная,
// расходится с приложением молча.
func TestPgОтветыПочтыСходятсяСЭталоном(t *testing.T) {
	c := loadContract(t)
	srv, _, key, post := почтоваяДверь(t)
	auth := map[string]string{"Authorization": "Bearer " + устройство(t, srv, key)}
	программа := map[string]string{"X-App-Key": key}
	mail := адрес()

	status, body, raw := call(t, srv, "POST", "/v1/me/email", auth,
		map[string]any{"email": mail})
	if want := c.Responses["POST /v1/me/email"].Status; status != want {
		t.Fatalf("POST /v1/me/email ответил %d, эталон обещает %d: %s", status, want, raw)
	}
	matchShape(t, "POST /v1/me/email", body, c.Responses["POST /v1/me/email"].Fields)

	status, body, raw = call(t, srv, "POST", "/v1/me/email/confirm", auth,
		map[string]any{"code": post.код(t)})
	if want := c.Responses["POST /v1/me/email/confirm"].Status; status != want {
		t.Fatalf("подтверждение ответило %d, эталон обещает %d: %s", status, want, raw)
	}
	matchShape(t, "POST /v1/me/email/confirm", body,
		c.Responses["POST /v1/me/email/confirm"].Fields)

	status, body, raw = call(t, srv, "POST", "/v1/recovery", программа,
		map[string]any{"email": mail})
	if want := c.Responses["POST /v1/recovery"].Status; status != want {
		t.Fatalf("POST /v1/recovery ответил %d, эталон обещает %d: %s", status, want, raw)
	}
	matchShape(t, "POST /v1/recovery", body, c.Responses["POST /v1/recovery"].Fields)

	status, body, raw = call(t, srv, "POST", "/v1/recovery/confirm", программа,
		map[string]any{"email": mail, "code": post.код(t), "platform": "android"})
	if want := c.Responses["POST /v1/recovery/confirm"].Status; status != want {
		t.Fatalf("возврат доступа ответил %d, эталон обещает %d: %s", status, want, raw)
	}
	matchShape(t, "POST /v1/recovery/confirm", body,
		c.Responses["POST /v1/recovery/confirm"].Fields)

	// Отказ тоже описан эталоном: приложение показывает врачу именно поле
	// error, и переименуй мы его — врач увидел бы пустое место.
	_, отказ, raw := call(t, srv, "POST", "/v1/me/email/confirm", auth,
		map[string]any{"code": "000000"})
	matchShape(t, "отказ ручки почты", отказ, c.Errors.Fields)
}
