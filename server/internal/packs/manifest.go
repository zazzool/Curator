// Пакеты задач: сборка, подпись и выдача.
//
// # Пакетом, а не лентой
//
// Клиент без сети должен видеть задачи, а не пустой экран. Лента этого не
// даёт: она страница за страницей, и в метро вторая страница не приедет.
// Пакет ставится целиком и живёт на устройстве.
//
// # Манифест подписан, и сверяет подпись устройство
//
// Между устройством и сервером стоит чужая сеть. Подменить содержание по
// дороге может всякий, кто в ней сидит, и заметить подмену можно только
// подписью. Закрытая половина ключа живёт в окружении и в базу не попадает
// никогда: утёкшая база не должна давать права подписывать.
//
// # Выпуск неизменяем
//
// Правка пакета — это новый выпуск, а не правка старого. На устройствах
// стоит именно тот состав, который подписан, и подменить его задним числом
// значит сделать подпись бессмысленной.
package packs

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Manifest — опись выпуска: что именно уехало на устройства.
type Manifest struct {
	Pack       string
	Version    int64
	Title      string
	ReleasedAt time.Time
	Cases      []Item
}

// Item — задача в пакете.
//
// Hash считается по содержанию задачи, а не по всему пакету: устройство
// качает задачи по одной и обязано проверить каждую, а не только итог.
// Проверка «весь пакет целиком» на обрыве посреди закачки не говорит
// ничего.
type Item struct {
	ID   string
	Ord  int
	Hash string
}

// Canonical приводит манифест к байтам, которые подписываются.
//
// # Почему не подписывается то, что лежит в базе
//
// Манифест хранится колонкой JSONB, а JSONB не хранит ни порядок ключей,
// ни пробелы: postgres пересобирает объект по-своему. Подпиши мы то, что
// достали из базы, — подпись не сошлась бы ни на одном устройстве, и
// причину искали бы неделю, потому что глазами оба текста одинаковы.
//
// Поэтому подписываемые байты СЧИТАЮТСЯ заново по одному правилу с обеих
// сторон: ключи по алфавиту, без пробелов, UTF-8, время по RFC 3339 в UTC.
// Правило записано в shared/pack-manifest.json вместе с разобранным
// примером — разойдись два способа привести манифест к байтам, и подпись
// не сойдётся нигде.
//
// Карта, а не структура: encoding/json пишет поля структуры в порядке
// объявления, а ключи карты — по алфавиту. Порядок, зависящий от того, как
// кто-то однажды переставил поля, для подписи не годится.
//
// И отдельно — экранирование. json.Marshal по умолчанию превращает <, > и
// & в \u003c, \u003e, \u0026: это наследство встраивания JSON в HTML, и
// больше никто так не делает. Пакет с амперсандом в названии подписался бы
// у нас одними байтами, а на устройстве свернулся бы в другие — и подпись
// не сошлась бы ровно у того выпуска, у которого в названии «и». Поэтому
// экранирование выключено явно.
func Canonical(m Manifest) ([]byte, error) {
	cases := make([]any, 0, len(m.Cases))
	for _, one := range m.Cases {
		cases = append(cases, map[string]any{
			"hash": one.Hash,
			"id":   one.ID,
			"ord":  one.Ord,
		})
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{
		"cases":      cases,
		"pack":       m.Pack,
		"releasedAt": m.ReleasedAt.UTC().Format(time.RFC3339),
		"title":      m.Title,
		"version":    m.Version,
	}); err != nil {
		return nil, err
	}
	// Encode дописывает перевод строки. В подпись он не входит: лишний
	// байт с одной стороны и его отсутствие с другой — то же расхождение,
	// что и порядок ключей.
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

// HashBody считает отпечаток содержания задачи.
//
// Отпечаток берётся от канонического вида, а не от байтов из базы, по той
// же причине, что и подпись: JSONB не хранит ни порядок ключей, ни
// пробелы.
func HashBody(body []byte) (string, error) {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("содержание задачи не разобрано: %w", err)
	}
	var canonical bytes.Buffer
	enc := json.NewEncoder(&canonical)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(parsed); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes.TrimRight(canonical.Bytes(), "\n"))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Sign подписывает манифест.
func Sign(m Manifest, key ed25519.PrivateKey) (string, error) {
	if len(key) != ed25519.PrivateKeySize {
		// Отказ, а не подпись пустым ключом: подпись, которую никто не
		// может проверить, выглядит на устройстве точно так же, как
		// настоящая, — до первой сверки.
		return "", errors.New("ключ подписи пакетов не задан или негоден")
	}
	raw, err := Canonical(m)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, raw)), nil
}

// Verify сверяет подпись. Тем же путём её сверяет и устройство.
func Verify(m Manifest, signature string, key ed25519.PublicKey) error {
	raw, err := Canonical(m)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("подпись не разобрана: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return errors.New("открытый ключ негоден")
	}
	if !ed25519.Verify(key, raw, sig) {
		// Ровно один текст на все случаи: и подменённое содержание, и
		// чужой ключ, и испорченная подпись означают одно — этому пакету
		// верить нельзя. Разные тексты рассказали бы тому, кто подменяет,
		// что именно у него не сошлось.
		return errors.New("подпись пакета не сошлась: этому выпуску верить нельзя")
	}
	return nil
}

// ParsePrivateKey читает закрытый ключ из окружения.
//
// Base64 от 64 байт ed25519. Хранится в окружении и в базу не попадает
// никогда.
func ParsePrivateKey(encoded string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("ключ подписи не разобран: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("ключ подписи длиной %d байт, а нужно %d",
			len(raw), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(raw), nil
}

// ParsePublicKey читает открытый ключ.
func ParsePublicKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("открытый ключ не разобран: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("открытый ключ длиной %d байт, а нужно %d",
			len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}
