package backup

import (
	"context"
	"fmt"
	"log"
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

// Съёмка идёт со СВОЕЙ базы, а не с общей проверочной.
//
// Это исправление красного сторожа, и цена промаха тут наглядна. Проверки
// разных пакетов идут ОДНОВРЕМЕННО по одной базе, и снимать её посреди
// чужой работы нельзя двояко. Во-первых, чужие строки, приехавшие после
// съёмки, делают исправный снимок «неполным» — на этом и покраснел
// сторож. Во-вторых, пакет схемы переименовывает и сносит public целыми
// схемами, и pg_dump, читавший её в ту же секунду, получил взаимную
// блокировку и оборванный COPY — отказ, которого в коде снимков нет
// вовсе.
//
// Своя база снимает оба случая разом: в неё не пишет никто, кроме самой
// проверки.
var (
	своя   string // откуда снимаем
	чужая  string // куда разворачиваем
	готовы bool
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn != "" {
		// Без строки подключения ничего не заводим и НЕ выходим: проверки
		// на живой базе обязаны отказать поимённо, а не пропасть из
		// отчёта. Отказ называет каждая из них сама (testDSN).
		своя = заведиБазу(dsn, "curator_snapsrc")
		чужая = заведиБазу(dsn, "curator_snapdst")
		готовы = true
	}
	code := m.Run()
	if готовы {
		убериБазу(dsn, своя)
		убериБазу(dsn, чужая)
	}
	os.Exit(code)
}

func заведиБазу(admin, prefix string) string {
	name := fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), rand.IntN(1000))
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		log.Fatalf("проверочная база недоступна: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		log.Fatalf("база под снимки не заведена: %v", err)
	}
	return подменитьБазу(admin, name)
}

func убериБазу(admin, dsn string) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return
	}
	defer conn.Close(ctx)
	cut := strings.LastIndex(dsn, "/") + 1
	name := dsn[cut:]
	if q := strings.Index(name, "?"); q >= 0 {
		name = name[:q]
	}
	_, _ = conn.Exec(ctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
}

// втораяБаза отдаёт базу под разворачивание.
//
// Отдельная от снимаемой — не придирка: разворачивание очищает таблицы
// перед вставкой, и проверка, взявшая ту же базу, стёрла бы то, что
// стережёт.
func втораяБаза(t *testing.T) string {
	t.Helper()
	testDSN(t)
	return чужая
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
	testDSN(t)
	return &Keeper{DSN: своя, Dir: t.TempDir()}
}

func TestPgСнимокРазворачиваетсяИСходитсяПоСтрокам(t *testing.T) {
	// Главная проверка: снимок, который никто не восстанавливал, — не
	// снимок. Файл может быть непустым, читаемым и негодным.
	ctx := context.Background()
	k := хранитель(t)
	k.VerifyDSN = втораяБаза(t)

	name := свояТаблица(t, k.DSN)
	выполни(t, k.DSN, `INSERT INTO "`+name+`" (что) VALUES ('строка снимка')`)

	snap, err := k.Take(ctx)
	if err != nil {
		t.Fatalf("снимок не снят: %v", err)
	}
	if snap.Bytes == 0 {
		t.Fatal("снимок пуст")
	}
	if err := k.Verify(ctx, snap.Path); err != nil {
		t.Fatalf("снимок не сошёлся: %v", err)
	}

	// Сверка сверкой, а строку смотрим глазами: «развернулось без отказа»
	// и «данные на месте» — разные утверждения, и второе здесь главное.
	вернулось := прочти(t, k.VerifyDSN, `SELECT что FROM "`+name+`"`)
	if len(вернулось) != 1 || вернулось[0] != "строка снимка" {
		t.Fatalf("из снимка вернулось %q, а клали одну строку", вернулось)
	}
}

// свояТаблица заводит таблицу только для этой проверки и убирает её.
//
// База у пакета своя, но проверок в ней несколько, и идут они одна за
// другой. Таблица, оставшаяся от предыдущей, задала бы следующей условия,
// которых та не заказывала, — а разбирать такое пришлось бы по чужому
// отказу.
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

// прочти отдаёт один столбец одним списком.
func прочти(t *testing.T, dsn, sql string) []string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var one string
		if err := rows.Scan(&one); err != nil {
			t.Fatal(err)
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
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

	// Вторая таблица в час съёмки ПУСТА и наполняется после. Ровно на
	// этом случае и покраснел сторож: прежняя сверка смотрела на каждую
	// таблицу отдельно и объявляла такую «не развернувшейся». У нас таких
	// таблиц полно — email_codes пуст, пока никто не привязывает почту, —
	// и первая же привязка после ночного снимка давала бы ложный отказ.
	пустая := свояТаблица(t, k.DSN)

	snap, err := k.Take(ctx)
	if err != nil {
		t.Fatalf("снимок не снят: %v", err)
	}
	// Дописываем столько же, сколько было: строк в живой базе вдвое
	// больше, чем в снимке, и это исправный случай.
	выполни(t, k.DSN, `INSERT INTO "`+name+`" (что) VALUES ('после снимка')`)
	выполни(t, k.DSN, `INSERT INTO "`+пустая+`" (что) VALUES ('первая за всё время')`)

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

	// Со строками: обрывать пустой снимок нечестно — он и целиком-то
	// состоит из заголовка, и отказ получился бы не про обрыв.
	name := свояТаблица(t, k.DSN)
	выполни(t, k.DSN, `INSERT INTO "`+name+`" (что) VALUES ('строка подлиннее, чтобы было что рубить')`)

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
