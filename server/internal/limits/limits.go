// Пределы на обращения: размер тела и частота.
//
// Во всей службе их не было ни одного, кроме сторожа входа в студию.
// Следствия были не теоретические:
//
//   - `POST /v1/devices` заводит учётные записи, и ключ программы лежит в
//     сборке, то есть в руках у всякого, кто её разобрал. Ключ мешает
//     любопытному ровно до того, как он откроет apk;
//   - `POST /v1/recovery` шлёт письмо на чужой адрес. Промежуток между
//     письмами в минуту — это не защита ящика от заваливания, а
//     расписание: 1440 писем в сутки с нашего обратного адреса, и
//     отвечает за них наша почтовая репутация;
//   - тело запроса не ограничивалось ничем. Потолок в 500 разборов на
//     посылку сверялся ПОСЛЕ полного разбора тела, а контейнеру отведено
//     512 мегабайт памяти.
//
// Слой один и стоит посередине, а не в каждой ручке: предел, который надо
// не забыть позвать, однажды забудут — и забудут именно в той ручке,
// которую написали в спешке.
package limits

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Body ограничивает размер тела запроса.
//
// Отказ приходит от http.MaxBytesReader при чтении, то есть разбор тела
// прекращается на потолке, а не после него. В этом вся разница: проверка
// «сколько пришло» после json.Decode означает, что память уже занята.
//
// Названная длина сверяется отдельно и раньше, и это не дублирование.
// MaxBytesReader обрывает поток, и наружу это уезжает как «тело не
// разобрано» — отказ, по которому врачу нечего сделать, а нам нечего
// найти в журнале. Исправный клиент длину называет всегда; тот, кто её не
// назвал или соврал, упрётся в MaxBytesReader, и разговаривать с ним
// словами уже не обязательно.
func Body(max int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > max {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = w.Write([]byte(
				`{"error":"Запрос слишком большой. Разбейте его на части"}`))
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, max)
		}
		next.ServeHTTP(w, r)
	})
}

// Ведро жетонов на один ключ.
type bucket struct {
	tokens float64
	last   time.Time
}

// Bucket — ведро с жетонами по ключу: сколько обращений и как быстро
// они восстанавливаются.
//
// Ведро, а не счётчик в окне: счётчик пропускает весь свой запас в первую
// секунду окна и молчит до следующего, и всплеск проходит целиком. Ведро
// разрешает всплеск ровно на свою ёмкость и дальше пропускает со
// скоростью пополнения.
type Bucket struct {
	capacity  float64
	perSecond float64

	mu      sync.Mutex
	buckets map[string]*bucket
}

// Сколько вёдер должно накопиться, чтобы уборка стала оправданной.
const sweepFrom = 1024

// NewBucket заводит ведро: burst обращений сразу, затем perMinute в минуту.
func NewBucket(burst int, perMinute float64) *Bucket {
	return &Bucket{
		capacity:  float64(burst),
		perSecond: perMinute / 60,
		buckets:   map[string]*bucket{},
	}
}

// Allow снимает жетон и говорит, можно ли пропустить обращение.
func (b *Bucket) Allow(key string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	one, ok := b.buckets[key]
	if !ok {
		// Уборка перед заведением нового и только когда вёдер уже много:
		// на каждом обращении обход карты стоил бы дороже самой защиты.
		if len(b.buckets) >= sweepFrom {
			b.sweep(now)
		}
		b.buckets[key] = &bucket{tokens: b.capacity - 1, last: now}
		return true
	}
	one.tokens += now.Sub(one.last).Seconds() * b.perSecond
	if one.tokens > b.capacity {
		one.tokens = b.capacity
	}
	one.last = now
	if one.tokens < 1 {
		return false
	}
	one.tokens--
	return true
}

// sweep убирает вёдра, нетронутые дольше остывания.
//
// Без уборки карта растёт по одному входу на каждый адрес, когда-либо
// обращавшийся, и не уменьшается никогда — утечка ровно по числу
// посетителей.
//
// Зовётся изнутри Allow, а не отдельным часовым, и это выбор, а не
// лень. Уборщик, который надо не забыть запустить, однажды не запустят —
// в проекте уже есть два написанных и ни разу не позванных, и нашёл их
// аудит, а не отказ. Здесь забыть нечего: уборка — часть работы ведра.
func (b *Bucket) sweep(now time.Time) {
	for key, one := range b.buckets {
		// Ведро, дожившее до полного, не помнит ничего: пустить такой
		// адрес заново и завести ему свежее ведро — одно и то же.
		if now.Sub(one.last).Seconds()*b.perSecond >= b.capacity {
			delete(b.buckets, key)
		}
	}
}

// Address — адрес обращающегося, каким его видит наш обратный прокси.
//
// Берётся ПОСЛЕДНЯЯ запись X-Forwarded-For, а не первая, и это не
// придирка. Заголовок собирает nginx как $proxy_add_x_forwarded_for: он
// дописывает адрес, который увидел сам, в конец того, что прислал клиент.
// Всё, что левее, прислано клиентом и подделывается одной строкой — возьми
// первую запись, и перебирающий сменит себе адрес на каждое обращение,
// а предел перестанет существовать, оставаясь на вид работающим.
func Address(r *http.Request) string {
	if raw := r.Header.Get("X-Forwarded-For"); raw != "" {
		parts := strings.Split(raw, ",")
		if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
			return last
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
