package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Учётная запись врача и его устройства.
//
// # Запись заводится молча, при первом запуске
//
// Спрашивать имя и почту у человека, который ещё не понял, что ему
// предлагают, — верный способ его потерять. Поэтому первый запуск не
// спрашивает ничего: приложение заводит устройство, получает токен и
// работает. Почта привязывается позже и по своей воле — она нужна, чтобы
// не потерять всё при смене телефона, и сказать это надо тогда, когда
// человеку уже есть что терять.

// Accounts — учётные записи в базе.
type Accounts struct {
	gate *dbgate.Gate
}

func NewAccounts(gate *dbgate.Gate) *Accounts { return &Accounts{gate: gate} }

// Device — устройство, каким его видит приложение.
type Device struct {
	AccountID int64
	DeviceID  int64

	// Token отдаётся ровно один раз — при заведении. Дальше в базе лежит
	// только отпечаток, и «напомнить токен» невозможно by design:
	// возможность напомнить означала бы, что утёкшая база даёт вход.
	Token string
}

// About — что приложение рассказывает о себе при заведении.
//
// Всё необязательно: сборка, не приславшая модель устройства, обязана
// работать. Эти сведения нужны отчётам, а не работе, и отказ из-за
// отсутствующей строки был бы отказом врачу ради нашего удобства.
type About struct {
	Platform   string `json:"platform"`
	OSVersion  string `json:"osVersion"`
	Model      string `json:"model"`
	AppVersion string `json:"appVersion"`
}

// Enroll заводит устройство, а с ним при надобности и учётную запись.
//
// Обе строки в одной транзакции: учётная запись без устройства — это
// запись, к которой никто не может войти, и появилась бы она молча, при
// обрыве связи ровно посередине.
func (a *Accounts) Enroll(ctx context.Context, about About) (Device, error) {
	token, err := newToken()
	if err != nil {
		return Device{}, err
	}

	var out Device
	err = a.gate.InTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO accounts DEFAULT VALUES RETURNING id`).Scan(&out.AccountID); err != nil {
			return fmt.Errorf("учётная запись не заведена: %w", err)
		}
		id, err := addDevice(ctx, tx, out.AccountID, token, about)
		if err != nil {
			return err
		}
		out.DeviceID = id
		return nil
	})
	if err != nil {
		return Device{}, err
	}
	out.Token = token
	return out, nil
}

// EnrollOn заводит устройство на уже существующей записи.
//
// Этим возвращается доступ тому, кто подтвердил почту с нового телефона:
// запись остаётся прежней — с опытом, знаками и купленным доступом, — а
// устройство добавляется рядом. Прежние устройства при этом не выбрасываются
// намеренно: врач, восстановившийся на планшете, не должен обнаружить, что
// его телефон разлогинился; лишнее он уберёт сам в «Устройствах».
func (a *Accounts) EnrollOn(ctx context.Context, accountID int64, about About) (Device, error) {
	token, err := newToken()
	if err != nil {
		return Device{}, err
	}

	out := Device{AccountID: accountID}
	err = a.gate.InTx(ctx, func(tx pgx.Tx) error {
		id, err := addDevice(ctx, tx, accountID, token, about)
		if err != nil {
			return err
		}
		out.DeviceID = id
		return nil
	})
	if err != nil {
		return Device{}, err
	}
	out.Token = token
	return out, nil
}

// addDevice — общая половина обоих заведений.
//
// Вынесена не ради краткости, а потому что колонки устройства правятся
// целиком: две копии этого INSERT разошлись бы на первой же новой колонке,
// и разошлись бы молча — оба пути возвращают рабочий токен.
func addDevice(ctx context.Context, tx pgx.Tx, accountID int64, token string, about About) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO devices (account_id, token_hash, platform, os_version, model, app_version, last_seen)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 RETURNING id`,
		accountID, fingerprint(token),
		trim(about.Platform, 32), trim(about.OSVersion, 64),
		trim(about.Model, 64), trim(about.AppVersion, 32)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("устройство не заведено: %w", err)
	}
	return id, nil
}

// newToken порождает токен устройства.
func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("токен устройства не выдан: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ErrNoDevice — устройство не опознано.
var ErrNoDevice = errors.New("устройство не опознано")

// ByToken находит устройство по токену.
//
// Заодно отмечает, что устройство живо. Отметка идёт тем же запросом, а не
// вторым: второй запрос на каждое обращение приложения — это удвоение
// нагрузки ради колонки, на которую смотрят раз в месяц.
func (a *Accounts) ByToken(ctx context.Context, token string) (Caller, error) {
	if token == "" {
		return Caller{}, ErrNoDevice
	}
	var out Caller
	var blocked *time.Time
	err := a.gate.QueryRow(ctx,
		`UPDATE devices SET last_seen = NOW()
		  WHERE token_hash = $1
		 RETURNING id, account_id,
		           (SELECT blocked_at FROM accounts WHERE id = devices.account_id)`,
		fingerprint(token)).Scan(&out.DeviceID, &out.AccountID, &blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Caller{}, ErrNoDevice
	}
	if err != nil {
		return Caller{}, fmt.Errorf("устройство не прочитано: %w", err)
	}
	out.Blocked = blocked != nil
	return out, nil
}

// Profile — что врач видит о себе.
type Profile struct {
	AccountID   int64
	Email       string
	DisplayName string
	CreatedAt   time.Time
}

// Profile читает учётную запись.
func (a *Accounts) Profile(ctx context.Context, accountID int64) (Profile, error) {
	var out Profile
	var email *string
	err := a.gate.QueryRow(ctx,
		`SELECT id, email, display_name, created_at FROM accounts WHERE id = $1`,
		accountID).Scan(&out.AccountID, &email, &out.DisplayName, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, errors.New("такой учётной записи нет")
	}
	if err != nil {
		return Profile{}, fmt.Errorf("учётная запись не прочитана: %w", err)
	}
	if email != nil {
		out.Email = *email
	}
	return out, nil
}

// Rename меняет имя, под которым врача видно в таблицах.
func (a *Accounts) Rename(ctx context.Context, accountID int64, name string) error {
	_, err := a.gate.Exec(ctx,
		`UPDATE accounts SET display_name = $2 WHERE id = $1`, accountID, trim(name, 64))
	if err != nil {
		return fmt.Errorf("имя не сохранено: %w", err)
	}
	return nil
}

// Forget убирает устройство.
//
// Выход с одного устройства не трогает остальные: врач, вышедший на
// служебном планшете, не должен вылететь со своего телефона.
func (a *Accounts) Forget(ctx context.Context, deviceID int64) error {
	_, err := a.gate.Exec(ctx, `DELETE FROM devices WHERE id = $1`, deviceID)
	if err != nil {
		return fmt.Errorf("устройство не забыто: %w", err)
	}
	return nil
}

// trim обрезает строку по рунам, а не по байтам.
//
// По байтам — значит разрубить кириллическую букву пополам, и в базу
// уедет строка, негодная как UTF-8. Сведения эти необязательные, и
// отказывать из-за длинной модели устройства незачем: приложение
// обновится не завтра, а работать должно сегодня.
func trim(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
