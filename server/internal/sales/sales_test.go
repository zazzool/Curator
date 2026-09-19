package sales

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"curator/server/internal/dbgate"
)

func testGate(t *testing.T) *dbgate.Gate {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут. " +
			"Это отказ, а не пропуск — см. server/.env.example")
	}
	gate, err := dbgate.Open(context.Background(), dsn, 0)
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

func врач(t *testing.T, gate *dbgate.Gate) int64 {
	t.Helper()
	var id int64
	if err := gate.QueryRow(context.Background(),
		`INSERT INTO accounts DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("учётная запись не заведена: %v", err)
	}
	return id
}

func набор(t *testing.T, gate *dbgate.Gate) string {
	t.Helper()
	slug := fmt.Sprintf("prodazha-%d-%d", time.Now().UnixNano(), rand.IntN(1000))
	if _, err := gate.Exec(context.Background(),
		`INSERT INTO packs (slug, title, status) VALUES ($1, $2, 'published')`,
		slug, "Набор "+slug); err != nil {
		t.Fatalf("набор не заведён: %v", err)
	}
	return slug
}

func ключ() string {
	return fmt.Sprintf("к-%d-%d", time.Now().UnixNano(), rand.IntN(100000))
}

func TestPgНаборБезЦеныОткрытВсем(t *testing.T) {
	// Правило названо вслух: молчаливое «нет цены — значит закрыто»
	// закрыло бы всё, что составитель ещё не оценил, и он узнал бы об этом
	// от врача.
	gate := testGate(t)
	access := NewAccess(gate)
	slug := набор(t, gate)

	ok, err := access.Allowed(context.Background(), врач(t, gate), slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("набор без цены закрыт")
	}
}

func TestPgПлатныйНаборЗакрытПокаНеКуплен(t *testing.T) {
	gate := testGate(t)
	access, payments, prices := NewAccess(gate), NewPayments(gate), NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)
	id := врач(t, gate)

	if err := prices.Set(ctx, "pack:"+slug, 39900, true); err != nil {
		t.Fatal(err)
	}
	ok, err := access.Allowed(ctx, id, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("платный набор открыт без покупки")
	}

	if _, err := payments.Accept(ctx, Income{
		AccountID: id, Purpose: "pack:" + slug, Kopecks: 39900,
		IdemKey: ключ(), By: "оператор",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	ok, err = access.Allowed(ctx, id, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("купленный набор остался закрытым")
	}
}

func TestPgПодпискаОткрываетВсеНаборы(t *testing.T) {
	gate := testGate(t)
	access, payments, prices := NewAccess(gate), NewPayments(gate), NewPrices(gate)
	ctx := context.Background()
	первый, второй := набор(t, gate), набор(t, gate)
	id := врач(t, gate)

	for _, slug := range []string{первый, второй} {
		if err := prices.Set(ctx, "pack:"+slug, 39900, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := payments.Accept(ctx, Income{
		AccountID: id, Purpose: "subscription:month", Kopecks: 99000,
		IdemKey: ключ(), By: "оператор",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	for _, slug := range []string{первый, второй} {
		ok, err := access.Allowed(ctx, id, slug, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("подписка не открыла набор %s", slug)
		}
	}
}

func TestPgИстёкшаяПодпискаНеОткрываетНичего(t *testing.T) {
	gate := testGate(t)
	access, payments, prices := NewAccess(gate), NewPayments(gate), NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)
	id := врач(t, gate)
	if err := prices.Set(ctx, "pack:"+slug, 39900, true); err != nil {
		t.Fatal(err)
	}

	куплено := time.Now().Add(-40 * 24 * time.Hour)
	if _, err := payments.Accept(ctx, Income{
		AccountID: id, Purpose: "subscription:month", Kopecks: 99000,
		IdemKey: ключ(), By: "оператор",
	}, куплено); err != nil {
		t.Fatal(err)
	}

	ok, err := access.Allowed(ctx, id, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("месячная подписка, купленная сорок дней назад, всё ещё открывает наборы")
	}
}

func TestPgПодпискаПродлеваетсяОтКонцаПрежней(t *testing.T) {
	// Купивший второй месяц за неделю до конца первого иначе потерял бы
	// неделю, за которую уже заплатил.
	gate := testGate(t)
	access, payments := NewAccess(gate), NewPayments(gate)
	ctx := context.Background()
	id := врач(t, gate)

	первый := time.Now().Add(-23 * 24 * time.Hour)
	if _, err := payments.Accept(ctx, Income{
		AccountID: id, Purpose: "subscription:month", Kopecks: 99000,
		IdemKey: ключ(), By: "оператор",
	}, первый); err != nil {
		t.Fatal(err)
	}
	if _, err := payments.Accept(ctx, Income{
		AccountID: id, Purpose: "subscription:month", Kopecks: 99000,
		IdemKey: ключ(), By: "оператор",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	list, err := access.Live(ctx, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var последний time.Time
	for _, one := range list {
		if one.Kind == "subscription" && one.ExpiresAt != nil && one.ExpiresAt.After(последний) {
			последний = *one.ExpiresAt
		}
	}
	// Первый месяц кончался через семь суток; второй обязан лечь на него,
	// а не начаться сегодня.
	ожидалось := первый.Add(60 * 24 * time.Hour)
	if разница := последний.Sub(ожидалось); разница > time.Minute || разница < -time.Minute {
		t.Errorf("подписка кончается %v, а от конца прежней вышло бы %v",
			последний.UTC(), ожидалось.UTC())
	}
}

func TestPgПовторНеОформляетВторойПлатёж(t *testing.T) {
	// Оператор нажал дважды, а не принял деньги дважды.
	gate := testGate(t)
	payments, prices := NewPayments(gate), NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)
	id := врач(t, gate)
	if err := prices.Set(ctx, "pack:"+slug, 39900, true); err != nil {
		t.Fatal(err)
	}

	key := ключ()
	in := Income{AccountID: id, Purpose: "pack:" + slug, Kopecks: 39900,
		IdemKey: key, By: "оператор"}

	first, err := payments.Accept(ctx, in, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if first.Repeated {
		t.Fatal("первый приход помечен повтором")
	}
	second, err := payments.Accept(ctx, in, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !second.Repeated {
		t.Error("повтор с тем же ключом оформился как новый приход")
	}
	if second.ID != first.ID {
		t.Errorf("повтор вернул другой платёж: %d вместо %d", second.ID, first.ID)
	}

	list, err := payments.Recent(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("платежей у врача %d, ожидался один", len(list))
	}
}

func TestPgВозвратОтзываетПравоВыданноеЭтимПлатежом(t *testing.T) {
	gate := testGate(t)
	access, payments, prices := NewAccess(gate), NewPayments(gate), NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)
	id := врач(t, gate)
	if err := prices.Set(ctx, "pack:"+slug, 39900, true); err != nil {
		t.Fatal(err)
	}

	out, err := payments.Accept(ctx, Income{
		AccountID: id, Purpose: "pack:" + slug, Kopecks: 39900,
		IdemKey: ключ(), By: "оператор",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := payments.Refund(ctx, out.ID, "врач передумал", "оператор", time.Now()); err != nil {
		t.Fatal(err)
	}

	ok, err := access.Allowed(ctx, id, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("после возврата набор остался открытым")
	}

	// Отзыв — событие, и строка о праве остаётся: удали мы её, разобрать
	// потом, за что были деньги, стало бы нечем.
	var count int
	if err := gate.QueryRow(ctx,
		`SELECT count(*) FROM entitlements WHERE payment_id = $1`, out.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("строк о праве по платежу %d, ожидалась одна", count)
	}
}

func TestPgПовторныйВозвратОтказывает(t *testing.T) {
	// «Вернули» платёж, которого нет или который уже возвращён, оператор
	// примет за правду.
	gate := testGate(t)
	payments, prices := NewPayments(gate), NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)
	if err := prices.Set(ctx, "pack:"+slug, 39900, true); err != nil {
		t.Fatal(err)
	}
	out, err := payments.Accept(ctx, Income{
		AccountID: врач(t, gate), Purpose: "pack:" + slug, Kopecks: 39900,
		IdemKey: ключ(), By: "оператор",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := payments.Refund(ctx, out.ID, "", "оператор", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := payments.Refund(ctx, out.ID, "", "оператор", time.Now()); err == nil {
		t.Error("второй возврат того же платежа прошёл молча")
	}
}

func TestPgПриходБезИмениИНаНольОтказывает(t *testing.T) {
	gate := testGate(t)
	payments := NewPayments(gate)
	ctx := context.Background()
	slug := набор(t, gate)
	id := врач(t, gate)

	if _, err := payments.Accept(ctx, Income{
		AccountID: id, Purpose: "pack:" + slug, Kopecks: 39900, IdemKey: ключ(),
	}, time.Now()); err == nil {
		t.Error("приход оформился без имени оформившего")
	}
	if _, err := payments.Accept(ctx, Income{
		AccountID: id, Purpose: "pack:" + slug, Kopecks: 0,
		IdemKey: ключ(), By: "оператор",
	}, time.Now()); err == nil {
		t.Error("приход на ноль принят")
	}
}

func TestНазначениеПлатежаЗакрытоОбразцом(t *testing.T) {
	// Назначение с опечаткой не найдётся ни одним отчётом и будет
	// выглядеть отсутствующим.
	for _, good := range []string{"pack:cardio-basics", "subscription:month", "subscription:year"} {
		if _, err := ParsePurpose(good); err != nil {
			t.Errorf("годное назначение %q отвергнуто: %v", good, err)
		}
	}
	for _, bad := range []string{"", "pack:", "pack:Кириллица", "pack:-минус",
		"subscription:week", "подписка:месяц", "pack cardio"} {
		if _, err := ParsePurpose(bad); err == nil {
			t.Errorf("негодное назначение %q принято", bad)
		}
	}
}

func TestPgЦенаВНольНеПринимается(t *testing.T) {
	// Ноль выглядит ценой и таковой не является. Бесплатный набор делается
	// выключением цены, и это видно в студии.
	gate := testGate(t)
	if err := NewPrices(gate).Set(context.Background(), "pack:"+набор(t, gate), 0, true); err == nil {
		t.Error("цена в ноль принята")
	}
}

func TestPgВыключеннаяЦенаДелаетНаборБесплатным(t *testing.T) {
	gate := testGate(t)
	access, prices := NewAccess(gate), NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)
	id := врач(t, gate)

	if err := prices.Set(ctx, "pack:"+slug, 39900, true); err != nil {
		t.Fatal(err)
	}
	if ok, _ := access.Allowed(ctx, id, slug, time.Now()); ok {
		t.Fatal("платный набор открыт без покупки")
	}
	if err := prices.Set(ctx, "pack:"+slug, 39900, false); err != nil {
		t.Fatal(err)
	}
	ok, err := access.Allowed(ctx, id, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("набор с выключенной ценой остался закрытым")
	}
}
