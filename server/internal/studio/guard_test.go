package studio

import (
	"testing"
	"time"
)

// Проверки сторожа двери идут без базы: считать попытки и гасить
// предъявленные коды — работа памяти, а не хранилища, и живой базы для неё
// не нужно. Сквозной вход через сторожа проверяется отдельно, на базе.

func TestСторожДаётРовноДесятьПопыток(t *testing.T) {
	g := newGuard()
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	for i := 1; i <= guardLimit; i++ {
		if !g.allow("мастер", at) {
			t.Fatalf("попытка %d отвергнута, а предел %d", i, guardLimit)
		}
		g.note("мастер", at)
	}
	if g.allow("мастер", at) {
		t.Fatal("одиннадцатая попытка прошла: перебор кода ничем не ограничен")
	}
}

func TestСторожСчитаетИмяБезОглядкиНаРегистр(t *testing.T) {
	// Иначе счёт обходится клавишей Caps Lock: в базе имя одно, а записей у
	// сторожа столько, сколько написаний придумает перебирающий.
	g := newGuard()
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	for i := 0; i < guardLimit; i++ {
		g.note("  Мастер  ", at)
	}
	if g.allow("мастер", at) {
		t.Fatal("то же имя другим написанием получило свежий предел попыток")
	}
}

func TestЗадержкаРастётВдвоеИУпираетсяВПотолок(t *testing.T) {
	g := newGuard()
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	want := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		guardMaxHold, guardMaxHold,
	}
	for i, expected := range want {
		if got := g.note("мастер", at); got != expected {
			t.Errorf("задержка после неудачи %d — %v, а должна быть %v", i+1, got, expected)
		}
	}
}

func TestУдачныйВходСбрасываетСчёт(t *testing.T) {
	g := newGuard()
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	for i := 0; i < guardLimit-1; i++ {
		g.note("мастер", at)
	}
	g.clear("мастер")

	if got := g.note("мастер", at); got != time.Second {
		t.Errorf("после удачного входа задержка %v, а счёт должен был обнулиться", got)
	}
}

func TestОкноНаблюденияИстекаетИОтпускает(t *testing.T) {
	// Плата за счёт по имени — чужое имя можно запереть. Она терпима
	// только потому, что запрет заживает сам.
	g := newGuard()
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	for i := 0; i < guardLimit; i++ {
		g.note("мастер", at)
	}
	if g.allow("мастер", at.Add(guardWindow-time.Minute)) {
		t.Fatal("запрет снялся до конца окна наблюдения")
	}
	if !g.allow("мастер", at.Add(guardWindow+time.Second)) {
		t.Fatal("окно наблюдения истекло, а имя осталось запертым")
	}
}

func TestКодГаснетПослеПервогоПредъявления(t *testing.T) {
	// Код годен три окна подряд: без гашения подсмотренный через плечо код
	// работает второй раз.
	g := newGuard()
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	if !g.consume("мастер", "123456", at) {
		t.Fatal("код не принят с первого раза")
	}
	if g.consume("мастер", "123456", at.Add(time.Second)) {
		t.Fatal("тот же код прошёл второй раз")
	}
	if !g.consume("другой", "123456", at.Add(time.Second)) {
		t.Fatal("код погас у чужого имени: коды у разных людей совпадают сами собой")
	}
	if !g.consume("мастер", "123456", at.Add(guardCodeLife+time.Second)) {
		t.Fatal("код помнится дольше, чем живёт: память сторожа только растёт")
	}
}
