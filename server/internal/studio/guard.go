package studio

import (
	"strings"
	"sync"
	"time"
)

// Сторож двери: предел неудачных попыток и одноразовость кода.
//
// # Зачем он нужен
//
// Код — шесть цифр, и годных в каждый момент три (окно в шаг в обе
// стороны). Один миллион вариантов на три годных — это не защита, если
// попытки не считать: перебор по сети добирается до входа за часы. Считать
// их — единственное, что превращает шесть цифр в довод.
//
// # Как устроено
//
// Счёт ведётся по имени входа, а не по адресу: студия стоит за обратным
// прокси, и адрес у всех обращений один — его. Считать по нему значит
// запирать дверь всем сразу из-за одного перебирающего.
//
// Плата за это названа вслух: перебирающий может запереть чужое имя на
// четверть часа. Это дешевле открытой двери, и заживает само — окно
// скользящее, а удавшийся вход сбрасывает счёт немедленно.
//
// # Почему в памяти, а не в базе
//
// Установка одна, процесс один. Перезапуск стирает счёт — но перезапустить
// сервер перебирающий не может, а выкатка, снявшая чужой запрет на четверть
// часа раньше срока, ничего не стоит. Таблица ради этого завела бы запись в
// базу на каждую неудачную попытку, то есть ровно то, чем перебор и грузит.
type guard struct {
	mu    sync.Mutex
	fails map[string]attemptRun
	used  map[string]time.Time
}

const (
	// guardWindow — окно наблюдения. Неудачи старше него не считаются.
	guardWindow = 15 * time.Minute

	// guardLimit — сколько неудач подряд выдерживает имя за окно.
	//
	// Десять: человек, у которого аутентификатор в руке, ошибается раз или
	// два, а перебирающему это оставляет меньше тысячи попыток в сутки —
	// на три годных кода из миллиона не хватит и жизни.
	guardLimit = 10

	// guardMaxHold — потолок задержки ответа.
	//
	// Задержка растёт вдвое с каждой неудачей (RFC тут ничего не велит, так
	// советует OWASP) и упирается в восемь секунд: дальше она перестаёт
	// мешать перебирающему — он всё равно шлёт запросы пачкой, — а честного
	// человека наказывает всерьёз.
	guardMaxHold = 8 * time.Second

	// guardCodeLife — сколько помнится предъявленный код.
	//
	// Полторы минуты: код годен в своё окно и в соседние, то есть не дольше
	// полутора шагов от выдачи. Помнить дольше незачем, короче — опасно.
	guardCodeLife = 90 * time.Second
)

type attemptRun struct {
	count int
	last  time.Time
}

func newGuard() *guard {
	return &guard{fails: map[string]attemptRun{}, used: map[string]time.Time{}}
}

// guardKey приводит имя к виду, по которому его ищут в базе.
//
// Иначе счёт обходится сменой регистра: «Мастер» и «мастер» — один
// пользователь и должны быть одной записью сторожа.
func guardKey(login string) string { return strings.TrimSpace(strings.ToLower(login)) }

// allow говорит, осталось ли имени право на попытку.
func (g *guard) allow(login string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forget(now)
	return g.fails[guardKey(login)].count < guardLimit
}

// note записывает неудачу и отдаёт, сколько придержать ответ.
//
// Задержка считается по числу неудач ПОСЛЕ этой: первая стоит секунды,
// вторая — двух, и так до потолка.
func (g *guard) note(login string, now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forget(now)

	key := guardKey(login)
	run := g.fails[key]
	run.count++
	run.last = now
	g.fails[key] = run

	hold := time.Second << min(run.count-1, 16)
	if hold > guardMaxHold || hold <= 0 {
		hold = guardMaxHold
	}
	return hold
}

// clear сбрасывает счёт: вошедший доказал, что он свой.
func (g *guard) clear(login string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, guardKey(login))
}

// consume отмечает код предъявленным и отдаёт false, если он уже был.
//
// Код годен три окна подряд, и всё это время подсмотренный через плечо (или
// подобранный из журнала прокси) код работает второй раз. OWASP велит
// гасить одноразовый код при удачной сверке — здесь это единственное место,
// где известно, что сверка удалась.
func (g *guard) consume(login, code string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forget(now)

	key := guardKey(login) + "\x00" + strings.TrimSpace(code)
	if until, seen := g.used[key]; seen && until.After(now) {
		return false
	}
	g.used[key] = now.Add(guardCodeLife)
	return true
}

// forget выбрасывает отжившее. Зовётся под замком из каждого действия, а не
// по часам: сторож без уборки — это карта, которая только растёт, и растёт
// она ровно от перебора.
func (g *guard) forget(now time.Time) {
	for key, run := range g.fails {
		if now.Sub(run.last) > guardWindow {
			delete(g.fails, key)
		}
	}
	for key, until := range g.used {
		if !until.After(now) {
			delete(g.used, key)
		}
	}
}
