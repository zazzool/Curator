package dbgate

import (
	"context"

	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
	"time"
)

// открыть заводит дверь к проверочной базе.
//
// Без строки подключения — отказ, а не пропуск: проверка, которая молча
// пропускается и возвращает успех, выдаёт зелёное за непроверенное.
func открыть(t *testing.T, opts Options) *Gate {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут. " +
			"Это отказ, а не пропуск — см. server/.env.example")
	}
	gate, err := Open(context.Background(), dsn, opts)
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

// Запрос сверх срока обрывается, а не висит.
func TestPgСрокОбрываетДолгийЗапрос(t *testing.T) {
	gate := открыть(t, Options{Timeout: 300 * time.Millisecond})

	начало := time.Now()
	var единица int
	err := gate.QueryRow(context.Background(), "SELECT 1 FROM pg_sleep(5)").Scan(&единица)
	if err == nil {
		t.Fatal("запрос на пять секунд прошёл при сроке в треть секунды")
	}
	// Срок обязан сработать примерно тогда, когда объявлен: сработавший
	// через пять секунд неотличим от отсутствующего.
	if ушло := time.Since(начало); ушло > 3*time.Second {
		t.Fatalf("оборвалось через %v — это не срок, а конец самого запроса", ушло)
	}
}

// Без срока запрос идёт столько, сколько нужно.
//
// Нулевой срок — не недосмотр: накат схемы, ввоз старой базы и разбор
// каталога идут минутами, и срок в полминуты оборвал бы выкатку.
func TestPgБезСрокаДолгийЗапросПроходит(t *testing.T) {
	gate := открыть(t, Options{})

	var единица int
	if err := gate.QueryRow(context.Background(),
		"SELECT 1 FROM pg_sleep(1.5)").Scan(&единица); err != nil {
		t.Fatalf("запрос без срока оборвался: %v", err)
	}
}

// Свой срок короче потолка работает.
func TestPgСвойСрокКорочеПотолкаРаботает(t *testing.T) {
	gate := открыть(t, Options{Timeout: 10 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	начало := time.Now()
	var единица int
	if err := gate.QueryRow(ctx, "SELECT 1 FROM pg_sleep(5)").Scan(&единица); err == nil {
		t.Fatal("свой срок в треть секунды не сработал при потолке в десять")
	}
	if ушло := time.Since(начало); ушло > 3*time.Second {
		t.Fatalf("оборвалось через %v — сработал потолок, а не свой срок", ушло)
	}
}

// Свой срок ДЛИННЕЕ потолка потолка не отодвигает.
//
// Проверка стоит потому, что здесь легко пообещать лишнее. Срок в
// контексте вызывающий назначает себе сам, а statement_timeout объявлен
// соединению, и про контекст база не знает: пропусти дверь чужой срок в
// пятьдесят секунд — и запрос всё равно оборвался бы базой на тридцатой,
// но уже необъяснимо для того, кто читает свой код.
func TestPgСвойСрокДлиннееПотолкаЕгоНеОтодвигает(t *testing.T) {
	gate := открыть(t, Options{Timeout: 300 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	начало := time.Now()
	var единица int
	if err := gate.QueryRow(ctx, "SELECT 1 FROM pg_sleep(5)").Scan(&единица); err == nil {
		t.Fatal("свой срок подлиннее отодвинул потолок двери")
	}
	if ушло := time.Since(начало); ушло > 3*time.Second {
		t.Fatalf("оборвалось через %v — потолок не сработал", ушло)
	}
}

// Строки читаются после возврата из Query.
//
// Ловушка, на которой чинится этот дефект: набор читается ПОСЛЕ возврата
// из Query, и обычное `defer cancel()` снимает срок раньше первого Next.
//
// Ответ здесь нарочно большой, и это главное в проверке. На маленьком она
// проходит и на сломанном: полтысячи чисел приезжают одним чтением и
// разбираются из буфера, не трогая контекст вовсе. Правда видна только
// там, где ответ не помещается в одно чтение, — то есть на большой
// странице у врача, а не в проверке. Замерено: на десяти мегабайтах
// сломанное чтение обрывается на 56-й строке из двадцати тысяч.
func TestPgБольшойОтветЧитаетсяПослеВозвратаИзQuery(t *testing.T) {
	gate := открыть(t, Options{Timeout: 30 * time.Second})

	const сколькоЖдём = 20000
	rows, err := gate.Query(context.Background(),
		"SELECT repeat('я', 500) FROM generate_series(1, $1)", сколькоЖдём)
	if err != nil {
		t.Fatalf("запрос не пошёл: %v", err)
	}
	defer rows.Close()

	var сколько int
	for rows.Next() {
		var строка string
		if err := rows.Scan(&строка); err != nil {
			t.Fatalf("строка %d не прочиталась: %v", сколько+1, err)
		}
		сколько++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("чтение оборвалось на %d-й строке из %d: %v", сколько, сколькоЖдём, err)
	}
	if сколько != сколькоЖдём {
		t.Fatalf("прочитано %d строк из %d", сколько, сколькоЖдём)
	}
}

// Значение читается после возврата из QueryRow.
//
// Тот же случай, что у Query: Scan зовут уже после возврата отсюда.
func TestPgЗначениеЧитаетсяПослеВозвратаИзQueryRow(t *testing.T) {
	gate := открыть(t, Options{Timeout: 10 * time.Second})

	// Значение большое по той же причине, что и ответ выше: маленькое
	// приезжает одним чтением, и сломанное Scan прочитало бы его из
	// буфера как ни в чём не бывало.
	row := gate.QueryRow(context.Background(), "SELECT repeat('я', 8000000)")
	// Между возвратом и чтением проходит время: так и бывает у
	// вызывающего, который сперва разбирается, что делать с ответом.
	time.Sleep(50 * time.Millisecond)

	var ответ string
	if err := row.Scan(&ответ); err != nil {
		t.Fatalf("значение не прочиталось после возврата: %v", err)
	}
	if len([]rune(ответ)) != 8000000 {
		t.Fatalf("прочитано %d знаков из восьми миллионов", len([]rune(ответ)))
	}
}

// Срок объявляется и самой базе.
//
// Срок в контексте обрывает ожидание у нас, а запрос продолжает идти на
// сервере и держать соединение. statement_timeout останавливает его там,
// где он работает.
func TestPgСрокОбъявленБазе(t *testing.T) {
	gate := открыть(t, Options{Timeout: 7 * time.Second})

	var объявлено string
	if err := gate.QueryRow(context.Background(),
		"SHOW statement_timeout").Scan(&объявлено); err != nil {
		t.Fatalf("statement_timeout не спрошен: %v", err)
	}
	if объявлено != "7s" {
		t.Fatalf("база знает срок как %q, а дверь объявляла семь секунд", объявлено)
	}
}

// Названное в строке подключения дверь не перебивает.
//
// Строка подключения — решение того, кто запускает, и молча переписанное
// решение потом ищут часами.
func TestPgНазванныйВСтрокеСрокНеПеребивается(t *testing.T) {
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут")
	}
	разделитель := "&"
	if !strings.Contains(dsn, "?") {
		разделитель = "?"
	}

	gate, err := Open(context.Background(), dsn+разделитель+"statement_timeout=11000",
		Options{Timeout: 7 * time.Second})
	if err != nil {
		t.Fatalf("дверь не открылась: %v", err)
	}
	defer gate.Close()

	var объявлено string
	if err := gate.QueryRow(context.Background(),
		"SHOW statement_timeout").Scan(&объявлено); err != nil {
		t.Fatalf("statement_timeout не спрошен: %v", err)
	}
	if объявлено != "11s" {
		t.Fatalf("названные в строке одиннадцать секунд стали %q", объявлено)
	}
}

// Срок стоит и на транзакции.
func TestPgСрокОбрываетДолгуюТранзакцию(t *testing.T) {
	gate := открыть(t, Options{Timeout: 300 * time.Millisecond})

	начало := time.Now()
	err := gate.InTx(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "SELECT pg_sleep(5)")
		return err
	})
	if err == nil {
		t.Fatal("транзакция на пять секунд прошла при сроке в треть секунды")
	}
	if ушло := time.Since(начало); ушло > 3*time.Second {
		t.Fatalf("оборвалось через %v — это не срок", ушло)
	}
}

// Живая база отвечает на опрос готовности.
func TestPgОпросГотовностиОтвечаетНаЖивойБазе(t *testing.T) {
	gate := открыть(t, Options{Timeout: 5 * time.Second})

	if err := gate.Ping(context.Background()); err != nil {
		t.Fatalf("живая база не ответила на опрос: %v", err)
	}
}

// Опрос закрытой двери отказывает, а не висит.
//
// Это и есть случай, ради которого ручка готовности заведена: база,
// отпавшая после подъёма.
func TestPgОпросЗакрытойДвериОтказывает(t *testing.T) {
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут")
	}
	gate, err := Open(context.Background(), dsn, Options{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("дверь не открылась: %v", err)
	}
	gate.Close()

	начало := time.Now()
	if err := gate.Ping(context.Background()); err == nil {
		t.Fatal("закрытая дверь ответила, что база готова")
	}
	if ушло := time.Since(начало); ушло > 5*time.Second {
		t.Fatalf("опрос висел %v: готовность узнавалась бы позже прокси", ушло)
	}
}

// Потолок считается верно, и считается без базы.
//
// Проверка стоит отдельно от проверок на живой базе потому, что там её
// подменяет statement_timeout: долгий запрос оборвёт база, и сроки в
// контексте можно снять целиком, не уронив ни одной. А нужны они как раз
// там, где база не обрывает ничего, — когда ответа нет вовсе: портал
// чужого Wi-Fi, оборванное соединение, отпавший прокси.
func TestПотолокСчитается(t *testing.T) {
	теперь := time.Now()

	// Без потолка срок не появляется из ниоткуда.
	пустая := &Gate{}
	ctx, cancel := пустая.deadline(context.Background())
	defer cancel()
	if _, есть := ctx.Deadline(); есть {
		t.Error("дверь без потолка назначила срок")
	}

	gate := &Gate{timeout: 10 * time.Second}

	// Своего срока нет — берётся потолок.
	ctx, cancel = gate.deadline(context.Background())
	defer cancel()
	срок, есть := ctx.Deadline()
	if !есть {
		t.Fatal("срок не назначен вовсе")
	}
	if осталось := срок.Sub(теперь); осталось < 9*time.Second || осталось > 11*time.Second {
		t.Errorf("до срока %v вместо десяти секунд", осталось)
	}

	// Свой срок короче потолка остаётся своим.
	свой, отмена := context.WithTimeout(context.Background(), time.Second)
	defer отмена()
	ctx, cancel = gate.deadline(свой)
	defer cancel()
	срок, _ = ctx.Deadline()
	if осталось := срок.Sub(теперь); осталось > 2*time.Second {
		t.Errorf("свой срок в секунду стал %v: потолок отодвинул ближний срок", осталось)
	}

	// Свой срок длиннее потолка потолка не отодвигает.
	длинный, отмена2 := context.WithTimeout(context.Background(), time.Minute)
	defer отмена2()
	ctx, cancel = gate.deadline(длинный)
	defer cancel()
	срок, _ = ctx.Deadline()
	if осталось := срок.Sub(теперь); осталось > 11*time.Second {
		t.Errorf("до срока %v: свой срок в минуту отодвинул потолок в десять секунд", осталось)
	}
}
