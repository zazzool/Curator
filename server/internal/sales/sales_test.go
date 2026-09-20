package sales

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/packs"
)

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

// наборЛинейки заводит набор названной линейки.
func наборЛинейки(t *testing.T, gate *dbgate.Gate, line string) string {
	t.Helper()
	slug := fmt.Sprintf("liniya-%d-%d", time.Now().UnixNano(), rand.IntN(1000))
	if _, err := gate.Exec(context.Background(),
		`INSERT INTO packs (slug, title, status, line) VALUES ($1, $2, 'published', $3)`,
		slug, "Набор "+slug, line); err != nil {
		t.Fatalf("набор не заведён: %v", err)
	}
	return slug
}

// привязатьПочту делает врача «авторизованным».
//
// Учётная запись заводится молча при первом запуске, и отличает
// назвавшегося от промолчавшего только почта — на ней и стоит базовая
// линейка.
func привязатьПочту(t *testing.T, gate *dbgate.Gate, id int64) {
	t.Helper()
	if _, err := gate.Exec(context.Background(),
		`UPDATE accounts SET email = $2 WHERE id = $1`,
		id, fmt.Sprintf("vrach-%d@example.ru", id)); err != nil {
		t.Fatalf("почта не привязана: %v", err)
	}
}

func ключ() string {
	return fmt.Sprintf("к-%d-%d", time.Now().UnixNano(), rand.IntN(100000))
}

func TestPgГостевойНаборОткрытБезЕдинойСтрокиПрав(t *testing.T) {
	// Врач, впервые открывший приложение, ещё никто: учётная запись
	// заведена молча, почты нет, прав нет. Закройся гостевая линейка — и
	// первый экран приложения был бы пуст, а пустой первый экран это
	// удаление приложения.
	gate := testGate(t)
	access := NewAccess(gate)
	slug := наборЛинейки(t, gate, packs.LineGuest)

	ok, err := access.Allowed(context.Background(), врач(t, gate), slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("гостевой набор закрыт от того, ради кого он и заведён")
	}
}

func TestPgБазовыйНаборОткрываетсяПривязаннойПочтой(t *testing.T) {
	// Обе половины сразу: «закрытого не видно» зелено и тогда, когда не
	// видно ничего, а «открытое видно» — и тогда, когда видно всё подряд.
	gate := testGate(t)
	access := NewAccess(gate)
	ctx := context.Background()
	slug := наборЛинейки(t, gate, packs.LineBasic)
	id := врач(t, gate)

	if ok, _ := access.Allowed(ctx, id, slug, time.Now()); ok {
		t.Fatal("базовый набор открыт тому, кто не назвался")
	}
	привязатьПочту(t, gate, id)
	ok, err := access.Allowed(ctx, id, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("привязавший почту не получил базового набора")
	}
}

func TestPgСпонсорскийНаборОткрытВсемИБесплатно(t *testing.T) {
	// За спонсорский набор уже заплатили, и второй раз — деньгами врача —
	// за него не платят никогда. Стена входа перед ним превратила бы
	// подарок спонсора в приманку.
	gate := testGate(t)
	access := NewAccess(gate)
	slug := наборЛинейки(t, gate, packs.LineSponsored)

	ok, err := access.Allowed(context.Background(), врач(t, gate), slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("спонсорский набор закрыт")
	}
}

func TestPgВыключеннаяЦенаБольшеНеОткрываетПлатныйНабор(t *testing.T) {
	// Прежде правилом было «нет действующей цены — набор бесплатен», и
	// проверка на это здесь стояла. Правило держалось ровно до тех пор,
	// пока граница бесплатного не была названа: теперь её несёт линейка,
	// а цена отвечает на другой вопрос — почём продаётся, а не кому
	// открыто. Оставь мы оба правила, выключенная цена открывала бы
	// платный набор всем, а линейка при этом говорила бы «платный»: два
	// источника правды об одном, и расходятся они молча.
	//
	// Бесплатным набор делается линейкой sponsored, и это видно в студии
	// словом, а не отсутствием числа.
	gate := testGate(t)
	access, prices := NewAccess(gate), NewPrices(gate)
	ctx := context.Background()
	slug := наборЛинейки(t, gate, packs.LinePaid)
	id := врач(t, gate)
	привязатьПочту(t, gate, id)

	редакция, err := prices.Set(ctx, "проверка", "pack:"+slug, 39900, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := access.Allowed(ctx, id, slug, time.Now()); ok {
		t.Fatal("платный набор открыт без покупки")
	}
	if _, err := prices.Set(ctx, "проверка", "pack:"+slug, 39900, false, редакция); err != nil {
		t.Fatal(err)
	}
	if ok, _ := access.Allowed(ctx, id, slug, time.Now()); ok {
		t.Error("выключенная цена открыла платный набор: " +
			"снятое с продажи не то же самое, что подаренное")
	}
}

func TestPgНабораКоторогоНетНеОткрытНикому(t *testing.T) {
	// Отвечать «открыт» на несуществующий набор значило бы пустить
	// выгрузку дальше — к набору, которого нет, — и разбирать потом отказ
	// на шаг позже того места, где он случился.
	gate := testGate(t)
	ok, err := NewAccess(gate).Allowed(
		context.Background(), врач(t, gate), "takogo-nabora-net", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("набор, которого нет, объявлен открытым")
	}
}

func TestPgПлатныйНаборЗакрытПокаНеКуплен(t *testing.T) {
	gate := testGate(t)
	access, payments, prices := NewAccess(gate), NewPayments(gate), NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)
	id := врач(t, gate)

	if _, err := prices.Set(ctx, "проверка", "pack:"+slug, 39900, true, 0); err != nil {
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
		if _, err := prices.Set(ctx, "проверка", "pack:"+slug, 39900, true, 0); err != nil {
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
	if _, err := prices.Set(ctx, "проверка", "pack:"+slug, 39900, true, 0); err != nil {
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
	if _, err := prices.Set(ctx, "проверка", "pack:"+slug, 39900, true, 0); err != nil {
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
	if _, err := prices.Set(ctx, "проверка", "pack:"+slug, 39900, true, 0); err != nil {
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
	if _, err := prices.Set(ctx, "проверка", "pack:"+slug, 39900, true, 0); err != nil {
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
	if _, err := NewPrices(gate).Set(context.Background(), "проверка", "pack:"+набор(t, gate), 0, true, 0); err == nil {
		t.Error("цена в ноль принята")
	}
}

func врачСПочтой(t *testing.T, gate *dbgate.Gate, email, name string) int64 {
	t.Helper()
	var id int64
	if err := gate.QueryRow(context.Background(),
		`INSERT INTO accounts (email, display_name) VALUES ($1, $2) RETURNING id`,
		email, name).Scan(&id); err != nil {
		t.Fatalf("учётная запись не заведена: %v", err)
	}
	return id
}

func TestPgКлиентНаходитсяПоПочтеИмениИНомеру(t *testing.T) {
	// Приход оформляется на номер учётной записи, а взять его было негде:
	// оператор, которому врач написал с почты, не мог найти его вовсе.
	gate := testGate(t)
	clients := NewClients(gate)
	ctx := context.Background()
	метка := fmt.Sprintf("%d%d", time.Now().UnixNano(), rand.IntN(1000))
	id := врачСПочтой(t, gate, "ivanov-"+метка+"@example.ru", "Иванов И.И. "+метка)

	for _, запрос := range []string{"ivanov-" + метка, "Иванов И.И. " + метка} {
		found, err := clients.Find(ctx, запрос, 0)
		if err != nil {
			t.Fatalf("поиск %q: %v", запрос, err)
		}
		if len(found) != 1 || found[0].ID != id {
			t.Errorf("по %q нашлось %d записей, а заводили одну", запрос, len(found))
		}
	}

	// Номер ищется точным равенством. Подстрокой «7» нашла бы и 17-го, и
	// 70-го, а оператор, которому врач назвал номер, ждёт одну строку.
	found, err := clients.Find(ctx, fmt.Sprint(id), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != id {
		t.Errorf("по номеру нашлось %d записей", len(found))
	}
}

func TestPgПоискПоНомеруНеЛовитЦифрыИзЧужойПочты(t *testing.T) {
	// Сторож поймал это на живой базе: поиск по номеру нашёл две записи.
	// Цифры живут и в почте (ivanov1985@…), и в имени, и «номер ИЛИ
	// подстрока» возвращает номер плюс всех, у кого эти цифры где-нибудь
	// встретились. Оператор, которому врач назвал свой номер, получает
	// список и не знает, кому оформлять приход.
	gate := testGate(t)
	clients := NewClients(gate)
	ctx := context.Background()

	метка := fmt.Sprintf("%d%d", time.Now().UnixNano(), rand.IntN(1000))
	id := врачСПочтой(t, gate, "ivanov-"+метка+"@example.ru", "Иванов "+метка)

	// Второй врач, у которого номер первого попал в почту и в имя.
	врачСПочтой(t, gate,
		fmt.Sprintf("petrov%d-%s@example.ru", id, метка),
		fmt.Sprintf("Петров %d %s", id, метка))

	found, err := clients.Find(ctx, fmt.Sprint(id), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != id {
		t.Fatalf("по номеру %d нашлось %d записей, а номер у врача один", id, len(found))
	}
}

func TestPgПоискНеНашедшийНикогоОтдаётПустойСписок(t *testing.T) {
	// Ненайденный клиент — исправный случай. Отдай поиск пустое значение,
	// экран падал бы ровно на опечатке в почте, то есть чаще всего.
	clients := NewClients(testGate(t))
	found, err := clients.Find(context.Background(), "такой-почты-нет@example.ru", 0)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil {
		t.Fatal("поиск отдал пустое значение вместо пустого списка")
	}
	if len(found) != 0 {
		t.Errorf("по несуществующей почте нашлось %d записей", len(found))
	}
}

func TestPgКарточкаСчитаетУстройстваИЖивыеПрава(t *testing.T) {
	// Оператор решает по карточке, тот ли это человек, и по ней же видит,
	// за что уже заплачено. Отозванное право в этом числе показало бы
	// оплаченным то, чего у врача нет.
	gate := testGate(t)
	clients := NewClients(gate)
	ctx := context.Background()
	id := врач(t, gate)

	if _, err := gate.Exec(ctx,
		`INSERT INTO devices (account_id, token_hash) VALUES ($1, $2)`,
		id, fmt.Sprintf("отпечаток-%d-%d", time.Now().UnixNano(), rand.IntN(1000))); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx,
		`INSERT INTO entitlements (account_id, kind, origin) VALUES ($1, 'subscription', 'grant')`,
		id); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx,
		`INSERT INTO entitlements (account_id, kind, origin, revoked_at)
		 VALUES ($1, 'subscription', 'grant', NOW())`, id); err != nil {
		t.Fatal(err)
	}

	one, err := clients.One(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if one.Devices != 1 {
		t.Errorf("устройств насчитано %d, а заводили одно", one.Devices)
	}
	if one.Rights != 1 {
		t.Errorf("живых прав насчитано %d: отозванное попало в живые", one.Rights)
	}
}

func TestPgБлокировкаСтавитсяОтметкойИСнимается(t *testing.T) {
	// Отметка проверяется на входе устройства, а поставить её было нечем.
	// Удалять же учётную запись нельзя: на неё ссылаются платежи и права,
	// и удаление порвало бы разбирательство о деньгах.
	gate := testGate(t)
	clients := NewClients(gate)
	ctx := context.Background()
	id := врач(t, gate)

	if err := clients.SetBlocked(ctx, id, true, time.Now()); err != nil {
		t.Fatalf("вход не закрыт: %v", err)
	}
	one, err := clients.One(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !one.Blocked {
		t.Fatal("отметка не встала")
	}

	if err := clients.SetBlocked(ctx, id, false, time.Now()); err != nil {
		t.Fatalf("вход не открыт: %v", err)
	}
	one, err = clients.One(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if one.Blocked {
		t.Error("отметка не снялась")
	}
}

func TestPgБлокировкаЧужогоНомераОтказывает(t *testing.T) {
	// Молчаливый успех на несуществующем номере сказал бы оператору, что
	// вход закрыт, — а закрывать было нечего.
	clients := NewClients(testGate(t))
	err := clients.SetBlocked(context.Background(), 1<<40, true, time.Now())
	if err == nil {
		t.Fatal("блокировка несуществующей записи прошла молча")
	}
}

func TestPgДвеОплатыПодрядДаютДваМесяца(t *testing.T) {
	// Продление читает конец действующей подписки и вставляет новую
	// строку. Без замка на записи врача две оплаты, пришедшие разом,
	// видели одно и то же «до» и обе считали от него: врач платил за два
	// месяца и получал один. Воспроизводилось в тридцати девяти случаях
	// из сорока, и потому кругов здесь много — с одним парным заходом
	// проверка проходила бы и на сломанном коде.
	ctx := context.Background()
	gate := testGate(t)
	pay := NewPayments(gate)
	const кругов = 20

	for круг := range кругов {
		account := врач(t, gate)
		now := time.Now()

		// Общий старт: без него горутины расходятся во времени, и
		// соперничество, ради которого всё написано, просто не случается.
		старт := make(chan struct{})
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-старт
				_, err := pay.Accept(ctx, Income{
					AccountID: account, Purpose: "subscription:month",
					Kopecks: 199000, By: "проверка",
					IdemKey: ключ(),
				}, now)
				if err != nil {
					t.Errorf("платёж не принят: %v", err)
				}
			}()
		}
		close(старт)
		wg.Wait()

		var until time.Time
		if err := gate.QueryRow(ctx, `SELECT max(expires_at) FROM entitlements
		     WHERE account_id = $1 AND kind = 'subscription'`, account).Scan(&until); err != nil {
			t.Fatalf("срок подписки не прочитан: %v", err)
		}
		if суток := until.Sub(now).Hours() / 24; суток < 59 {
			t.Fatalf("круг %d: оплачено два месяца, выдано %.0f суток", круг, суток)
		}
	}
}

func TestPgКлючПовторностиСверяетсяСПриходом(t *testing.T) {
	// Ключ уникален сам по себе, а не в паре с врачом и назначением.
	// Прежде повтор отдавал ПРЕЖНИЙ платёж без сверки: второй врач платил,
	// получал чужой приход с пометкой «повтор» и оставался без прав, а
	// оператор читал уверенное «уже оформлен».
	ctx := context.Background()
	gate := testGate(t)
	pay := NewPayments(gate)
	первый, второй := врач(t, gate), врач(t, gate)
	key := ключ()
	now := time.Now()

	if _, err := pay.Accept(ctx, Income{AccountID: первый,
		Purpose: "subscription:year", Kopecks: 249000,
		IdemKey: key, By: "оператор"}, now); err != nil {
		t.Fatalf("первый приход не оформлен: %v", err)
	}

	_, err := pay.Accept(ctx, Income{AccountID: второй,
		Purpose: "subscription:month", Kopecks: 39900,
		IdemKey: key, By: "оператор"}, now)
	if err == nil {
		t.Fatal("чужой ключ принят молча: второй врач заплатил и остался без прав")
	}
	if !strings.Contains(err.Error(), "уже занят другим приходом") {
		t.Errorf("отказ не называет причину: %v", err)
	}

	var прав int
	if err := gate.QueryRow(ctx,
		`SELECT count(*) FROM entitlements WHERE account_id = $1`, второй).Scan(&прав); err != nil {
		t.Fatal(err)
	}
	if прав != 0 {
		t.Errorf("отказанный приход всё же выдал прав: %d", прав)
	}
}

func TestPgПовторТемЖеПриходомОтдаётПрежнийПлатёж(t *testing.T) {
	// Оборотная сторона той же сверки: оператор нажал дважды, а не принял
	// деньги дважды. Тот же врач, то же назначение, та же сумма — это
	// повтор, и он обязан отдать прежний платёж, а не отказ.
	ctx := context.Background()
	gate := testGate(t)
	pay := NewPayments(gate)
	account := врач(t, gate)
	key := ключ()
	now := time.Now()

	in := Income{AccountID: account, Purpose: "subscription:month",
		Kopecks: 39900, IdemKey: key, By: "оператор"}
	первый, err := pay.Accept(ctx, in, now)
	if err != nil {
		t.Fatalf("приход не оформлен: %v", err)
	}
	второй, err := pay.Accept(ctx, in, now)
	if err != nil {
		t.Fatalf("повтор того же прихода отказал: %v", err)
	}
	if !второй.Repeated {
		t.Error("повтор не помечен повтором")
	}
	if второй.ID != первый.ID {
		t.Errorf("повтор завёл второй платёж: %d и %d", первый.ID, второй.ID)
	}

	var платежей int
	if err := gate.QueryRow(ctx,
		`SELECT count(*) FROM payments WHERE account_id = $1`, account).Scan(&платежей); err != nil {
		t.Fatal(err)
	}
	if платежей != 1 {
		t.Errorf("платежей %d, а деньги приходили один раз", платежей)
	}
}

// Набор, ставший бесплатным, оставляет след в журнале.
//
// Выключение цены — предусмотренный способ сделать набор бесплатным, и
// беда была не в способе, а в том, что след оставался только в самой
// строке цены: без ответа на «кто» и «что было до». Вопрос этот задают
// один раз и ровно тогда, когда деньги уже не пришли.
func TestPgВыключеннаяЦенаПопадаетВЖурнал(t *testing.T) {
	gate := testGate(t)
	prices := NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)

	редакция, err := prices.Set(ctx, "составитель", "pack:"+slug, 39900, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prices.Set(ctx, "составитель", "pack:"+slug, 39900, false, редакция); err != nil {
		t.Fatal(err)
	}

	var by string
	var details map[string]any
	err = gate.QueryRow(ctx, `
		SELECT user_login, details FROM admin_journal
		 WHERE action = 'price:set' AND subject = $1
		 ORDER BY id DESC LIMIT 1`, "pack:"+slug).Scan(&by, &details)
	if err != nil {
		t.Fatalf("следа в журнале нет: %v", err)
	}
	if by != "составитель" {
		t.Errorf("журнал не помнит, кто выключил цену: %q", by)
	}
	if details["becameFree"] != true {
		t.Errorf("журнал не называет переход в бесплатное: %v", details)
	}
	if details["wasKopecks"] != float64(39900) {
		t.Errorf("журнал не помнит прежней цены: %v", details["wasKopecks"])
	}
}

// Заведение цены переходом в бесплатное не считается.
//
// Иначе журнал наполнился бы строками «стал бесплатным» о наборах,
// которые бесплатными были всегда, и настоящий переход потерялся бы среди
// них — то есть журнал был бы, а ответа в нём не было бы.
func TestPgЗаведениеЦеныНеСчитаетсяПереходомВБесплатное(t *testing.T) {
	gate := testGate(t)
	prices := NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)

	if _, err := prices.Set(ctx, "составитель", "pack:"+slug, 19900, true, 0); err != nil {
		t.Fatal(err)
	}
	var details map[string]any
	if err := gate.QueryRow(ctx, `
		SELECT details FROM admin_journal
		 WHERE action = 'price:set' AND subject = $1
		 ORDER BY id DESC LIMIT 1`, "pack:"+slug).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if details["becameFree"] != false {
		t.Errorf("первое заведение цены записано как переход в бесплатное: %v", details)
	}
}

// Негодная цена не оставляет ни цены, ни записи в журнале.
//
// Запись о том, чего не случилось, хуже её отсутствия: по журналу потом
// и восстанавливают, что было.
func TestPgОтвергнутаяЦенаНеПишетВЖурнал(t *testing.T) {
	gate := testGate(t)
	prices := NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)

	if _, err := prices.Set(ctx, "составитель", "pack:"+slug, 0, true, 0); err == nil {
		t.Fatal("цена в ноль принята")
	}
	var n int
	if err := gate.QueryRow(ctx,
		`SELECT count(*) FROM admin_journal WHERE subject = $1`, "pack:"+slug).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("отвергнутая цена оставила %d записей в журнале", n)
	}
}

// Приведение предела не съедает запрос «на одного больше».
//
// Ручка спрашивает на одного больше своего предела, чтобы отличить «их
// ровно столько» от «их больше». Приведи хранилище этот довод второй раз
// — и при пределе в самый потолок «больше» превратилось бы в пятьдесят:
// поиск молча сузился бы вчетверо ровно там, где оператор просил шире
// всего.
func TestПределКлиентовПриводитсяОдинРаз(t *testing.T) {
	if got := ClientsShown(0); got != 50 {
		t.Errorf("неназванный предел стал %d вместо пятидесяти", got)
	}
	if got := ClientsShown(10); got != 10 {
		t.Errorf("названный предел 10 стал %d", got)
	}
	if got := ClientsShown(200); got != 200 {
		t.Errorf("предел в потолок стал %d", got)
	}
	if got := ClientsShown(201); got != 50 {
		t.Errorf("предел выше потолка стал %d вместо пятидесяти", got)
	}
}

// Поиск клиентов говорит, что показал не всех.
//
// Оператор ищет врача по куску фамилии, видит полный список без
// пятьдесят первого и заводит вторую учётную запись тому, у кого она
// есть. «Показаны первые N» не было написано нигде.
func TestPgПоискКлиентовНазываетОбрезанное(t *testing.T) {
	gate := testGate(t)
	ctx := context.Background()
	clients := NewClients(gate)

	// Общий кусок в имени: по нему и ищем, чтобы в выборку попали ровно
	// свои, а не все, кого завели соседние проверки.
	метка := fmt.Sprintf("обрезка%d", time.Now().UnixNano())
	for i := 0; i < 4; i++ {
		врачСПочтой(t, gate, fmt.Sprintf("%s-%d@example.ru", метка, i), метка)
	}

	found, err := clients.Find(ctx, метка, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 {
		t.Fatalf("при пределе 3 найдено %d", len(found))
	}
	// Ручка спросила бы на одного больше и по нему поняла бы, что есть
	// ещё. Здесь проверяется то же самое напрямую.
	probe, err := clients.Find(ctx, метка, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe) != 4 {
		t.Errorf("запрос на одного больше нашёл %d: обрезанное не отличить от полного", len(probe))
	}
}

func TestPgЦенаНеЗатираетсяОпоздавшимОператором(t *testing.T) {
	// Двое открыли витрину. Первый ставит 399 рублей, второй, видевший
	// прежнюю цену, сохраняет свою следом — и прежде побеждала последняя
	// запись. Узнаётся это по непришедшим деньгам, и ровно тогда, когда
	// возвращать поздно.
	gate := testGate(t)
	prices := NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)

	редакция, err := prices.Set(ctx, "первый", "pack:"+slug, 39900, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prices.Set(ctx, "второй", "pack:"+slug, 19900, true, 0); !errors.Is(err, ErrStale) {
		t.Fatalf("опоздавшая цена принята: %v", err)
	}

	// Цена осталась первой, а не той, что пришла последней.
	live, err := prices.Live(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if live["pack:"+slug] != 39900 {
		t.Errorf("в витрине %d копеек вместо 39900: опоздавший всё же затёр",
			live["pack:"+slug])
	}
	// А от своей редакции тот же оператор правит свободно.
	if _, err := prices.Set(ctx, "первый", "pack:"+slug, 19900, true, редакция); err != nil {
		t.Errorf("правка от своей редакции отклонена: %v", err)
	}
}

func TestPgЗаведениеЦеныПоверхЗаведённойОтказывает(t *testing.T) {
	// Нулевая редакция означает «цены не было». Пришедший с ней к товару,
	// которому цену уже назначили, не правит её, а заводит заново — и
	// затирает чужое решение, ничего о нём не зная. Отказ поэтому говорит
	// не о числах, а о том, что случилось.
	gate := testGate(t)
	prices := NewPrices(gate)
	ctx := context.Background()
	slug := набор(t, gate)

	if _, err := prices.Set(ctx, "первый", "pack:"+slug, 39900, true, 0); err != nil {
		t.Fatal(err)
	}
	_, err := prices.Set(ctx, "второй", "pack:"+slug, 19900, true, 0)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("цена заведена поверх заведённой: %v", err)
	}
	if !strings.Contains(err.Error(), "назначили, пока вы открывали витрину") {
		t.Errorf("отказ не говорит, что цену назначили без него: %s", err)
	}
}
