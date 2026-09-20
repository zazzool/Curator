package backup

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Снимок, не прошедший сверку, не вывозится.
//
// Вывезенный негодный файл — это вторая копия того, чему верить нельзя, и
// хуже отсутствия второй копии: она выглядит защитой. Проверка стоит
// потому, что порядок «снял — вывез — сверил» пишется сам собой и
// выглядит разумно.
func TestPgНесошедшийсяСнимокНеВывозится(t *testing.T) {
	принято := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		принято++
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	defer srv.Close()

	k := хранитель(t)
	// База под разворачивание — та же, что снимаемая: своей же защитой
	// Verify такое отвергает, и снимок останется несошедшимся.
	k.VerifyDSN = k.DSN
	k.Offsite = Offsite{
		Endpoint: srv.URL,
		Bucket:   "curator-snimki",
		KeyID:    "KEYID",
		Secret:   "SECRET",
		SealKey:  bytes.Repeat([]byte{7}, 32),
		Client:   srv.Client(),
	}

	k.once(context.Background())

	if принято != 0 {
		t.Fatalf("несошедшийся снимок уехал в хранилище (%d обращений)", принято)
	}
}

// Сошедшийся снимок уезжает, и уезжает запечатанным.
func TestPgСошедшийсяСнимокУезжаетЗапечатанным(t *testing.T) {
	var приехало []byte
	var путь string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		приехало, _ = io.ReadAll(r.Body)
		путь = r.URL.Path
	}))
	defer srv.Close()

	k := хранитель(t)
	k.VerifyDSN = втораяБаза(t)

	// Пустая база сверку не проходит: сверять в ней нечего.
	выполни(t, k.DSN, `INSERT INTO "`+свояТаблица(t, k.DSN)+`" (что) VALUES ('строка')`)
	key := bytes.Repeat([]byte{7}, 32)
	k.Offsite = Offsite{
		Endpoint: srv.URL,
		Bucket:   "curator-snimki",
		KeyID:    "KEYID",
		Secret:   "SECRET",
		SealKey:  key,
		Client:   srv.Client(),
		Now:      func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) },
	}

	k.once(context.Background())

	if len(приехало) == 0 {
		t.Fatal("сошедшийся снимок никуда не уехал")
	}
	// Имя в хранилище — имя файла, и не больше: путь на нашем хосте
	// восстанавливающему не нужен, а хранилищу и подавно.
	if filepath.Dir(путь) != "/curator-snimki" {
		t.Errorf("положено по адресу %q", путь)
	}

	// Распечатывается обратно в тот же файл, что лежит у нас.
	lying, err := os.ReadDir(k.Dir)
	if err != nil || len(lying) == 0 {
		t.Fatalf("снимка нет на диске: %v", err)
	}
	local, err := os.ReadFile(filepath.Join(k.Dir, lying[0].Name()))
	if err != nil {
		t.Fatalf("снимок не прочитан: %v", err)
	}
	var back bytes.Buffer
	if err := Open(&back, bytes.NewReader(приехало), key); err != nil {
		t.Fatalf("вывезенное не распечаталось: %v", err)
	}
	if !bytes.Equal(back.Bytes(), local) {
		t.Fatal("вывезенное распечаталось не в тот снимок, что лежит у нас")
	}
}

// Отказ вывоза не отменяет ни снимка, ни уборки старых.
//
// Отказ вывоза — это «снимок пока в одном месте», а не «снимка нет».
func TestPgОтказВывозаНеОтменяетСнимка(t *testing.T) {
	пробовали := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		пробовали++
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<Error><Code>AccessDenied</Code></Error>"))
	}))
	defer srv.Close()

	k := хранитель(t)
	k.VerifyDSN = втораяБаза(t)

	// Пустая база сверку не проходит: сверять в ней нечего.
	выполни(t, k.DSN, `INSERT INTO "`+свояТаблица(t, k.DSN)+`" (что) VALUES ('строка')`)
	k.Offsite = Offsite{
		Endpoint: srv.URL,
		Bucket:   "curator-snimki",
		KeyID:    "KEYID",
		Secret:   "SECRET",
		SealKey:  bytes.Repeat([]byte{7}, 32),
		Client:   srv.Client(),
	}

	k.once(context.Background())

	// Сперва убеждаемся, что вывоз вообще пробовали: без этого проверка
	// проходит и там, где вывоза не случилось по другой причине, — и
	// проверяет тогда ровно ничего.
	if пробовали == 0 {
		t.Fatal("вывоз не пробовали вовсе: проверять отказ не на чем")
	}
	lying, err := os.ReadDir(k.Dir)
	if err != nil {
		t.Fatalf("каталог снимков не прочитан: %v", err)
	}
	if len(lying) == 0 {
		t.Fatal("отказ вывоза унёс с собой и сам снимок")
	}
}
