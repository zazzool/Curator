// Вывоз снимков с хоста.
//
// # Зачем
//
// Снимок, лежащий рядом с базой, переживает контейнер и не переживает
// хост. Один отказавший диск или один шифровальщик уносит и базу, и все
// её снимки разом — а задачи, разметка и разборы существуют в одном
// экземпляре, и восстановить их из ничего нельзя: модель напишет другое.
//
// # Почему по HTTPS в хранилище, а не scp или rclone
//
// И scp, и rclone — это чужая программа в образе и ключ от чужой машины
// внутри контейнера. Ключ, дающий вход на вторую машину, лежащий в
// контейнере, который смотрит в интернет, — это не вторая копия, а вторая
// машина в том же периметре. Ключ доступа к одному ведру не даёт ни входа,
// ни соседних вёдер.
//
// Хранилище — любое, говорящее по-S3: своё, отечественное или чужое. Это
// не привязка к поставщику, а общий язык, на котором говорят все.
//
// # Что уезжает
//
// Запечатанный снимок и ничего больше. Ключ шифрования в хранилище не
// уезжает никогда и живёт только у нас: хранилище, у которого есть и файл,
// и ключ, защищает ровно от того, кто унесёт его диск, и ни от чего больше.
package backup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Offsite — второе хранилище: куда вывозить и чем запечатывать.
type Offsite struct {
	// Endpoint — адрес хранилища целиком, со схемой.
	Endpoint string

	// Bucket — ведро. Обращение идёт путём (endpoint/bucket/имя), а не
	// именем поддомена: имя поддомена требует своей записи DNS и своего
	// сертификата, и на своём хранилище его чаще всего нет.
	Bucket string

	Region string
	KeyID  string
	Secret string

	// SealKey — ключ шифрования, 32 байта. Пусто — вывоза нет вовсе:
	// вывезти снимок незапечатанным нельзя даже один раз.
	SealKey []byte

	// Client — чем ходить. Полем ради проверок.
	Client *http.Client

	// Now — источник времени. Полем по той же причине: подпись зависит от
	// часа, и проверка с настоящим временем сверяла бы сама с собой.
	Now func() time.Time
}

// Ready — можно ли вывозить.
//
// Все поля или ни одного. Наполовину заполненная настройка — это вывоз,
// который выглядит заведённым и не работает, и узнается это в тот
// единственный день, когда снимок нужен.
func (o *Offsite) Ready() bool {
	return o != nil && o.Endpoint != "" && o.Bucket != "" &&
		o.KeyID != "" && o.Secret != "" && len(o.SealKey) == 32
}

// Put запечатывает src и кладёт под именем name.
//
// Запечатывается в память целиком, и это осознанная цена: подпись S3
// требует отпечатка всего тела заранее, а отпечаток по потоку означает
// либо второй проход по файлу, либо подпись по кускам — вдвое больше
// кода в том самом месте, ошибка в котором молча отменяет шифрование.
// Снимок базы с задачами и разметкой — это десятки мегабайт, а не
// десятки гигабайт.
func (o *Offsite) Put(ctx context.Context, name string, src io.Reader) error {
	if !o.Ready() {
		return fmt.Errorf("второе хранилище не настроено")
	}

	var sealed bytes.Buffer
	if err := Seal(&sealed, src, o.SealKey); err != nil {
		return fmt.Errorf("снимок не запечатан: %w", err)
	}
	body := sealed.Bytes()

	where, err := url.JoinPath(o.Endpoint, o.Bucket, name)
	if err != nil {
		return fmt.Errorf("адрес в хранилище не собран: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, where, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("запрос не собран: %w", err)
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	o.sign(req, body)

	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	reply, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("хранилище недоступно: %w", err)
	}
	defer reply.Body.Close()
	if reply.StatusCode < 200 || reply.StatusCode >= 300 {
		// Первые пол-килобайта ответа: S3 объясняет отказ в теле, а
		// голый код состояния не даёт понять, дело в праве, в имени
		// ведра или в часах, разошедшихся с хранилищем.
		said, _ := io.ReadAll(io.LimitReader(reply.Body, 512))
		return fmt.Errorf("хранилище отказало (%d): %s",
			reply.StatusCode, strings.TrimSpace(string(said)))
	}
	return nil
}

// sign подписывает запрос по AWS Signature Version 4.
//
// Своим кодом, а не набором поставщика: набор тянет за собой десятки
// зависимостей ради сотни строк, а зависимостей у сервера четыре, и
// каждая названа вслух.
func (o *Offsite) sign(req *http.Request, body []byte) {
	now := time.Now().UTC()
	if o.Now != nil {
		now = o.Now().UTC()
	}
	stamp := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	region := o.Region
	if region == "" {
		// Хранилища, не знающие областей, принимают любое имя, но пустое
		// место в строке доверия делает подпись негодной у всех.
		region = "us-east-1"
	}

	payload := sha256.Sum256(body)
	hashed := hex.EncodeToString(payload[:])
	req.Header.Set("X-Amz-Content-Sha256", hashed)
	req.Header.Set("X-Amz-Date", stamp)

	signed := "host;x-amz-content-sha256;x-amz-date"
	canonical := strings.Join([]string{
		req.Method,
		escapePath(req.URL.Path),
		req.URL.RawQuery,
		"host:" + req.URL.Host,
		"x-amz-content-sha256:" + hashed,
		"x-amz-date:" + stamp,
		"",
		signed,
		hashed,
	}, "\n")

	scope := day + "/" + region + "/s3/aws4_request"
	sum := sha256.Sum256([]byte(canonical))
	toSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		stamp,
		scope,
		hex.EncodeToString(sum[:]),
	}, "\n")

	signature := hex.EncodeToString(mac(signingKey(o.Secret, day, region, "s3"), toSign))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		o.KeyID, scope, signed, signature))
}

// signingKey выводит ключ подписи дня.
//
// Ключ живёт один день и одну область: утёкшая подпись не даёт ни
// завтрашнего дня, ни соседней службы.
func signingKey(secret, day, region, service string) []byte {
	key := mac([]byte("AWS4"+secret), day)
	key = mac(key, region)
	key = mac(key, service)
	return mac(key, "aws4_request")
}

func mac(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// escapePath кодирует путь так, как этого ждёт подпись.
//
// Косая черта остаётся собой, всё прочее кодируется; пробел уезжает как
// %20, а не как плюс — url.QueryEscape делает наоборот, и подпись
// разошлась бы ровно на именах с пробелом.
func escapePath(path string) string {
	parts := strings.Split(path, "/")
	for i, one := range parts {
		parts[i] = strings.ReplaceAll(url.QueryEscape(one), "+", "%20")
	}
	return strings.Join(parts, "/")
}

// FromEnv собирает вывоз из переменных окружения.
//
// Одним местом на службу и на разовую съёмку: две сборки одной настройки
// расходятся молча, и расходиться они будут в том, что у одной вывоз есть,
// а у другой нет.
//
// Ключ шифрования читается из base64 и обязан дать ровно 32 байта.
// Негодный ключ — это отказ вывоза целиком, а не вывоз без шифрования:
// второе выглядело бы как работающая защита.
func FromEnv(env func(string) string) (Offsite, error) {
	out := Offsite{
		Endpoint: env("CURATOR_OFFSITE_ENDPOINT"),
		Bucket:   env("CURATOR_OFFSITE_BUCKET"),
		Region:   env("CURATOR_OFFSITE_REGION"),
		KeyID:    env("CURATOR_OFFSITE_KEY_ID"),
		Secret:   env("CURATOR_OFFSITE_SECRET"),
	}
	raw := env("CURATOR_OFFSITE_SEAL_KEY")
	if raw == "" {
		// Пусто — вывоза просто нет, и это не отказ: настройка
		// необязательна. Отказом это становится, когда её начали
		// заполнять и не дозаполнили.
		if out.Endpoint == "" && out.Bucket == "" && out.KeyID == "" && out.Secret == "" {
			return Offsite{}, nil
		}
		return Offsite{}, fmt.Errorf(
			"вывоз настроен наполовину: нет CURATOR_OFFSITE_SEAL_KEY, " +
				"а вывозить снимок незапечатанным нельзя")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return Offsite{}, fmt.Errorf("CURATOR_OFFSITE_SEAL_KEY не читается как base64: %w", err)
	}
	if len(key) != 32 {
		return Offsite{}, fmt.Errorf(
			"CURATOR_OFFSITE_SEAL_KEY даёт %d байт вместо 32", len(key))
	}
	out.SealKey = key
	if !out.Ready() {
		return Offsite{}, fmt.Errorf(
			"вывоз настроен наполовину: нужны CURATOR_OFFSITE_ENDPOINT, " +
				"BUCKET, KEY_ID и SECRET")
	}
	return out, nil
}
