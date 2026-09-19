package studio

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"curator/server/internal/dbgate"
)

// SessionTTL — сколько живёт сессия студии.
//
// Сутки: составитель работает днями подряд, и вход, истекающий за час,
// люди обходят — оставляют вкладку открытой или заводят второй способ
// входа. Срок, который обходят, не защищает.
const SessionTTL = 24 * time.Hour

// Sessions — сессии студии.
//
// Токен хранится отпечатком, а не текстом: утёкшая база не должна давать
// входа. По той же причине сравнение идёт по отпечатку — искать в базе по
// самому токену значило бы его туда положить.
type Sessions struct {
	gate *dbgate.Gate
}

// NewSessions собирает хранилище на готовой двери.
func NewSessions(gate *dbgate.Gate) *Sessions { return &Sessions{gate: gate} }

// Issue выдаёт сессию и возвращает токен. Токен виден один раз — дальше
// живёт только его отпечаток.
func (s *Sessions) Issue(ctx context.Context, userID int64, now time.Time) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = s.gate.Exec(ctx,
		`INSERT INTO admin_sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hashToken(token), userID, now.Add(SessionTTL))
	if err != nil {
		return "", fmt.Errorf("сессия не выдана: %w", err)
	}
	return token, nil
}

// User возвращает пользователя действующей сессии.
//
// Истёкшая сессия — это отказ, а не пустой пользователь: пустой пользователь
// прошёл бы дальше и получил бы ответ без прав, а такой ответ читается как
// «ничего нет», а не как «войдите заново».
func (s *Sessions) User(ctx context.Context, users *Users, token string, now time.Time) (User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return User{}, errors.New("сессия не предъявлена")
	}
	var (
		login   string
		expires time.Time
	)
	err := s.gate.QueryRow(ctx,
		`SELECT u.login, s.expires_at
		   FROM admin_sessions s
		   JOIN users u ON u.id = s.user_id
		  WHERE s.token_hash = $1`, hashToken(token)).Scan(&login, &expires)
	if err != nil {
		return User{}, errors.New("сессия не найдена")
	}
	if !expires.After(now) {
		return User{}, errors.New("сессия истекла")
	}
	user, _, err := users.ByLogin(ctx, login)
	if err != nil {
		return User{}, err
	}
	if user.Disabled {
		// Отключённый пользователь не ходит по выданной ранее сессии:
		// иначе увольнение вступало бы в силу через сутки.
		return User{}, errors.New("вход отключён")
	}
	return user, nil
}

// Close закрывает сессию.
func (s *Sessions) Close(ctx context.Context, token string) error {
	_, err := s.gate.Exec(ctx, `DELETE FROM admin_sessions WHERE token_hash = $1`, hashToken(token))
	return err
}

// Sweep убирает истёкшие сессии.
//
// Нужен не ради места, а ради того, чтобы таблица сессий не превращалась в
// журнал входов, которого никто не заводил.
func (s *Sessions) Sweep(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.gate.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// newToken порождает непрозрачный токен.
func newToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("токен не порождён: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// hashToken — отпечаток токена.
//
// Простой SHA-256 без соли и растяжения, и это осознанно: токен — тридцать
// два случайных байта, а не пароль человека. Перебирать здесь нечего, и
// растяжение платило бы временем на каждом обращении студии.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
