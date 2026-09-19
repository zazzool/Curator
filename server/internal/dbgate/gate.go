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
//     висеть он будет как раз под нагрузкой;
//   - одно место для журнала медленных запросов. Запрос, ставший медленным,
//     узнаётся по журналу, а не по жалобе.
package dbgate

import (
	"context"
	"fmt"
	"log"
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
}

// Open открывает пул и проверяет, что база отвечает.
//
// Проверка обязательна: pgxpool соединяется лениво, и без неё процесс
// поднялся бы с негодной строкой подключения и упал бы на первом запросе —
// то есть у человека, а не в журнале запуска.
func Open(ctx context.Context, dsn string, slow time.Duration) (*Gate, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("пул соединений не создан: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("база не отвечает: %w", err)
	}
	return &Gate{pool: pool, slow: slow}, nil
}

// Close закрывает пул.
func (g *Gate) Close() {
	if g != nil && g.pool != nil {
		g.pool.Close()
	}
}

// Exec выполняет команду.
func (g *Gate) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	defer g.watch(time.Now(), sql)
	return g.pool.Exec(ctx, sql, args...)
}

// Query выполняет запрос со многими строками.
func (g *Gate) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	defer g.watch(time.Now(), sql)
	return g.pool.Query(ctx, sql, args...)
}

// QueryRow выполняет запрос об одной строке.
func (g *Gate) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	defer g.watch(time.Now(), sql)
	return g.pool.QueryRow(ctx, sql, args...)
}

// InTx выполняет работу в транзакции, откатывая её при любом отказе.
//
// Откат идёт через defer, а не в конце функции: работа может вернуть
// отказ из середины, а может и запаниковать, и транзакция, оставшаяся
// открытой, держит соединение до таймаута сервера.
func (g *Gate) InTx(ctx context.Context, work func(pgx.Tx) error) error {
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
