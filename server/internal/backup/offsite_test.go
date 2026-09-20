package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// принятое — что доехало до хранилища.
type принятое struct {
	method  string
	path    string
	headers http.Header
	body    []byte
}

// хранилище поднимает поддельное S3 на петле.
//
// Настоящим сервером, а не подделкой клиента: подпись считается по тому,
// что уехало на самом деле, и поддельный клиент подделал бы её вместе с
// ошибкой.
func хранилище(t *testing.T, status int, say string) (*httptest.Server, *принятое) {
	t.Helper()
	out := &принятое{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		out.method, out.path, out.headers, out.body = r.Method, r.URL.Path, r.Header.Clone(), body
		w.WriteHeader(status)
		_, _ = w.Write([]byte(say))
	}))
	t.Cleanup(srv.Close)
	return srv, out
}

func вывоз(t *testing.T, srv *httptest.Server) *Offsite {
	t.Helper()
	return &Offsite{
		Endpoint: srv.URL,
		Bucket:   "curator-snimki",
		Region:   "ru-1",
		KeyID:    "KEYID",
		Secret:   "SECRET",
		SealKey:  bytes.Repeat([]byte{7}, 32),
		Client:   srv.Client(),
		Now: func() time.Time {
			return time.Date(2026, 9, 20, 12, 45, 0, 0, time.UTC)
		},
	}
}

// Ключ подписи сходится с примером из документации AWS.
//
// Единственное место наряда, где можно ошибиться молча: неверная подпись
// даёт отказ хранилища, а не тихую беду, — но проверить её без настоящего
// хранилища больше нечем. Числа взяты из примера AWS, а не посчитаны этим
// же кодом: иначе проверка сверяла бы код сам с собой.
func TestКлючПодписиСходитсяСПримеромAWS(t *testing.T) {
	got := hex.EncodeToString(signingKey(
		"wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20150830", "us-east-1", "iam"))
	const want = "c4afb1cc5771d871763a393e44b703571b55cc28424d1a5e86da6ed3c154a4b9"
	if got != want {
		t.Fatalf("ключ подписи %s вместо %s", got, want)
	}
}

// Доехавшее в хранилище — запечатанный снимок, а не сам снимок.
//
// Ради этого всё и написано: хранилище не должно мочь прочесть то, что
// хранит.
func TestВХранилищеУезжаетЗапечатанное(t *testing.T) {
	srv, got := хранилище(t, http.StatusOK, "")
	o := вывоз(t, srv)

	const снимок = "PGDMP задачи, разметка, разборы врачей"
	if err := o.Put(context.Background(), "curator-20260920.dump", strings.NewReader(снимок)); err != nil {
		t.Fatalf("не вывезлось: %v", err)
	}

	if bytes.Contains(got.body, []byte("задачи")) {
		t.Fatal("снимок уехал в хранилище открытым текстом")
	}
	// И распечатывается обратно: вывезти нечитаемое навсегда — то же
	// самое, что не вывезти.
	var back bytes.Buffer
	if err := Open(&back, bytes.NewReader(got.body), o.SealKey); err != nil {
		t.Fatalf("вывезенное не распечаталось: %v", err)
	}
	if back.String() != снимок {
		t.Fatalf("распечаталось %q", back.String())
	}
}

// Кладётся туда, куда просили, и тем способом.
func TestКладётсяПутёмВНазванноеВедро(t *testing.T) {
	srv, got := хранилище(t, http.StatusOK, "")
	if err := вывоз(t, srv).Put(context.Background(), "curator-20260920.dump",
		strings.NewReader("снимок")); err != nil {
		t.Fatalf("не вывезлось: %v", err)
	}

	if got.method != http.MethodPut {
		t.Errorf("способ %s вместо PUT", got.method)
	}
	// Путём, а не именем поддомена: имя поддомена требует своей записи
	// DNS и своего сертификата, и на своём хранилище его чаще всего нет.
	if got.path != "/curator-snimki/curator-20260920.dump" {
		t.Errorf("положено по адресу %q", got.path)
	}
}

// Подписано ровно то, что уехало.
//
// Самая частая беда этой подписи: подписали одно тело, отправили другое.
// Хранилище ответит отказом, и искать причину будут в правах.
func TestПодписаноТоЖеТело(t *testing.T) {
	srv, got := хранилище(t, http.StatusOK, "")
	o := вывоз(t, srv)
	if err := o.Put(context.Background(), "снимок с пробелом.dump",
		strings.NewReader(strings.Repeat("данные", 10000))); err != nil {
		t.Fatalf("не вывезлось: %v", err)
	}

	sum := sha256.Sum256(got.body)
	if got.headers.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) {
		t.Fatal("подписан отпечаток не того тела, которое уехало")
	}

	auth := got.headers.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("доверие названо как %q", auth)
	}
	// День в строке доверия и час в заголовке обязаны сойтись: разошлись
	// — и подпись негодна на границе суток, то есть примерно никогда, и
	// потому незаметно.
	if !strings.Contains(auth, "Credential=KEYID/20260920/ru-1/s3/aws4_request") {
		t.Fatalf("строка доверия собрана не так: %q", auth)
	}
	if got.headers.Get("X-Amz-Date") != "20260920T124500Z" {
		t.Fatalf("час подписи %q", got.headers.Get("X-Amz-Date"))
	}
	// Названные в подписи заголовки обязаны быть теми, что уехали.
	for _, name := range []string{"X-Amz-Content-Sha256", "X-Amz-Date"} {
		if got.headers.Get(name) == "" {
			t.Errorf("заголовок %s назван в подписи, но не уехал", name)
		}
	}
}

// Пробел в имени кодируется как %20, а не как плюс.
//
// url.QueryEscape делает наоборот, и подпись разошлась бы ровно на именах
// с пробелом — то есть на части имён, а не на всех.
func TestПробелВИмениКодируетсяКакВПодписи(t *testing.T) {
	if got := escapePath("/ведро/снимок от 20 сентября.dump"); strings.Contains(got, "+") {
		t.Fatalf("пробел уехал плюсом: %s", got)
	}
	if got := escapePath("/a/b/c"); got != "/a/b/c" {
		t.Fatalf("косые черты закодированы: %s", got)
	}
}

// Отказ хранилища доносится словами хранилища.
func TestОтказХранилищаДоноситсяСловами(t *testing.T) {
	srv, _ := хранилище(t, http.StatusForbidden,
		"<Error><Code>SignatureDoesNotMatch</Code></Error>")
	err := вывоз(t, srv).Put(context.Background(), "снимок.dump", strings.NewReader("тело"))
	if err == nil {
		t.Fatal("отказ хранилища прошёл как удача")
	}
	// Голый код состояния не даёт понять, дело в праве, в имени ведра или
	// в часах, разошедшихся с хранилищем.
	if !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Fatalf("отказ ничего не объясняет: %v", err)
	}
}

// Ненастроенный вывоз отказывает, а не вывозит незапечатанное.
func TestНенастроенныйВывозОтказывает(t *testing.T) {
	srv, got := хранилище(t, http.StatusOK, "")

	полный := вывоз(t, srv)
	for name, ломаем := range map[string]func(*Offsite){
		"нет адреса":          func(o *Offsite) { o.Endpoint = "" },
		"нет ведра":           func(o *Offsite) { o.Bucket = "" },
		"нет ключа доступа":   func(o *Offsite) { o.KeyID = "" },
		"нет тайны":           func(o *Offsite) { o.Secret = "" },
		"нет ключа шифра":     func(o *Offsite) { o.SealKey = nil },
		"короткий ключ шифра": func(o *Offsite) { o.SealKey = bytes.Repeat([]byte{7}, 31) },
	} {
		один := *полный
		ломаем(&один)
		if один.Ready() {
			t.Errorf("%s: вывоз объявил себя настроенным", name)
		}
		if err := один.Put(context.Background(), "снимок.dump",
			strings.NewReader("тайна")); err == nil {
			t.Errorf("%s: вывезлось", name)
		}
	}
	if got.body != nil {
		t.Fatal("ненастроенный вывоз что-то отправил в хранилище")
	}
}

// Настройка из окружения: пусто — вывоза нет, половина — отказ.
func TestНастройкаВывозаИзОкружения(t *testing.T) {
	полная := map[string]string{
		"CURATOR_OFFSITE_ENDPOINT": "https://s3.example",
		"CURATOR_OFFSITE_BUCKET":   "curator-snimki",
		"CURATOR_OFFSITE_REGION":   "ru-1",
		"CURATOR_OFFSITE_KEY_ID":   "KEYID",
		"CURATOR_OFFSITE_SECRET":   "SECRET",
		"CURATOR_OFFSITE_SEAL_KEY": base64.StdEncoding.EncodeToString(
			bytes.Repeat([]byte{7}, 32)),
	}
	из := func(m map[string]string) func(string) string {
		return func(name string) string { return m[name] }
	}

	got, err := FromEnv(из(полная))
	if err != nil {
		t.Fatalf("полная настройка не принята: %v", err)
	}
	if !got.Ready() {
		t.Fatal("полная настройка не объявила себя готовой")
	}

	// Пусто — не отказ: вывоз необязателен.
	got, err = FromEnv(из(map[string]string{}))
	if err != nil {
		t.Fatalf("пустая настройка дала отказ: %v", err)
	}
	if got.Ready() {
		t.Fatal("пустая настройка объявила себя готовой")
	}

	// А вот наполовину заполненная — отказ. Настройка, выглядящая
	// заведённой и не работающая, узнаётся в тот единственный день, когда
	// снимок нужен.
	for _, name := range []string{
		"CURATOR_OFFSITE_ENDPOINT", "CURATOR_OFFSITE_BUCKET",
		"CURATOR_OFFSITE_KEY_ID", "CURATOR_OFFSITE_SECRET",
		"CURATOR_OFFSITE_SEAL_KEY",
	} {
		неполная := map[string]string{}
		for k, v := range полная {
			if k != name {
				неполная[k] = v
			}
		}
		if _, err := FromEnv(из(неполная)); err == nil {
			t.Errorf("без %s настройка принята", name)
		}
	}
}

// Негодный ключ шифрования — отказ, а не вывоз без шифрования.
func TestНегодныйКлючШифрованияОтказывает(t *testing.T) {
	основа := map[string]string{
		"CURATOR_OFFSITE_ENDPOINT": "https://s3.example",
		"CURATOR_OFFSITE_BUCKET":   "curator-snimki",
		"CURATOR_OFFSITE_KEY_ID":   "KEYID",
		"CURATOR_OFFSITE_SECRET":   "SECRET",
	}
	for name, raw := range map[string]string{
		"не base64":   "это не база64!!",
		"короткий":    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16)),
		"длинный":     base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 64)),
		"почти тот":   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 31)),
		"пустой ключ": base64.StdEncoding.EncodeToString(nil),
	} {
		env := map[string]string{"CURATOR_OFFSITE_SEAL_KEY": raw}
		for k, v := range основа {
			env[k] = v
		}
		got, err := FromEnv(func(n string) string { return env[n] })
		if err == nil {
			t.Errorf("%s: принят", name)
		}
		if got.Ready() {
			t.Errorf("%s: вывоз объявил себя готовым", name)
		}
	}
}
