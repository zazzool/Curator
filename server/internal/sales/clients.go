package sales

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Клиент глазами оператора.
//
// # Зачем это заведено
//
// Приход оформляется на номер учётной записи, а взять этот номер было
// негде: карточка клиента и его права читались по номеру, который никто
// не выдавал. Оператор, которому врач написал с почты, не мог ни найти
// его, ни убедиться, что нашёл того самого, — то есть вся продажа
// упиралась в число, известное только базе.
//
// # Почему учётная запись не удаляется
//
// Блокировка ставит отметку, а не стирает строку: на учётную запись
// ссылаются платежи, права и попытки, и удаление порвало бы
// разбирательство о деньгах ровно тогда, когда оно понадобится.

// Clients — учётные записи в базе, какими их видит студия.
type Clients struct {
	gate *dbgate.Gate
}

func NewClients(gate *dbgate.Gate) *Clients { return &Clients{gate: gate} }

// Client — строка списка и она же карточка.
//
// Одна строка на оба случая намеренно: оператор ищет человека и тут же
// решает, тот ли он, — и решает по тому же, что показано в списке.
type Client struct {
	ID          int64
	Email       string
	DisplayName string
	CreatedAt   time.Time
	LastSeen    *time.Time
	Blocked     bool
	Devices     int
	Rights      int
}

// Find ищет клиента по почте, имени или номеру.
//
// Пустой запрос — не отказ, а последние заведённые: на свежей установке
// оператору нужен именно список, а искать ему пока не по чему.
func (c *Clients) Find(ctx context.Context, query string, limit int) ([]Client, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query = strings.TrimSpace(query)

	// Запрос из одних цифр — это номер, и ищется он ТОЛЬКО точным
	// равенством. Поиск номера подстрокой нашёл бы на «7» ещё и 17-го, и
	// 70-го, а оператор, которому врач назвал свой номер, ждёт одну
	// строку.
	//
	// Подстрока при этом не добавляется вторым доводом, и это не мелочь:
	// цифры живут и в почте (ivanov1985@…), и в имени, так что «найти по
	// номеру ИЛИ по подстроке» возвращает номер плюс всех, у кого эти
	// цифры где-нибудь встретились. Сторож поймал это ровно так: поиск по
	// номеру нашёл две записи вместо одной. Цену размена называем вслух:
	// по куску почты, состоящему из одних цифр, искать теперь нельзя —
	// оператор назовёт хоть одну букву или собаку, и поиск снова станет
	// подстрочным.
	var byID int64
	if n, err := strconv.ParseInt(query, 10, 64); err == nil && n > 0 {
		byID = n
	}

	rows, err := c.gate.Query(ctx, `
		SELECT a.id, coalesce(a.email, ''), a.display_name, a.created_at, a.last_seen,
		       a.blocked_at IS NOT NULL,
		       (SELECT count(*) FROM devices d WHERE d.account_id = a.id),
		       (SELECT count(*) FROM entitlements e
		         WHERE e.account_id = a.id AND e.revoked_at IS NULL
		           AND (e.expires_at IS NULL OR e.expires_at > NOW()))
		  FROM accounts a
		 WHERE $1 = ''
		    OR ($2 > 0 AND a.id = $2)
		    OR ($2 = 0 AND (coalesce(a.email, '') ILIKE '%' || $1 || '%'
		                 OR a.display_name ILIKE '%' || $1 || '%'))
		 ORDER BY a.id DESC
		 LIMIT $3`, query, byID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Пустой список — [], а не null: ненайденный клиент это исправный
	// случай, и он же единственный на свежей установке.
	out := []Client{}
	for rows.Next() {
		var one Client
		if err := rows.Scan(&one.ID, &one.Email, &one.DisplayName, &one.CreatedAt,
			&one.LastSeen, &one.Blocked, &one.Devices, &one.Rights); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

// One отдаёт одну учётную запись.
func (c *Clients) One(ctx context.Context, id int64) (Client, error) {
	var one Client
	err := c.gate.QueryRow(ctx, `
		SELECT a.id, coalesce(a.email, ''), a.display_name, a.created_at, a.last_seen,
		       a.blocked_at IS NOT NULL,
		       (SELECT count(*) FROM devices d WHERE d.account_id = a.id),
		       (SELECT count(*) FROM entitlements e
		         WHERE e.account_id = a.id AND e.revoked_at IS NULL
		           AND (e.expires_at IS NULL OR e.expires_at > NOW()))
		  FROM accounts a WHERE a.id = $1`, id).
		Scan(&one.ID, &one.Email, &one.DisplayName, &one.CreatedAt, &one.LastSeen,
			&one.Blocked, &one.Devices, &one.Rights)
	if err == pgx.ErrNoRows {
		return Client{}, errors.New("такой учётной записи нет")
	}
	if err != nil {
		return Client{}, err
	}
	return one, nil
}

// SetBlocked закрывает или открывает вход учётной записи.
//
// Отметка, а не удаление строки, и проверяется она на входе устройства:
// заблокированный перестаёт получать задачи, но его платежи, права и
// разборы остаются на месте — иначе первое же разбирательство о деньгах
// упёрлось бы в пустоту.
//
// Прав блокировка не отзывает намеренно. Это разные решения: закрыть вход
// можно на время разбирательства, а отозвать оплаченное право — значит
// вернуть деньги, и делается это возвратом платежа.
func (c *Clients) SetBlocked(ctx context.Context, id int64, blocked bool, now time.Time) error {
	var at any
	if blocked {
		at = now
	}
	tag, err := c.gate.Exec(ctx, `UPDATE accounts SET blocked_at = $2 WHERE id = $1`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("такой учётной записи нет")
	}
	return nil
}
