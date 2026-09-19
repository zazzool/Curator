// Одноразовые коды входа (RFC 6238).
//
// Написано на стандартной библиотеке: это HMAC-SHA1 над счётчиком времени и
// десяток строк обвязки, а всё нужное — crypto/hmac, crypto/sha1,
// encoding/base32 — уже есть. Зависимость ради этого не заводится.
//
// # Почему SHA-1 и почему это не дефект
//
// RFC 6238 называет SHA-1 обязательным алгоритмом, и его понимают все
// приложения-аутентификаторы. Слабость SHA-1 — это слабость к поиску
// столкновений, а здесь он работает в HMAC над секретом, где столкновения
// ни при чём. Заменить его на SHA-256 значит получить коды, которые не
// сойдутся ни с одним аутентификатором.
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// Step — шаг счётчика. Тридцать секунд — то, что подставляют по умолчанию
// все аутентификаторы; другое значение пришлось бы объявлять в ссылке
// привязки и всё равно объяснять человеку.
const Step = 30 * time.Second

// Digits — длина кода.
const Digits = 6

// Code считает код для указанного времени.
func Code(secret string, at time.Time) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	counter := uint64(at.Unix()) / uint64(Step.Seconds())
	return hotp(key, counter), nil
}

// Verify сверяет код, допуская один шаг в обе стороны.
//
// Окно в один шаг — не послабление, а необходимость: часы телефона и
// сервера расходятся на секунды, и человек, набравший код на 29-й секунде,
// отправит его на 31-й. Окно шире стоило бы дорого: каждый лишний шаг
// удваивает число годных кодов.
//
// Сравнение — за постоянное время. Перебор по сети и так ограничен, но
// привычка сравнивать секреты побайтово однажды обходится дорого.
func Verify(secret, code string, at time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != Digits {
		return false
	}
	for _, shift := range []time.Duration{-Step, 0, Step} {
		want, err := Code(secret, at.Add(shift))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// URI — ссылка привязки для аутентификатора.
//
// Отдаётся вместе с секретом при заведении пользователя: набирать секрет
// руками человек не станет, а ошибётся — и вход не заработает, причём
// молча.
func URI(issuer, login, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&digits=%d&period=%d",
		issuer, login, strings.ToUpper(secret), issuer, Digits, int(Step.Seconds()))
}

// hotp — код по счётчику (RFC 4226).
func hotp(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Усечение по последнему полубайту: так велит RFC, и так же считают
	// аутентификаторы.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	mod := uint32(1)
	for i := 0; i < Digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", Digits, value%mod)
}

// decodeSecret читает секрет из base32.
//
// Дополнение необязательно и пробелы выбрасываются: аутентификаторы
// показывают секрет группами по четыре знака, и человек, скопировавший его
// вместе с пробелами, не должен получать отказ без объяснения.
func decodeSecret(secret string) ([]byte, error) {
	clean := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	if clean == "" {
		return nil, fmt.Errorf("секрет пуст")
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("секрет не читается как base32: %w", err)
	}
	return key, nil
}
