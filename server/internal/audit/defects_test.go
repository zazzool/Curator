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
	"curator/server/internal/studio"
)

func gate(t *testing.T) *dbgate.Gate {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: показывать дефекты не на чем")
	}
	g, err := dbgate.Open(context.Background(), dsn, dbgate.Options{})
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(g.Close)
	return g
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
