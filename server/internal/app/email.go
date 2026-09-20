package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Почта врача: привязка и возврат доступа.
//
// # Зачем это вообще
//
// Запись заводится молча, при первом запуске, и живёт на устройстве. Со
// сменой телефона врач теряет всё: разобранные задачи, знаки, купленные
// наборы. Почта — единственный способ это вернуть, и других доводов у
// него нет: пароля в продукте не существует.
//
// # Отсюда и осторожность
//
// Раз почта — единственный довод, то ручка возврата доступа и есть дверь
// к чужой записи. Поэтому: код одноразовый и лежит отпечатком, у него
// срок и потолок попыток, а ответ на запрос **не зависит** от того, знаем
// ли мы такую почту. Иначе ручка становится способом узнать, заведён ли
// такой врач, — и это при том, что почта врача обычно рабочая и легко
// угадывается.

const (
	// Срок кода. Пятнадцать минут — столько человек ищет письмо в папке
	// «спам» и успевает вернуться; час дал бы злоумышленнику окно на
	// перебор вчетверо шире при той же пользе.
	codeLives = 15 * time.Minute

	// Потолок попыток на один код. Шестизначный код перебирается за
	// миллион попыток, и без потолка это вопрос минут.
	codeAttempts = 5

	// Между двумя письмами на один адрес. Без задержки ручка становится
	// способом завалить чужой ящик нашими письмами — с нашего адреса.
	codeCooldown = time.Minute

	// Зачем выдан код. Сверяется при гашении: код, выданный для привязки
	// адреса, не должен открывать возврат доступа, и наоборот. Строки те
	// же, что в CHECK у колонки — разойдись они, накат отказал бы на
	// первой же вставке.
	purposeBind     = "bind"
	purposeRecovery = "recovery"
)

// Emails — коды на почту.
type Emails struct {
	accounts *Accounts
}

func NewEmails(accounts *Accounts) *Emails { return &Emails{accounts: accounts} }

// ErrTooOften — код просили слишком часто.
var ErrTooOften = errors.New("письмо уже отправлено, подождите минуту")

// ErrBadCode — код не подошёл.
//
// Один отказ на «не тот код», «срок вышел» и «попытки кончились»:
// разные ответы рассказали бы перебирающему, движется ли он верно.
var ErrBadCode = errors.New("код не подошёл или устарел")

// Normalize приводит адрес к виду, в котором он сравнивается.
//
// Регистр снимается, пробелы по краям срезаются. Дальше этого не идём:
// точки в имени ящика и «плюс-адреса» значимы не у всех поставщиков, и
// сведя их, мы склеили бы двух разных людей в одного.
func Normalize(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Looks говорит, похоже ли это на адрес.
//
// Проверка намеренно грубая: строгой проверки адреса не существует, а
// каждая попытка её написать отвергает чей-нибудь настоящий ящик. Кто
// ошибся в адресе — не получит письма и увидит это сам.
func Looks(email string) bool {
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return false
	}
	return strings.Contains(email[at+1:], ".") && !strings.ContainsAny(email, " \t\r\n")
}

// newCode порождает шестизначный код.
//
// crypto/rand, а не math/rand: предсказуемый код — это вход в чужую
// запись без единой попытки перебора.
func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", fmt.Errorf("код не порождён: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// StartBind заводит код привязки и отдаёт его для отправки письмом.
//
// Адрес, уже привязанный к другой записи, кода не получает — но и отказа
// не вызывает: ответ обязан быть одинаковым, иначе врач с чужим токеном
// устройства узнаёт, заведён ли у нас интересующий его человек.
// Вернувшийся пустой код означает «письма не будет», и знать об этом
// должен только сервер.
func (e *Emails) StartBind(ctx context.Context, accountID int64, email string, now time.Time) (string, error) {
	email = Normalize(email)
	if !Looks(email) {
		return "", errors.New("это не похоже на адрес почты")
	}

	code, err := newCode()
	if err != nil {
		return "", err
	}

	err = e.accounts.gate.InTx(ctx, func(tx pgx.Tx) error {
		var busy int64
		err := tx.QueryRow(ctx,
			`SELECT account_id FROM account_identities
			  WHERE provider = 'email' AND external_id = $1`, email).Scan(&busy)
		switch {
		case err == nil && busy != accountID:
			// Чужой адрес: код не заводим, но и отказа не даём.
			code = ""
			return nil
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return err
		}

		var recent bool
		err = tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM email_codes
			  WHERE email = $1 AND used_at IS NULL AND created_at > $2)`,
			email, now.Add(-codeCooldown)).Scan(&recent)
		if err != nil {
			return err
		}
		if recent {
			return ErrTooOften
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO email_codes (account_id, email, code_hash, purpose, expires_at, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			accountID, email, fingerprint(code), purposeBind, now.Add(codeLives), now)
		return err
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// ConfirmBind сверяет код и привязывает почту.
func (e *Emails) ConfirmBind(ctx context.Context, accountID int64, code string, now time.Time) (string, error) {
	var email string
	var ok bool
	err := e.accounts.gate.InTx(ctx, func(tx pgx.Tx) error {
		var err error
		email, _, ok, err = consume(ctx, tx, pickByAccount, accountID, purposeBind, code, now)
		if err != nil || !ok {
			// Несовпадение отсюда НЕ уходит отказом: отказ откатил бы
			// транзакцию, а с нею и засчитанную попытку. Отказ называется
			// после, когда счётчик уже записан.
			return err
		}

		// Запись и опознание меняются вместе: почта в accounts — то, что
		// показывается врачу, а строка в account_identities — то, по чему
		// его находят при возврате доступа. Разойдись они, врач видел бы
		// привязанную почту, а войти по ней не мог.
		if _, err := tx.Exec(ctx,
			`UPDATE accounts SET email = $2 WHERE id = $1`, accountID, email); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO account_identities (account_id, provider, external_id)
			 VALUES ($1, 'email', $2)
			 ON CONFLICT (provider, external_id) DO NOTHING`, accountID, email)
		return err
	})
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrBadCode
	}
	return email, nil
}

// StartRecovery заводит код возврата доступа.
//
// Пустой код означает «такой почты у нас нет», и наружу это не уезжает:
// ручка отвечает одинаково всегда.
func (e *Emails) StartRecovery(ctx context.Context, email string, now time.Time) (string, error) {
	email = Normalize(email)
	if !Looks(email) {
		return "", nil
	}

	code, err := newCode()
	if err != nil {
		return "", err
	}

	err = e.accounts.gate.InTx(ctx, func(tx pgx.Tx) error {
		var accountID int64
		err := tx.QueryRow(ctx,
			`SELECT account_id FROM account_identities
			  WHERE provider = 'email' AND external_id = $1`, email).Scan(&accountID)
		if errors.Is(err, pgx.ErrNoRows) {
			code = ""
			return nil
		}
		if err != nil {
			return err
		}

		var recent bool
		err = tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM email_codes
			  WHERE email = $1 AND used_at IS NULL AND created_at > $2)`,
			email, now.Add(-codeCooldown)).Scan(&recent)
		if err != nil {
			return err
		}
		if recent {
			// Молча, без отказа: иначе задержка сама становится ответом на
			// вопрос, заведена ли такая почта.
			code = ""
			return nil
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO email_codes (account_id, email, code_hash, purpose, expires_at, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			accountID, email, fingerprint(code), purposeRecovery, now.Add(codeLives), now)
		return err
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// ConfirmRecovery сверяет код и заводит устройство на найденной записи.
//
// Гашение кода и заведение устройства идут одной транзакцией. Разними их —
// и обрыв связи ровно посередине сжигает единственный код врача, не дав
// ему взамен ничего: писать второй раз ему нельзя минуту, а понять, что
// произошло, нельзя вовсе.
func (e *Emails) ConfirmRecovery(ctx context.Context, email, code string, about About, now time.Time) (Device, error) {
	email = Normalize(email)

	token, err := newToken()
	if err != nil {
		return Device{}, err
	}

	var out Device
	var ok bool
	err = e.accounts.gate.InTx(ctx, func(tx pgx.Tx) error {
		_, accountID, matched, err := consume(ctx, tx, pickByEmail, email, purposeRecovery, code, now)
		ok = matched
		if err != nil || !matched {
			return err
		}
		id, err := addDevice(ctx, tx, accountID, token, about)
		if err != nil {
			return err
		}
		out = Device{AccountID: accountID, DeviceID: id}
		return nil
	})
	if err != nil {
		return Device{}, err
	}
	if !ok {
		return Device{}, ErrBadCode
	}
	out.Token = token
	return out, nil
}

// Чем выбирается код. Привязку ищем по учётной записи (адрес ещё не её,
// и найти по нему нельзя), возврат доступа — по адресу (учётной записи мы
// как раз и не знаем).
const (
	pickByAccount = `SELECT id FROM email_codes
	                  WHERE account_id = $1 AND purpose = $2 AND used_at IS NULL
	                  ORDER BY id DESC LIMIT 1`
	pickByEmail = `SELECT id FROM email_codes
	                WHERE email = $1 AND purpose = $2 AND used_at IS NULL
	                ORDER BY id DESC LIMIT 1`
)

// consume засчитывает попытку и гасит код, если он совпал.
//
// Одним запросом, а не «прочитать, сверить, записать». Причина стоила
// проверки: несовпадение — это отказ, отказ откатывает транзакцию, а
// откат уносит с собой и счётчик попыток. Потолок попыток при этом
// существует только на бумаге, и перебор идёт бесплатно. Здесь счётчик
// двигается тем же UPDATE, что и гашение, и остаётся записанным, чем бы
// дело ни кончилось; отказ называется снаружи, после успешной
// транзакции.
//
// Совпадение возвращается третьим значением, а не отказом, по той же
// причине: отказ отсюда откатил бы попытку.
func consume(ctx context.Context, tx pgx.Tx, pick string, key any, purpose, code string,
	now time.Time) (email string, accountID int64, ok bool, err error) {
	err = tx.QueryRow(ctx,
		`UPDATE email_codes
		    SET attempts = attempts + 1,
		        used_at = CASE WHEN code_hash = $3 AND expires_at > $4 THEN $4 END
		  WHERE id = (`+pick+`)
		    AND attempts < $5
		 RETURNING email, account_id, used_at IS NOT NULL`,
		key, purpose, fingerprint(strings.TrimSpace(code)), now, codeAttempts,
	).Scan(&email, &accountID, &ok)
	if errors.Is(err, pgx.ErrNoRows) {
		// Кода нет вовсе, либо попытки кончились. Один и тот же ответ на
		// оба случая: разные рассказали бы перебирающему, движется ли он
		// верно.
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return email, accountID, ok, nil
}
