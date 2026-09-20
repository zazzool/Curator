//go:build audit

// Проверки, показывающие найденные дефекты. Как их звать и почему они
// лежат отдельно — в doc.go.
package audit

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/gen"
	"curator/server/internal/sales"
	"curator/server/internal/studio"
)

func gate(t *testing.T) *dbgate.Gate {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: показывать дефекты не на чем")
	}
	g, err := dbgate.Open(context.Background(), dsn, 0)
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(g.Close)
	return g
}

// СЕР-1. Продление подписки читает конец действующей и пишет новую двумя
// отдельными запросами, без замка на записи врача. Два платежа, пришедшие
// разом, видят одно и то же «до» и оба считают от него.
//
// Цена: врач заплатил за два месяца и получил один.
func TestДефектПодпискаДваПлатежаОдновременно(t *testing.T) {
	ctx := context.Background()
	g := gate(t)
	pay := sales.NewPayments(g)
	const rounds = 40
	bad := 0

	for round := 0; round < rounds; round++ {
		var account int64
		if err := g.QueryRow(ctx, `INSERT INTO accounts DEFAULT VALUES RETURNING id`).Scan(&account); err != nil {
			t.Fatal(err)
		}
		now := time.Now()

		// Общий старт: без него горутины успевают разойтись во времени, и
		// соперничество, которое и есть дефект, просто не случается.
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, err := pay.Accept(ctx, sales.Income{
					AccountID: account, Purpose: "subscription:month", Kopecks: 199000,
					IdemKey: fmt.Sprintf("аудит-%d-%d", round, i), By: "аудит",
				}, now)
				if err != nil {
					t.Errorf("платёж не принят: %v", err)
				}
			}(i)
		}
		close(start)
		wg.Wait()

		var until time.Time
		if err := g.QueryRow(ctx, `SELECT max(expires_at) FROM entitlements
		     WHERE account_id = $1 AND kind = 'subscription'`, account).Scan(&until); err != nil {
			t.Fatal(err)
		}
		if days := until.Sub(now).Hours() / 24; days < 59 {
			bad++
			if bad == 1 {
				t.Logf("круг %d: оплачено два месяца, выдано %.0f суток", round, days)
			}
		}
	}
	if bad > 0 {
		t.Errorf("из %d пар одновременных оплат %d дали один месяц вместо двух", rounds, bad)
	}
}

// СЕР-2. Ключ повторности платежа уникален сам по себе, а не в паре с
// врачом и назначением. Повтор отдаёт ПРЕЖНИЙ платёж, не сверив, тот ли
// это врач и та ли сумма.
//
// Цена: второй врач заплатил и не получил ничего, а оператор увидел
// чужой приход со словами «уже оформлен».
func TestДефектКлючПовторностиНеПривязанКВрачу(t *testing.T) {
	ctx := context.Background()
	g := gate(t)
	pay := sales.NewPayments(g)

	var первый, второй int64
	if err := g.QueryRow(ctx, `INSERT INTO accounts DEFAULT VALUES RETURNING id`).Scan(&первый); err != nil {
		t.Fatal(err)
	}
	if err := g.QueryRow(ctx, `INSERT INTO accounts DEFAULT VALUES RETURNING id`).Scan(&второй); err != nil {
		t.Fatal(err)
	}

	key := "приход-" + time.Now().Format("20060102-150405.000000")
	now := time.Now()

	if _, err := pay.Accept(ctx, sales.Income{AccountID: первый, Purpose: "subscription:year",
		Kopecks: 249000, IdemKey: key, By: "оператор"}, now); err != nil {
		t.Fatal(err)
	}
	ответ, err := pay.Accept(ctx, sales.Income{AccountID: второй, Purpose: "subscription:month",
		Kopecks: 39900, IdemKey: key, By: "оператор"}, now)
	if err != nil {
		t.Fatal(err)
	}

	var прав int
	if err := g.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE account_id = $1`,
		второй).Scan(&прав); err != nil {
		t.Fatal(err)
	}
	if прав == 0 {
		t.Errorf("второй врач заплатил и остался без прав; "+
			"оператору отдан приход врача %d на %d коп. с пометкой «повтор»=%v",
			ответ.AccountID, ответ.Kopecks, ответ.Repeated)
	}
}

// СЕР-3. Оберег «не снимай право мастерской с последнего» считает
// оставшихся и правит двумя отдельными запросами, без замка. Два снятия,
// пришедшие разом, видят друг друга живыми и проходят оба.
//
// Цена: в студию некому войти, и лечится это руками на контуре.
func TestДефектПоследнийМастерСнимаетсяДвумяСразу(t *testing.T) {
	ctx := context.Background()
	g := gate(t)
	users := studio.NewUsers(g)
	const rounds = 40
	bad := 0

	for round := 0; round < rounds; round++ {
		stamp := time.Now().UnixNano()
		a := fmt.Sprintf("аудит-a-%d-%d", round, stamp)
		b := fmt.Sprintf("аудит-b-%d-%d", round, stamp)
		for _, login := range []string{a, b} {
			if _, _, err := users.Create(ctx, login, login,
				[]studio.Permission{studio.PermWorkshop}); err != nil {
				t.Fatal(err)
			}
		}
		// Кроме этих двоих мастеров в базе быть не должно, иначе оберег
		// и не обязан срабатывать.
		if _, err := g.Exec(ctx, `UPDATE users SET disabled_at = NOW()
		     WHERE login <> $1 AND login <> $2 AND 'workshop' = ANY (permissions)`, a, b); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, login := range []string{a, b} {
			wg.Add(1)
			go func(login string) {
				defer wg.Done()
				<-start
				_ = users.SetPermissions(ctx, login, []studio.Permission{studio.PermCaseRead})
			}(login)
		}
		close(start)
		wg.Wait()

		var осталось int
		if err := g.QueryRow(ctx, `SELECT count(*) FROM users
		     WHERE disabled_at IS NULL AND 'workshop' = ANY (permissions)`).Scan(&осталось); err != nil {
			t.Fatal(err)
		}
		if осталось == 0 {
			bad++
			if bad == 1 {
				t.Logf("круг %d: права мастерской не осталось ни у кого", round)
			}
			if _, err := g.Exec(ctx,
				`UPDATE users SET permissions = ARRAY['workshop']::TEXT[] WHERE login = $1`, a); err != nil {
				t.Fatal(err)
			}
		}
	}
	if bad > 0 {
		t.Errorf("из %d кругов %d заперли студию", rounds, bad)
	}
}

// СЕР-4. Подстановка переменных в задание модели перебирает карту, а
// порядок обхода карты в Go случаен по замыслу. Значение одной переменной,
// содержащее имя другой, раскрывается или нет — как повезёт на этом круге.
//
// Цена: одно и то же задание уходит модели двумя разными текстами, и
// запись обращения в llm_calls перестаёт быть следом того, что было.
// Текст переменной приезжает из загруженного документа, то есть подставить
// туда имя другой переменной может составитель, сам того не желая.
func TestДефектПодстановкаПеременныхНеОднозначна(t *testing.T) {
	plan := gen.Plan{StatementsMd: "ПОЛОЖЕНИЯ ИСТОЧНИКА"}
	plan.Unit.Title = "{положения}" // так назвали единицу в документе

	seen := map[string]int{}
	for i := 0; i < 400; i++ {
		seen[gen.Render("Название единицы: {название}", plan)]++
	}
	if len(seen) > 1 {
		for text, n := range seen {
			t.Logf("%3d раз из 400: %q", n, text)
		}
		t.Errorf("один и тот же заказ дал %d разных заданий модели", len(seen))
	}
}
