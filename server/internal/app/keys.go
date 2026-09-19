package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Ключ программы: чем сборка называет себя серверу.
//
// Ключ не удостоверяет человека и не даёт доступа к чужим данным — он
// отвечает на вопрос «какая сборка стучится». Нужен он затем, чтобы
// заведение устройства не было открыто всякому: первый же любопытный
// завёл бы нам миллион учётных записей, и отличить их от настоящих было
// бы нечем.
//
// Ключ лежит в сборке приложения, то есть в руках у всякого, кто её
// разобрал. Это известно и принято: он останавливает случайное, а не
// целенаправленное. Всё, что стоит денег или открывает чужое, закрыто
// токеном устройства, а не ключом.

// Keys — ключи программ.
type Keys struct {
	gate *dbgate.Gate
}

func NewKeys(gate *dbgate.Gate) *Keys { return &Keys{gate: gate} }

// ErrNoKey — ключ не подошёл.
var ErrNoKey = errors.New("ключ программы не подошёл")

// Check сверяет ключ и отдаёт его номер.
//
// Ключ приходит как «номер.секрет»: по номеру ищем строку, секрет
// сверяем отпечатком. Искать по отпечатку секрета было бы короче, но
// тогда смена секрета у ключа требовала бы нового номера, а номер стоит
// в журналах и отчётах.
func (k *Keys) Check(ctx context.Context, raw string) (string, error) {
	keyID, secret, ok := strings.Cut(strings.TrimSpace(raw), ".")
	if !ok || keyID == "" || secret == "" {
		return "", ErrNoKey
	}

	var hash string
	var disabled *string
	err := k.gate.QueryRow(ctx,
		`SELECT secret_hash, disabled_at::text FROM app_keys WHERE key_id = $1`,
		keyID).Scan(&hash, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoKey
	}
	if err != nil {
		return "", fmt.Errorf("ключ программы не прочитан: %w", err)
	}
	if disabled != nil {
		return "", ErrNoKey
	}
	// Сравнение за постоянное время: обычное сравнение строк отвечает тем
	// быстрее, чем раньше разошлись байты, и по времени ответа секрет
	// подбирается по знаку за раз.
	if subtle.ConstantTimeCompare([]byte(hash), []byte(fingerprint(secret))) != 1 {
		return "", ErrNoKey
	}
	return keyID, nil
}

// Issue заводит ключ программы и отдаёт его целиком.
//
// Целиком — единственный раз в жизни ключа: дальше в базе лежит только
// отпечаток. Потерявший ключ заводит новый, а не «смотрит старый»;
// возможность посмотреть означала бы, что утёкшая база даёт ключи.
func (k *Keys) Issue(ctx context.Context, keyID, title string) (string, error) {
	if strings.TrimSpace(keyID) == "" || strings.Contains(keyID, ".") {
		// Точка — разделитель в самом ключе. Номер с точкой разобрался бы
		// не тем местом, и ключ молча перестал бы подходить.
		return "", errors.New("номер ключа пуст или содержит точку")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("секрет ключа не выдан: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)

	// ON CONFLICT DO NOTHING, а не отказ базы наружу: отказ базы приезжает
	// человеку строкой вида «duplicate key value violates unique constraint
	// "app_keys_pkey" (SQLSTATE 23505)», и по ней не понять ни что
	// случилось, ни что делать. Повтор номера — обычная человеческая
	// ошибка, и говорить о ней надо словами.
	//
	// Именно DO NOTHING, а не DO UPDATE: перезапись подменила бы отпечаток
	// живого ключа, и все сборки, ходящие со старым, отказали бы разом —
	// молча и не у нас, а на руках у врачей.
	tag, err := k.gate.Exec(ctx,
		`INSERT INTO app_keys (key_id, secret_hash, title) VALUES ($1, $2, $3)
		 ON CONFLICT (key_id) DO NOTHING`,
		keyID, fingerprint(secret), title)
	if err != nil {
		return "", fmt.Errorf("ключ программы %q не заведён: %w", keyID, err)
	}
	if tag.RowsAffected() == 0 {
		return "", fmt.Errorf("ключ программы с номером %q уже заведён: возьмите другой номер или отключите прежний", keyID)
	}
	return keyID + "." + secret, nil
}

// Key — ключ программы, каким его видит студия. Секрета в нём нет: он не
// хранится, и показать его повторно нельзя ни при каком желании.
type Key struct {
	KeyID     string
	Title     string
	Disabled  bool
	CreatedAt time.Time
}

// All отдаёт заведённые ключи.
func (k *Keys) All(ctx context.Context) ([]Key, error) {
	rows, err := k.gate.Query(ctx,
		`SELECT key_id, title, disabled_at IS NOT NULL, created_at
		   FROM app_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("ключи программ не прочитаны: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: студия ходит по нему циклом, и на
	// свежей установке, где ключей ещё нет, nil уехал бы наружу как null.
	out := []Key{}
	for rows.Next() {
		var one Key
		if err := rows.Scan(&one.KeyID, &one.Title, &one.Disabled, &one.CreatedAt); err != nil {
			return nil, fmt.Errorf("ключ не прочитан: %w", err)
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ключи не дочитаны: %w", err)
	}
	return out, nil
}

// Disable выключает ключ.
//
// Строка не удаляется: ключ стоит в журналах обращений, и удалённый
// оставил бы их ссылающимися в никуда.
func (k *Keys) Disable(ctx context.Context, keyID string) error {
	_, err := k.gate.Exec(ctx,
		`UPDATE app_keys SET disabled_at = NOW() WHERE key_id = $1 AND disabled_at IS NULL`,
		keyID)
	if err != nil {
		return fmt.Errorf("ключ программы %q не выключен: %w", keyID, err)
	}
	return nil
}
