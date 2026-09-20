// Единственная дверь к базе.
//
// Пул спрятан здесь и наружу не выдаётся: у типа, который получает
// вызывающий, нет способа добраться до *pgxpool.Pool. Это не соглашение
// «ходите через шлюз», а невозможность пройти мимо — соглашение забывают,
// компилятор не забывает.
//
// # Зачем дверь, если организаций нет
//
// В доноре у двери была вторая работа, главная: она объявляла организацию
// первой командой каждой транзакции, и запрос без объявления обязан был
// отказать. В Кураторе организаций нет, и этой работы у двери тоже нет —
// поэтому сказать, зачем она осталась, надо честно:
//
//   - один пул на процесс. Второй пул — это второй набор соединений, вторые
//     таймауты и вторая точка, где кончаются соединения; ловится он только
//     обходом дерева, и ловить его проще, когда пройти мимо нельзя;
//   - одно место для сроков. Запрос без срока висит до перезапуска, и
//     висеть он будет как раз под нагрузкой. Довод этот полгода был
//     обещанием: срока не ставил ни один из четырёх методов, и нашёл это
//     аудит, а не отказ. Теперь срок ставится здесь и только здесь;
//   - одно место для журнала медленных запросов. Запрос, ставший медленным,
//     узнаётся по журналу, а не по жалобе.
package dbgate

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Gate — дверь к базе. Пул неэкспортируемый и остаётся таким.
type Gate struct {
	pool *pgxpool.Pool

	// slow — с какой длительности запрос попадает в журнал. Ноль выключает
	// журнал совсем.
	slow time.Duration

	// timeout — срок на один запрос. Ноль снимает срок совсем.
	timeout time.Duration
}

// Options — чем отличается одна дверь от другой.
//
// Полями, а не двумя соседними Duration в списке доводов: перепутанные
// местами порог журнала и срок запроса компилятор не заметит, и служба
// поднимется с сроком в полсекунды и журналом от тридцати секунд.
type Options struct {
	// Slow — с какой длительности запрос попадает в журнал. Ноль
	// выключает журнал.
	Slow time.Duration

	// Timeout — потолок на один запрос, а не умолчание: свой срок
	// вызывающего работает, пока он короче. Ноль снимает потолок, и это
	// не недосмотр, а нужный случай: накат схемы, ввоз старой базы и
	// разбор каталога идут минутами, и потолок в полминуты оборвал бы
	// выкатку посередине. Потолок нужен там, где запрос ждёт человек.
	Timeout time.Duration
}

// Open открывает пул и проверяет, что база отвечает.
//
// Проверка обязательна: pgxpool соединяется лениво, и без неё процесс
// поднялся бы с негодной строкой подключения и упал бы на первом запросе —
// то есть у человека, а не в журнале запуска.
func Open(ctx context.Context, dsn string, opts Options) (*Gate, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("строка подключения не разобрана: %w", err)
	}

	// Тот же срок объявляется и базе. Срок в контексте обрывает ожидание
	// у нас, но запрос при этом продолжает идти на сервере и держать
	// соединение: отменённый контекст рвёт соединение, а не запрос.
	// statement_timeout останавливает его там, где он работает.
	//
	// Уже названное в строке подключения не перебивается: строка — это
	// решение того, кто запускает, и молча переписанное решение потом
	// ищут часами.
	if opts.Timeout > 0 {
		if _, ok := config.ConnConfig.RuntimeParams["statement_timeout"]; !ok {
			config.ConnConfig.RuntimeParams["statement_timeout"] =
				strconv.FormatInt(opts.Timeout.Milliseconds(), 10)
		}
	}

	// Соединение не живёт вечно. Прокси и сама база рвут долгие
	// соединения со своей стороны, и рвут молча: пул отдаёт работе
	// соединение, о котором ещё не знает, что оно мёртвое. Свой срок
	// жизни короче их — тогда закрывает пул, а не они.
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("пул соединений не создан: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("база не отвечает: %w", err)
	}
	return &Gate{pool: pool, slow: opts.Slow, timeout: opts.Timeout}, nil
}

// Ping спрашивает базу, жива ли она.
//
// Отдельным именем, а не через Query: этим ответом меряется готовность
// службы, и мерить её запросом к таблице — значит однажды признать
// службу неготовой из-за опечатки в запросе.
func (g *Gate) Ping(ctx context.Context) error {
	ctx, cancel := g.deadline(ctx)
	defer cancel()
	return g.pool.Ping(ctx)
}

// deadline навешивает срок на запрос.
//
// Срок двери — ПОТОЛОК, а не умолчание, и это разные вещи. Умолчание
// вызывающий переспорил бы своим сроком подлиннее, а потолок — нет, и так
// и надо: statement_timeout объявлен соединению, и про контекст
// вызывающего база ничего не знает. Позволь здесь чужой срок длиннее
// потолка — и запрос всё равно оборвался бы базой, но уже необъяснимо:
// вызывающий видел бы отказ на пятой секунде, объявив себе пятьдесят.
// Обещание, которого нижний слой не держит, хуже прямого запрета.
//
// Свой срок короче потолка при этом работает: WithTimeout не отодвигает
// уже назначенный срок, а оставляет ближний.
//
// Кому нужен срок длиннее — тому нужна своя дверь с другим потолком, и
// разовые работы (накат, ввоз, разбор каталога) открывают её с нулевым.
func (g *Gate) deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if g.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, g.timeout)
}

// Close закрывает пул.
func (g *Gate) Close() {
	if g != nil && g.pool != nil {
		g.pool.Close()
	}
}

// Exec выполняет команду.
func (g *Gate) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	ctx, cancel := g.deadline(ctx)
	defer cancel()
	defer g.watch(time.Now(), sql)
	return g.pool.Exec(ctx, sql, args...)
}

// Query выполняет запрос со многими строками.
//
// Отмена срока переезжает в Close, и это не украшение. Строки читаются
// ПОСЛЕ возврата из Query: поставь здесь обычное `defer cancel()`, и срок
// снимется раньше первого Next — то есть чтение оборвётся на исправном
// запросе, и оборвётся ровно на большом ответе, который читается дольше.
func (g *Gate) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	ctx, cancel := g.deadline(ctx)
	defer g.watch(time.Now(), sql)
	rows, err := g.pool.Query(ctx, sql, args...)
	if err != nil {
		cancel()
		return nil, err
	}
	return timedRows{Rows: rows, cancel: cancel}, nil
}

// QueryRow выполняет запрос об одной строке.
//
// Тот же случай, что у Query: значение читается вызовом Scan, уже после
// возврата отсюда.
func (g *Gate) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	ctx, cancel := g.deadline(ctx)
	defer g.watch(time.Now(), sql)
	return timedRow{row: g.pool.QueryRow(ctx, sql, args...), cancel: cancel}
}

// timedRows держат отмену срока до закрытия набора.
type timedRows struct {
	pgx.Rows
	cancel context.CancelFunc
}

func (s timedRows) Close() {
	s.Rows.Close()
	s.cancel()
}

// timedRow держит отмену срока до чтения значения.
//
// Не позвавший Scan держит срок до его истечения, а не навсегда: тем
// временем и ограничен здесь ущерб от забытой отмены.
type timedRow struct {
	row    pgx.Row
	cancel context.CancelFunc
}

func (s timedRow) Scan(dest ...any) error {
	defer s.cancel()
	return s.row.Scan(dest...)
}

// InTx выполняет работу в транзакции, откатывая её при любом отказе.
//
// Откат идёт через defer, а не в конце функции: работа может вернуть
// отказ из середины, а может и запаниковать, и транзакция, оставшаяся
// открытой, держит соединение до таймаута сервера.
func (g *Gate) InTx(ctx context.Context, work func(pgx.Tx) error) error {
	ctx, cancel := g.deadline(ctx)
	defer cancel()
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("транзакция не начата: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := work(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// watch пишет в журнал запрос, шедший дольше порога.
//
// Текст запроса режется: журнал читают глазами, а схема с комментариями
// приезжает сюда целиком, если позвать накат через эту же дверь.
func (g *Gate) watch(started time.Time, sql string) {
	if g.slow <= 0 {
		return
	}
	took := time.Since(started)
	if took < g.slow {
		return
	}
	short := sql
	if len(short) > 120 {
		short = short[:120] + "…"
	}
	log.Printf("медленный запрос (%s): %s", took.Round(time.Millisecond), short)
}
