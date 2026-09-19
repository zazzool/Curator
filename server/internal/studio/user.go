package studio

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/totp"
)

// User — пользователь студии.
//
// Вход именованный, а не один на всю установку: пока составитель один, общий
// вход работает, а со вторым человеком это единственная дверь, за которой
// видно всё, и по журналу не сказать, кто что сделал.
type User struct {
	ID          int64
	Login       string
	DisplayName string
	Permissions []Permission
	Disabled    bool
}

// Can говорит, есть ли у пользователя право.
func (u *User) Can(p Permission) bool {
	if u == nil || u.Disabled {
		return false
	}
	for _, own := range u.Permissions {
		if own == p {
			return true
		}
	}
	return false
}

// Users — хранилище пользователей студии.
type Users struct {
	gate *dbgate.Gate
}

// NewUsers собирает хранилище на готовой двери.
func NewUsers(gate *dbgate.Gate) *Users { return &Users{gate: gate} }

// Create заводит пользователя и возвращает его вместе с секретом для
// привязки аутентификатора.
//
// Секрет отдаётся ровно один раз — при заведении. Хранится он, чтобы
// сверять коды, но показать его снова нельзя: секрет, который можно
// посмотреть в студии, перестаёт быть вторым доводом и становится вторым
// паролем, лежащим рядом с первым.
func (u *Users) Create(ctx context.Context, login, displayName string, perms []Permission) (User, string, error) {
	login = strings.TrimSpace(strings.ToLower(login))
	if login == "" {
		return User{}, "", errors.New("у пользователя нет имени входа")
	}
	for _, p := range perms {
		if !Known(p) {
			return User{}, "", fmt.Errorf("права %q не существует", p)
		}
	}

	secret, err := newSecret()
	if err != nil {
		return User{}, "", err
	}

	var id int64
	err = u.gate.QueryRow(ctx,
		`INSERT INTO users (login, display_name, totp_secret, permissions)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		login, displayName, secret, permStrings(perms)).Scan(&id)
	if err != nil {
		return User{}, "", fmt.Errorf("пользователь %q не заведён: %w", login, err)
	}
	return User{ID: id, Login: login, DisplayName: displayName, Permissions: perms}, secret, nil
}

// ByLogin читает пользователя по имени входа.
func (u *Users) ByLogin(ctx context.Context, login string) (User, string, error) {
	var (
		user     User
		secret   string
		perms    []string
		disabled *time.Time
	)
	err := u.gate.QueryRow(ctx,
		`SELECT id, login, display_name, totp_secret, permissions, disabled_at
		   FROM users WHERE login = $1`,
		strings.TrimSpace(strings.ToLower(login))).
		Scan(&user.ID, &user.Login, &user.DisplayName, &secret, &perms, &disabled)
	if err != nil {
		return User{}, "", fmt.Errorf("пользователь %q не найден: %w", login, err)
	}
	user.Disabled = disabled != nil
	// Право, которого больше нет в словаре, выбрасывается молча, и это
	// единственное место, где такое допустимо: словарь закрыт, а строка в
	// базе осталась от прежней редакции. Обратное — пустить неизвестное
	// право дальше — означало бы проверять доступ по значению, которого
	// никто уже не понимает.
	for _, raw := range perms {
		if p := Permission(raw); Known(p) {
			user.Permissions = append(user.Permissions, p)
		}
	}
	return user, secret, nil
}

// VerifyCode сверяет одноразовый код пользователя.
//
// Отключённый пользователь не входит, и проверяется это здесь, а не
// вызывающим: забыть проверку в одном из мест входа легче всего, а цена
// забывчивости — работающий вход у того, кого уволили.
func (u *Users) VerifyCode(ctx context.Context, login, code string, at time.Time) (User, error) {
	user, secret, err := u.ByLogin(ctx, login)
	if err != nil {
		return User{}, err
	}
	if user.Disabled {
		return User{}, errors.New("вход отключён")
	}
	if secret == "" {
		return User{}, errors.New("у пользователя не привязан аутентификатор")
	}
	if !totp.Verify(secret, code, at) {
		return User{}, errors.New("код не подошёл")
	}
	return user, nil
}

// newSecret порождает секрет аутентификатора.
//
// Двадцать байт — длина, которую RFC 4226 называет обязательным минимумом
// и которую ждут аутентификаторы.
func newSecret() (string, error) {
	var raw [20]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("секрет не порождён: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]), nil
}

func permStrings(perms []Permission) []string {
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, string(p))
	}
	return out
}
