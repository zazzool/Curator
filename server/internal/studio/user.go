package studio

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log"
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

	// secretSealed — лежал ли секрет в базе запечатанным. Наружу не
	// выдаётся: это не свойство пользователя, а состояние строки, по
	// которому VerifyCode решает, догонять её или нет.
	secretSealed bool
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
	// seal — ключ запечатывания секретов аутентификатора. Пусто — секреты
	// лежат открыто, как лежали; см. secret.go.
	seal []byte
}

// NewUsers собирает хранилище на готовой двери.
//
// Ключ запечатывания — отдельный довод, а не поле в настройках: он
// обязателен к прочтению тем, кто заводит хранилище, и nil здесь значит
// «решено хранить открыто», а не «забыли».
func NewUsers(gate *dbgate.Gate, seal []byte) *Users {
	return &Users{gate: gate, seal: seal}
}

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
	stored, err := sealSecret(u.seal, secret)
	if err != nil {
		return User{}, "", err
	}

	var id int64
	err = u.gate.QueryRow(ctx,
		`INSERT INTO users (login, display_name, totp_secret, permissions)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		login, displayName, stored, permStrings(perms)).Scan(&id)
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
	secret, sealed, err := openSecret(u.seal, secret)
	if err != nil {
		return User{}, "", fmt.Errorf("секрет пользователя %q не прочитан: %w", login, err)
	}
	user.secretSealed = sealed
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

// decoySecret — секрет-подставка для сверки, которой нечего сверять.
//
// Лежит открыто, и беды в этом нет: секрет пользователя — двадцать
// случайных байт, и совпасть с написанным здесь он не может. Работа у
// подставки одна — стоить столько же времени, сколько настоящая сверка.
const decoySecret = "CURATORDECOYSECRETFORTIMINGONLY2"

// VerifyCode сверяет одноразовый код пользователя.
//
// Отключённый пользователь не входит, и проверяется это здесь, а не
// вызывающим: забыть проверку в одном из мест входа легче всего, а цена
// забывчивости — работающий вход у того, кого уволили.
//
// # Негодный случай не уходит раньше годного
//
// Неизвестное имя, закрытый вход и непривязанный аутентификатор проходят ту
// же сверку кода, что и годный пользователь, — по секрету-подставке. Выйди
// они раньше, и ответ на них приходил бы заметно быстрее: разное время
// ответа отвечает на вопрос, существует ли имя, — ровно на тот, ради
// которого снаружи держится один отказ на все случаи (см. ErrGate).
//
// Чуда здесь нет: запрос к базе за ненайденным именем и за найденным стоит
// по-разному, и отсюда этого не выправить. Выправлено выправимое — разница
// в собственной работе входа.
func (u *Users) VerifyCode(ctx context.Context, login, code string, at time.Time) (User, error) {
	user, secret, reason := u.ByLogin(ctx, login)
	if reason == nil && user.Disabled {
		reason = errors.New("вход отключён")
	}
	if secret == "" {
		secret = decoySecret
		if reason == nil {
			reason = errors.New("у пользователя не привязан аутентификатор")
		}
	}

	fits := totp.Verify(secret, code, at)
	if reason != nil {
		return User{}, reason
	}
	if !fits {
		return User{}, errors.New("код не подошёл")
	}
	u.catchUp(ctx, user, secret)
	return user, nil
}

// catchUp запечатывает секрет, лежавший открытым.
//
// Догоняется он здесь, а не отдельным накатом, и довод тот же, по которому
// схема растёт только добавлением: накат, переписывающий секреты, идёт до
// подмены кода — то есть старый код получил бы запечатанное и перестал бы
// пускать в студию всех сразу. Здесь же перепись случается ПОСЛЕ удачной
// сверки, по одному человеку за раз, и каждая уже проверена тем, что код
// подошёл.
//
// Отказ переписи не роняет вход: человек уже доказал, кто он, и не пускать
// его из-за неудавшейся переписи значит менять беду на большую. Отказ
// уезжает в журнал — там он и разбирается.
func (u *Users) catchUp(ctx context.Context, user User, secret string) {
	if len(u.seal) == 0 || user.secretSealed {
		return
	}
	stored, err := sealSecret(u.seal, secret)
	if err != nil {
		log.Printf("секрет %q не запечатан: %v", user.Login, err)
		return
	}
	// Условие на открытость обязательно: два входа подряд могут прийти
	// разом, и второй перепечатал бы уже запечатанное — то есть запечатал
	// бы шифртекст вторым слоем, и распечатать его стало бы нечем.
	//
	// Метка берётся из своей постоянной, а не пишется в запросе строкой:
	// два места для одного значения расходятся молча, и разойдись они
	// здесь — перепись стала бы бесконечной.
	if _, err := u.gate.Exec(ctx,
		`UPDATE users SET totp_secret = $2
		   WHERE id = $1 AND totp_secret NOT LIKE $3`,
		user.ID, stored, sealedPrefix+"%"); err != nil {
		log.Printf("секрет %q не запечатан: %v", user.Login, err)
	}
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

// Shown — пользователь студии в списке.
//
// Секрета здесь нет и быть не может: секрет, который можно посмотреть в
// студии, перестаёт быть вторым доводом и становится вторым паролем,
// лежащим рядом с первым.
type Shown struct {
	Login       string
	DisplayName string
	Permissions []Permission
	Disabled    bool
	CreatedAt   time.Time
}

// All отдаёт пользователей студии.
func (u *Users) All(ctx context.Context) ([]Shown, error) {
	rows, err := u.gate.Query(ctx,
		`SELECT login, display_name, permissions, disabled_at, created_at
		   FROM users ORDER BY login`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Shown{}
	for rows.Next() {
		var one Shown
		var perms []string
		var disabled *time.Time
		if err := rows.Scan(&one.Login, &one.DisplayName, &perms, &disabled, &one.CreatedAt); err != nil {
			return nil, err
		}
		one.Disabled = disabled != nil
		one.Permissions = []Permission{}
		// Право не из словаря выбрасывается молча по тому же доводу, что
		// и в ByLogin: строка могла остаться от прежней редакции, а
		// показать право, которого никто не понимает, значит соврать.
		for _, raw := range perms {
			if p := Permission(raw); Known(p) {
				one.Permissions = append(one.Permissions, p)
			}
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

// ErrLastWorkshop — отказ, оберегающий вход в мастерскую.
//
// Заводит пользователей право workshop, и оно же их отключает. Сними его
// с последнего — и завести первого снова будет нельзя по кругу: входа,
// из-под которого это делается, не останется ни у кого, и лечится это
// только руками на контуре.
var ErrLastWorkshop = errors.New(
	"это последний человек с правом мастерской: сняв его, " +
		"завести пользователя будет некому")

// SetPermissions меняет права пользователя.
func (u *Users) SetPermissions(ctx context.Context, login string, perms []Permission) error {
	for _, p := range perms {
		if !Known(p) {
			return fmt.Errorf("права %q не существует", p)
		}
	}
	keeps := false
	for _, p := range perms {
		if p == PermWorkshop {
			keeps = true
		}
	}
	return u.change(ctx, login, keeps, func(ctx context.Context, login string) error {
		tag, err := u.gate.Exec(ctx,
			`UPDATE users SET permissions = $2 WHERE login = $1`, login, permStrings(perms))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("пользователя %q нет", login)
		}
		return nil
	})
}

// SetDisabled закрывает или открывает вход пользователю студии.
//
// Отметка, а не удаление строки: на пользователя ссылаются приходы и
// правки, подписанные его именем, и удаление порвало бы ответ на вопрос,
// кто это сделал.
func (u *Users) SetDisabled(ctx context.Context, login string, disabled bool) error {
	return u.change(ctx, login, !disabled, func(ctx context.Context, login string) error {
		var at any
		if disabled {
			at = time.Now()
		}
		tag, err := u.gate.Exec(ctx,
			`UPDATE users SET disabled_at = $2 WHERE login = $1`, login, at)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("пользователя %q нет", login)
		}
		return nil
	})
}

// change применяет правку, если после неё в мастерскую останется кому
// войти.
//
// Считается не «сам ли ты это делаешь», а сколько таких людей останется:
// запрет снимать право с себя обходится в два хода через второго
// пользователя, и запирается установка так же насмерть.
func (u *Users) change(ctx context.Context, login string, keepsWorkshop bool,
	apply func(context.Context, string) error) error {
	login = strings.TrimSpace(strings.ToLower(login))
	if !keepsWorkshop {
		var others int
		err := u.gate.QueryRow(ctx, `
			SELECT count(*) FROM users
			 WHERE login <> $1 AND disabled_at IS NULL
			   AND $2 = ANY (permissions)`, login, string(PermWorkshop)).Scan(&others)
		if err != nil {
			return err
		}
		if others == 0 {
			return ErrLastWorkshop
		}
	}
	return apply(ctx, login)
}
