package backup

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Проверки идут на настоящей базе и настоящим pg_dump.
//
// Поддельный запуск проверил бы только то, что мы позвали программу с
// теми доводами, которые сами и написали. Снимок при этом мог бы не
// разворачиваться — а узнают об этом в тот единственный день, когда он
// нужен.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут. " +
			"Это отказ, а не пропуск — см. server/.env.example")
	}
	return dsn
}

// втораяБаза заводит отдельную базу под разворачивание и убирает её.
//
// Отдельная — не придирка: разворачивание очищает таблицы перед вставкой,
// и проверка, взявшая ту же базу, стёрла бы данные остальных проверок,
// идущих рядом.
func втораяБаза(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	live := testDSN(t)

	name := fmt.Sprintf("curator_snap_%d_%d", time.Now().UnixNano(), rand.IntN(1000))

	admin, err := pgx.Connect(ctx, live)
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatalf("вторая база не заведена: %v", err)
	}
	t.Cleanup(func() {
		clean, err := pgx.Connect(context.Background(), live)
		if err != nil {
			return
		}
		defer clean.Close(context.Background())
		_, _ = clean.Exec(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})
	return подменитьБазу(live, name)
}

// подменитьБазу меняет имя базы в строке подключения, не трогая прочего.
func подменитьБазу(dsn, name string) string {
	cut := strings.LastIndex(dsn, "/")
	tail := ""
	if q := strings.Index(dsn[cut:], "?"); q >= 0 {
		tail = dsn[cut:][q:]
	}
	return dsn[:cut+1] + name + tail
}

func хранитель(t *testing.T) *Keeper {
	t.Helper()
	return &Keeper{DSN: testDSN(t), Dir: t.TempDir()}
}

func TestPgСнимокРазворачиваетсяИСходитсяПоСтрокам(t *testing.T) {
	// Главная проверка: снимок, который никто не восстанавливал, — не
	// снимок. Файл может быть непустым, читаемым и негодным.
	k := хранитель(t)
	k.VerifyDSN = втораяБаза(t)

	snap, err := k.Take(context.Background())
	if err != nil {
		t.Fatalf("снимок не снят: %v", err)
	}
	if snap.Bytes == 0 {
		t.Fatal("снимок пуст")
	}
	if err := k.Verify(context.Background(), snap.Path); err != nil {
		t.Fatalf("снимок не сошёлся: %v", err)
	}
}

// свояТаблица заводит таблицу только для этой проверки и убирает её.
//
// Своя — не придирка: проверки разных пакетов идут ОДНОВРЕМЕННО по одной
// базе, и чужие строки, приехавшие посреди съёмки, объявили бы отказом
// исправный снимок.
func свояТаблица(t *testing.T, dsn string) string {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("снимок_%d_%d", time.Now().UnixNano(), rand.IntN(1000))

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx,
		`CREATE TABLE "`+name+`" (id BIGSERIAL PRIMARY KEY, что TEXT)`); err != nil {
		t.Fatalf("своя таблица не заведена: %v", err)
	}
	t.Cleanup(func() {
		clean, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			return
		}
		defer clean.Close(context.Background())
		_, _ = clean.Exec(context.Background(), `DROP TABLE IF EXISTS "`+name+`"`)
	})
	return name
}

func выполни(t *testing.T, dsn, sql string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestPgСнимокБезНовойТаблицыНеПроходитСверку(t *testing.T) {
	// Ровно то, ради чего сверка и заведена: схема выросла, снимок о новой
	// таблице не знает, а снимок при этом получается — непустой, читаемый
	// и негодный. Выяснилось бы это в тот единственный день, когда он
	// нужен.
	ctx := context.Background()
	k := хранитель(t)
	k.VerifyDSN = втораяБаза(t)

	snap, err := k.Take(ctx)
	if err != nil {
		t.Fatalf("снимок не снят: %v", err)
	}
	// Таблица появляется ПОСЛЕ съёмки — в снимке её нет.
	name := свояТаблица(t, k.DSN)

	err = k.Verify(ctx, snap.Path)
	if err == nil {
		t.Fatal("сверка приняла снимок, в котором нет целой таблицы")
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("отказ не называет пропавшую таблицу: %v", err)
	}
}

func TestPgСнимокСхемыБезДанныхНеПроходитСверку(t *testing.T) {
	// Второй настоящий отказ: таблица развернулась, а строк в ней нет.
	ctx := context.Background()
	k := хранитель(t)
	k.VerifyDSN = втораяБаза(t)

	name := свояТаблица(t, k.DSN)
	snap, err := k.Take(ctx)
	if err != nil {
		t.Fatalf("снимок не снят: %v", err)
	}
	// Строки появляются ПОСЛЕ съёмки: в снимке таблица есть, а пуста.
	выполни(t, k.DSN, `INSERT INTO "`+name+`" (что) VALUES ('после снимка')`)

	err = k.Verify(ctx, snap.Path)
	if err == nil {
		t.Fatal("сверка приняла снимок, развернувший схему без данных")
	}
	if !strings.Contains(err.Error(), "ни одной") {
		t.Fatalf("отказ не называет, чего не хватает: %v", err)
	}
}

func TestPgРастущаяБазаНеОбъявляетИсправныйСнимокНегодным(t *testing.T) {
	// Живая база растёт всё время, пока идут съёмка и разворачивание:
	// врач решает задачу, приложение шлёт телеметрию. Поштучная сверка
	// кричала бы на каждый такой снимок, то есть на каждый, — а проверка,
	// кричащая всегда, перестаёт читаться, и настоящий отказ пройдёт
	// вместе с ложными.
	ctx := context.Background()
	k := хранитель(t)
	k.VerifyDSN = втораяБаза(t)

	name := свояТаблица(t, k.DSN)
	выполни(t, k.DSN, `INSERT INTO "`+name+`" (что) VALUES ('до снимка')`)

	snap, err := k.Take(ctx)
	if err != nil {
		t.Fatalf("снимок не снят: %v", err)
	}
	// Дописываем столько же, сколько было: строк в живой базе вдвое
	// больше, чем в снимке, и это исправный случай.
	выполни(t, k.DSN, `INSERT INTO "`+name+`" (что) VALUES ('после снимка')`)

	if err := k.Verify(ctx, snap.Path); err != nil {
		t.Fatalf("исправный снимок объявлен негодным из-за роста базы: %v", err)
	}
}

func TestPgОборванныйСнимокНеПроходитСверку(t *testing.T) {
	// pg_restore по умолчанию ДОКЛАДЫВАЕТ об ошибках и продолжает: без
	// --exit-on-error он возвращает успех, восстановив половину. Снимок
	// при этом выглядит развёрнутым.
	ctx := context.Background()
	k := хранитель(t)
	k.VerifyDSN = втораяБаза(t)

	snap, err := k.Take(ctx)
	if err != nil {
		t.Fatalf("снимок не снят: %v", err)
	}
	if err := os.Truncate(snap.Path, snap.Bytes/2); err != nil {
		t.Fatal(err)
	}

	if err := k.Verify(ctx, snap.Path); err == nil {
		t.Fatal("сверка приняла оборванный снимок")
	}
}

func TestPgСверкаОтказываетКогдаБазаПроверкиСовпадаетСЖивой(t *testing.T) {
	// Разворачивание очищает таблицы перед вставкой. Совпади базы —
	// защита стёрла бы ровно то, что стережёт.
	k := хранитель(t)
	k.VerifyDSN = k.DSN

	err := k.Verify(context.Background(), filepath.Join(k.Dir, "нет.dump"))
	if err == nil {
		t.Fatal("сверка согласилась развернуть снимок в живую базу")
	}
	if !strings.Contains(err.Error(), "живой") {
		t.Fatalf("отказ не называет причину: %v", err)
	}
}

func TestPgСнимокВНедоступныйКаталогОтказывает(t *testing.T) {
	// Молчаливый успех здесь означал бы, что снимков нет, а журнал
	// говорит, что они есть.
	k := хранитель(t)
	k.Dir = filepath.Join(k.Dir, "файл", "дальше")
	if err := os.WriteFile(filepath.Dir(k.Dir), []byte("не каталог"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Take(context.Background()); err == nil {
		t.Fatal("снимок в несуществующий каталог прошёл")
	}
}

func TestНенастроенныеСнимкиНеВыдаютСебяЗаНастроенные(t *testing.T) {
	// Пустой каталог обязан выглядеть как отсутствие защиты, а не как
	// защита: заполненная переменная выглядит как заведённая защита.
	if (&Keeper{DSN: "postgres://x/y"}).Ready() {
		t.Error("снимки без каталога назвались настроенными")
	}
	if (&Keeper{Dir: "/tmp"}).Ready() {
		t.Error("снимки без строки подключения назвались настроенными")
	}
	if _, err := (&Keeper{}).Take(context.Background()); err == nil {
		t.Error("ненастроенные снимки сняли снимок")
	}
}

func TestСтарыеСнимкиУбираютсяПоИмени(t *testing.T) {
	// По имени, а не по времени файла: копия каталога получает время
	// копирования, и самым свежим стал бы самый старый.
	dir := t.TempDir()
	for _, name := range []string{
		"curator-20260101-000000.dump",
		"curator-20260103-000000.dump",
		"curator-20260102-000000.dump",
		"не-снимок.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	k := &Keeper{Dir: dir, Keep: 2}
	removed, err := k.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("убрано %d снимков, а лишний был один", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "curator-20260101-000000.dump")); !os.IsNotExist(err) {
		t.Error("убрали не самый старый")
	}
	for _, оставшийся := range []string{
		"curator-20260102-000000.dump", "curator-20260103-000000.dump", "не-снимок.txt",
	} {
		if _, err := os.Stat(filepath.Join(dir, оставшийся)); err != nil {
			t.Errorf("убрали лишнее: %s", оставшийся)
		}
	}
}

func TestНулевоеЧислоСнимковНеОзначаетСтеретьВсё(t *testing.T) {
	// Ноль в этом поле чаще всего незаполненная переменная, и понимать её
	// как «стереть всё» нельзя: защита, стирающая себя от пустой строки в
	// .env, хуже отсутствия защиты.
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("curator-2026010%d-000000.dump", i+1)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := (&Keeper{Dir: dir}).Prune()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("с незаданным числом снимков убрано %d", removed)
	}
}
