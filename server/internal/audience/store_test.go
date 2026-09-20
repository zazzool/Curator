package audience

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"curator/server/internal/dbgate"
)

// Проверки групп на живой базе.
//
// На живой, а не в памяти: правило считается по портрету, а портрет — это
// запрос. Зелёная проверка в памяти означала бы, что сошлись две наши
// выдумки, тогда как расходятся здесь ровно запрос и правило.

func testGate(t *testing.T) *dbgate.Gate {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут. " +
			"Это отказ, а не пропуск — см. server/.env.example")
	}
	gate, err := dbgate.Open(context.Background(), dsn, dbgate.Options{})
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

func метка(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), rand.IntN(100000))
}

// врач заводит учётную запись и отдаёт её номер.
func врач(t *testing.T, gate *dbgate.Gate) int64 {
	t.Helper()
	var id int64
	if err := gate.QueryRow(context.Background(),
		`INSERT INTO accounts DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("учётная запись не заведена: %v", err)
	}
	return id
}

// почта делает врача «авторизованным».
func почта(t *testing.T, gate *dbgate.Gate, account int64) {
	t.Helper()
	if _, err := gate.Exec(context.Background(),
		`UPDATE accounts SET email = 'vrach-' || id || '@example.ru' WHERE id = $1`,
		account); err != nil {
		t.Fatalf("почта не привязана: %v", err)
	}
}

// попытка кладёт решение задачи, случившееся столько-то суток назад.
func попытка(t *testing.T, gate *dbgate.Gate, account int64, верно bool, давность time.Duration) {
	t.Helper()
	if _, err := gate.Exec(context.Background(), `
		INSERT INTO attempts (account_id, case_id, correct, happened_at)
		VALUES ($1, $2, $3, NOW() - $4::interval)`,
		account, метка("c"), верно, fmt.Sprintf("%d seconds", int(давность.Seconds()))); err != nil {
		t.Fatalf("попытка не записана: %v", err)
	}
}

// набор заводит выпущенный набор названной линейки и отдаёт его номер.
func набор(t *testing.T, gate *dbgate.Gate, line string) int64 {
	t.Helper()
	slug := метка("gruppy")
	var id int64
	if err := gate.QueryRow(context.Background(),
		`INSERT INTO packs (slug, title, status, line)
		 VALUES ($1, $2, 'published', $3) RETURNING id`,
		slug, "Набор "+slug, line).Scan(&id); err != nil {
		t.Fatalf("набор не заведён: %v", err)
	}
	return id
}

// группа заводит группу с правилом и отдаёт её метку.
func группа(t *testing.T, store *Store, rule Rule) string {
	t.Helper()
	slug := метка("gruppa")
	if err := store.Create(context.Background(), slug, "Группа "+slug, "", rule); err != nil {
		t.Fatalf("группа не заведена: %v", err)
	}
	return slug
}

func TestPgПризнакиСчитаютсяИзТогоЧтоУжеПишется(t *testing.T) {
	// Главная проверка наряда: правило применяется в Go, а портрет
	// считается запросом, и разойтись они могут только здесь. Каждый
	// опыт ниже держится на двух половинах — «попал» и «не попал», —
	// потому что «попали все» зелено ровно так же, как «попал нужный».
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()
	now := time.Now()

	назвавшийся := врач(t, gate)
	почта(t, gate, назвавшийся)
	промолчавший := врач(t, gate)

	прилежный := врач(t, gate)
	for i := 0; i < 10; i++ {
		// Разные сутки, чтобы дни занятий считались днями, а не
		// попытками: решивший триста задач за вечер и решающий по десять
		// каждый день — разные врачи.
		попытка(t, gate, прилежный, i < 8, time.Duration(i)*24*time.Hour)
	}
	новичок := врач(t, gate)
	попытка(t, gate, новичок, false, time.Hour)

	for _, случай := range []struct {
		имя     string
		правило Rule
		ждём    map[int64]bool
	}{
		{
			"почта привязана",
			Rule{{Trait: TraitEmail}},
			map[int64]bool{назвавшийся: true, промолчавший: false},
		},
		{
			"почта не привязана",
			Rule{{Trait: TraitEmail, Not: true}},
			map[int64]bool{назвавшийся: false, промолчавший: true},
		},
		{
			"решил не меньше пяти",
			Rule{{Trait: TraitSolved, N: 5}},
			map[int64]bool{прилежный: true, новичок: false},
		},
		{
			"доля верных не ниже восьмидесяти при пяти решённых",
			Rule{{Trait: TraitAccuracy, N: 80}, {Trait: TraitSolved, N: 5}},
			map[int64]bool{прилежный: true, новичок: false, промолчавший: false},
		},
		{
			"занимался не меньше пяти дней за тридцать суток",
			Rule{{Trait: TraitActiveDays, N: 5, Over: Window30}},
			map[int64]bool{прилежный: true, новичок: false},
		},
		{
			"за неделю прилежным уже не выглядит: окно считается своё",
			Rule{{Trait: TraitActiveDays, N: 9, Over: Window7}},
			map[int64]bool{прилежный: false},
		},
	} {
		slug := группа(t, store, случай.правило)
		for account, ждём := range случай.ждём {
			p, err := store.Portrait(ctx, account, now, случай.правило.NeedsBehaviour())
			if err != nil {
				t.Fatalf("%s: портрет врача %d не собран: %v", случай.имя, account, err)
			}
			if got := случай.правило.Matches(p); got != ждём {
				t.Errorf("%s: врач %d попадает = %v, ждали %v (портрет %+v)",
					случай.имя, account, got, ждём, p)
			}
		}
		if _, err := store.Size(ctx, slug); err != nil {
			t.Errorf("%s: счёт группы не сошёлся: %v", случай.имя, err)
		}
	}
}

func TestPgПоимённыйСписокДополняетПравило(t *testing.T) {
	// Кафедре правила не подобрать: общего признака у её ординаторов нет,
	// и выдумывать его пришлось бы ради механизма, а не ради дела.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()

	ординатор := врач(t, gate)
	чужой := врач(t, gate)
	// Правило пустое, и это НЕ «все»: группа держится списком.
	slug := группа(t, store, Rule{})

	если(t, store.AddMember(ctx, slug, ординатор, "operator"))
	// Повторное добавление не отказ: оператор, добавляющий кафедру
	// списком, не должен спотыкаться на том, кто уже добавлен.
	если(t, store.AddMember(ctx, slug, ординатор, "operator"))

	мои, err := store.Membership(ctx, ординатор, time.Now())
	если(t, err)
	one, err := store.One(ctx, slug)
	если(t, err)
	if !входит(мои, one.ID) {
		t.Error("названный поимённо в группу не попал")
	}

	чужие, err := store.Membership(ctx, чужой, time.Now())
	если(t, err)
	if входит(чужие, one.ID) {
		t.Error("в группу с пустым правилом попал непричастный: пустое правило принято за «все»")
	}

	// Опечатка в номере — отказ, а не молчаливый успех: оператор иначе
	// решит, что врач добавлен, и узнает обратное от самого врача.
	if err := store.AddMember(ctx, slug, 0, "operator"); err == nil {
		t.Error("врач с несуществующим номером добавлен")
	}

	если(t, store.DropMember(ctx, slug, ординатор))
	после, err := store.Membership(ctx, ординатор, time.Now())
	если(t, err)
	if входит(после, one.ID) {
		t.Error("убранный из списка остался в группе")
	}
}

func TestPgСвязиНабораПравятсяЦеликом(t *testing.T) {
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()

	packID := набор(t, gate, "paid")
	var slug string
	если(t, gate.QueryRow(ctx, `SELECT slug FROM packs WHERE id = $1`, packID).Scan(&slug))

	кафедра := группа(t, store, Rule{})
	уснувшие := группа(t, store, Rule{{Trait: TraitIdle, N: 90}})

	если(t, store.SetPack(ctx, slug, []Bound{
		{Slug: кафедра, Mode: ModeOpen},
		{Slug: уснувшие, Mode: ModeHidden},
	}))
	связи, err := store.OfPack(ctx, slug)
	если(t, err)
	if len(связи) != 2 {
		t.Fatalf("связей %d, ждали 2: %+v", len(связи), связи)
	}

	// Правка целиком: названное заново вытесняет прежнее. Иначе «кому
	// открыт» правилось бы чередой мелких шагов, из которых итог не виден
	// тому, кто их делает.
	если(t, store.SetPack(ctx, slug, []Bound{{Slug: кафедра, Mode: ModeOpen}}))
	связи, err = store.OfPack(ctx, slug)
	если(t, err)
	if len(связи) != 1 || связи[0].Slug != кафедра {
		t.Fatalf("прежние связи не сняты: %+v", связи)
	}

	// Опечатка в метке — отказ на всю правку, а не пропуск строки: иначе
	// «сохранено» вернулось бы при потерянной половине списка.
	err = store.SetPack(ctx, slug, []Bound{
		{Slug: кафедра, Mode: ModeOpen},
		{Slug: "takoj-gruppy-net", Mode: ModeOpen},
	})
	if err == nil {
		t.Fatal("правка с несуществующей группой принята")
	}
	связи, _ = store.OfPack(ctx, slug)
	if len(связи) != 1 {
		t.Errorf("отказавшая правка что-то изменила: %+v", связи)
	}

	// Одна группа не бывает набору и открывающей, и скрывающей сразу.
	if err := store.SetPack(ctx, slug, []Bound{
		{Slug: кафедра, Mode: ModeOpen},
		{Slug: кафедра, Mode: ModeHidden},
	}); err == nil {
		t.Error("группа принята сразу открывающей и скрывающей")
	}
	if err := store.SetPack(ctx, slug, []Bound{{Slug: кафедра, Mode: "naverno"}}); err == nil {
		t.Error("непонятый род связи принят")
	}

	// Группу, которой что-то открыто, не убрать: удаление молча сменило
	// бы состав корпуса у всех её членов.
	if err := store.Drop(ctx, кафедра); err == nil {
		t.Error("привязанная к набору группа убрана")
	}
	если(t, store.SetPack(ctx, slug, nil))
	если(t, store.Drop(ctx, кафедра))
	если(t, store.Drop(ctx, уснувшие))
}

func TestPgСломанноеПравилоНеПрименяетсяНиВТуНиВДругуюСторону(t *testing.T) {
	// Правило, записанное прежней редакцией сервера или правленное
	// руками в базе, не должно ни раздавать, ни отнимать: применить его
	// «как открывающее» значило бы раздать по непонятому, «как
	// скрывающее» — отнять по нему же. Набор остаётся при своей линейке,
	// а студия показывает поломку словами.
	gate := testGate(t)
	store := NewStore(gate)
	ctx := context.Background()

	slug := группа(t, store, Rule{})
	если(t, exec(gate, `UPDATE audiences SET rule = '[{"trait":"vip"}]'::jsonb WHERE slug = $1`, slug))

	one, err := store.One(ctx, slug)
	если(t, err)
	if one.Broken == "" {
		t.Fatal("сломанное правило прочиталось как исправное")
	}
	if !strings.Contains(one.Broken, "vip") {
		t.Errorf("в словах о поломке нет самого признака: %s", one.Broken)
	}

	// Список не роняется: группа приезжает со словами о поломке, а не
	// исчезает — исчезнувшая выглядела бы как несуществующая.
	все, err := store.All(ctx)
	если(t, err)
	if !нашлась(все, slug) {
		t.Error("сломанная группа пропала из списка")
	}

	// И никому ничего не открывает.
	кто := врач(t, gate)
	мои, err := store.Membership(ctx, кто, time.Now())
	если(t, err)
	if входит(мои, one.ID) {
		t.Error("сломанное правило кого-то впустило")
	}

	если(t, store.Drop(ctx, slug))
}

func если(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func exec(gate *dbgate.Gate, sql string, args ...any) error {
	_, err := gate.Exec(context.Background(), sql, args...)
	return err
}

func входит(ids []int64, id int64) bool {
	for _, one := range ids {
		if one == id {
			return true
		}
	}
	return false
}

func нашлась(groups []Group, slug string) bool {
	for _, one := range groups {
		if one.Slug == slug {
			return true
		}
	}
	return false
}
