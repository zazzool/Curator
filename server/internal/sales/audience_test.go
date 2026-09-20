package sales

import (
	"context"
	"testing"
	"time"

	"curator/server/internal/audience"
	"curator/server/internal/dbgate"
	"curator/server/internal/packs"
)

// Группы врачей глазами доступа.
//
// Это главная проверка того, ради чего группы заведены: витрина, корпус и
// выгрузка обязаны отвечать одинаково. Проверяются поэтому обе стороны
// сразу — Allowed (выгрузка одного набора) и OpenPackIDs (корпус), — и
// расхождение между ними ловится здесь, а не у врача.

// вГруппу заводит группу с правилом, привязывает к ней набор и отдаёт
// метку группы.
func вГруппу(t *testing.T, gate *dbgate.Gate, pack string, rule audience.Rule, mode string) string {
	t.Helper()
	store := audience.NewStore(gate)
	ctx := context.Background()
	slug := ключ2()
	if err := store.Create(ctx, slug, "Группа "+slug, "", rule); err != nil {
		t.Fatalf("группа не заведена: %v", err)
	}
	if err := store.SetPack(ctx, pack, []audience.Bound{{Slug: slug, Mode: mode}}); err != nil {
		t.Fatalf("набор не привязан к группе: %v", err)
	}
	return slug
}

// ключ2 — метка группы латиницей: метка её проверяется тем же образцом,
// что у набора, и кириллический ключ() сюда не годится.
func ключ2() string {
	var out []byte
	for _, r := range ключ() {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, byte(r))
		}
	}
	return "g" + string(out)
}

// открытые отвечает, попал ли набор в корпус этого врача.
func открытые(t *testing.T, access *Access, account, packID int64) bool {
	t.Helper()
	ids, _, err := access.OpenPackIDs(context.Background(), account, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, one := range ids {
		if one == packID {
			return true
		}
	}
	return false
}

// номер набора по метке.
func номер(t *testing.T, gate *dbgate.Gate, slug string) int64 {
	t.Helper()
	var id int64
	if err := gate.QueryRow(context.Background(),
		`SELECT id FROM packs WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("набор %s не найден: %v", slug, err)
	}
	return id
}

func TestPgГруппаОткрываетПлатныйНаборБезПокупки(t *testing.T) {
	// Кафедральный заказ: набор платный, но ординаторам кафедры открыт.
	// Прежде это потребовало бы строки права каждому поимённо, а их
	// сотни, и выдавал бы их человек руками.
	gate := testGate(t)
	access := NewAccess(gate)
	ctx := context.Background()
	slug := наборЛинейки(t, gate, packs.LinePaid)
	packID := номер(t, gate, slug)

	свой := врач(t, gate)
	чужой := врач(t, gate)
	привязатьПочту(t, gate, свой)
	привязатьПочту(t, gate, чужой)

	// Правило по признаку, который есть у одного и нет у другого: почта
	// у обоих, значит различает их поимённый список.
	группа := вГруппу(t, gate, slug, audience.Rule{}, audience.ModeOpen)
	if err := audience.NewStore(gate).AddMember(ctx, группа, свой, "operator"); err != nil {
		t.Fatal(err)
	}

	// Обе половины. «Открытое видно» зелено и тогда, когда видно всё
	// подряд, поэтому рядом стоит чужой, которому ничего не открывали.
	ok, err := access.Allowed(ctx, свой, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("набор, открытый группе, закрыт для её члена")
	}
	if ok, _ := access.Allowed(ctx, чужой, slug, time.Now()); ok {
		t.Error("набор, открытый группе, открыт постороннему")
	}

	// И корпус обязан ответить то же самое: разойдись они, витрина
	// показывала бы «открыто» там, где выгрузка отвечает отказом.
	if !открытые(t, access, свой, packID) {
		t.Error("корпус не знает про группу, а выгрузка знает")
	}
	if открытые(t, access, чужой, packID) {
		t.Error("корпус открыл набор постороннему, а выгрузка не открыла")
	}
}

func TestPgГруппаСкрываетНаборНоНеОтбираетКупленное(t *testing.T) {
	gate := testGate(t)
	access := NewAccess(gate)
	ctx := context.Background()
	slug := наборЛинейки(t, gate, packs.LineGuest)
	packID := номер(t, gate, slug)

	// Правило, под которое попадает всякий: у обоих врачей стаж не
	// меньше нуля суток. Различать их будет покупка, а не правило.
	вГруппу(t, gate, slug, audience.Rule{{Trait: audience.TraitAge, N: 0}}, audience.ModeHidden)

	скрытый := врач(t, gate)
	купивший := врач(t, gate)
	if _, err := gate.Exec(ctx, `
		INSERT INTO entitlements (account_id, kind, pack_id, origin)
		VALUES ($1, 'pack', $2, 'grant')`, купивший, packID); err != nil {
		t.Fatal(err)
	}

	if ok, _ := access.Allowed(ctx, скрытый, slug, time.Now()); ok {
		t.Error("скрытый от группы набор остался открыт")
	}
	if открытые(t, access, скрытый, packID) {
		t.Error("скрытый набор остался в корпусе")
	}

	// Купленное сильнее скрытия: отобрать оплаченное правкой правила
	// значило бы отобрать деньги молча, а правила правят на живых
	// группах, и задеть купившего проще всего.
	ok, err := access.Allowed(ctx, купивший, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("скрытие группой отобрало купленный набор")
	}
	if !открытые(t, access, купивший, packID) {
		t.Error("купленный набор пропал из корпуса при скрытии группой")
	}
}

func TestPgСкрытыйНаборНеЗакрытАСкрыт(t *testing.T) {
	gate := testGate(t)
	access := NewAccess(gate)
	ctx := context.Background()
	slug := наборЛинейки(t, gate, packs.LinePaid)
	packID := номер(t, gate, slug)

	вГруппу(t, gate, slug, audience.Rule{{Trait: audience.TraitAge, N: 0}}, audience.ModeHidden)
	скрытый := врач(t, gate)
	купивший := врач(t, gate)
	if _, err := gate.Exec(ctx, `
		INSERT INTO entitlements (account_id, kind, pack_id, origin)
		VALUES ($1, 'pack', $2, 'grant')`, купивший, packID); err != nil {
		t.Fatal(err)
	}

	each, err := access.StateEach(ctx, скрытый, []string{slug}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !each[slug].Hidden {
		t.Error("скрытый набор не назван скрытым: витрина оставит его на прилавке с ценником")
	}
	if each[slug].Open {
		t.Error("скрытый набор назван открытым")
	}

	// Купивший видит его по-прежнему: право сильнее скрытия, и пропасть
	// у него из витрины набор не должен. Скрытый и открытый разом — не
	// противоречие, а именно этот случай.
	его, err := access.StateEach(ctx, купивший, []string{slug}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !его[slug].Open {
		t.Error("скрытие отобрало купленное")
	}
	if !его[slug].Hidden {
		t.Error("купивший перестал считаться попавшим под скрывающую группу")
	}
}

func TestPgБезГруппКорпусТакойЖеКакБылДоНих(t *testing.T) {
	// Самая скучная и самая нужная проверка наряда: группы не должны
	// менять ничего у того, кто ни в одну не попал. Линейка решает как
	// прежде, и доводов у неё столько же.
	gate := testGate(t)
	access := NewAccess(gate)
	ctx := context.Background()

	гостевой := наборЛинейки(t, gate, packs.LineGuest)
	базовый := наборЛинейки(t, gate, packs.LineBasic)
	платный := наборЛинейки(t, gate, packs.LinePaid)

	// Группа заведена и привязана к ЧУЖОМУ набору: она есть, портрет
	// считается, и всё равно ничего не меняет.
	чужой := наборЛинейки(t, gate, packs.LinePaid)
	вГруппу(t, gate, чужой, audience.Rule{{Trait: audience.TraitSubscribed}}, audience.ModeOpen)

	id := врач(t, gate)
	привязатьПочту(t, gate, id)
	for _, случай := range []struct {
		slug string
		ждём bool
	}{
		{гостевой, true},
		{базовый, true},
		{платный, false},
		{чужой, false},
	} {
		ok, err := access.Allowed(ctx, id, случай.slug, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if ok != случай.ждём {
			t.Errorf("набор %s: открыт = %v, ждали %v", случай.slug, ok, случай.ждём)
		}
	}
}

func TestPgВитринаГоворитЧемНаборОткрытАНеТолькоЧтоОткрыт(t *testing.T) {
	// Одного «открыт» витрине мало. Купленное не отбирают никогда, а
	// открытое группой держится на правиле, и правило смотрит на живого
	// врача: попавший в группу «не заходил месяц» выйдет из неё, едва
	// зайдя, и набор пропадёт у него сам. Промолчи витрина о доводе —
	// врач прочёл бы пропажу как поломку приложения.
	//
	// Проверяется здесь именно связка доводов на живой базе, а не
	// перебор: перебор проверен таблицей в packs. Здесь важно, что довод
	// доезжает из базы до ответа не подменившись — что купившего не
	// объявили членом группы и наоборот.
	gate := testGate(t)
	access := NewAccess(gate)
	ctx := context.Background()
	slug := наборЛинейки(t, gate, packs.LinePaid)
	packID := номер(t, gate, slug)

	группой := врач(t, gate)
	покупкой := врач(t, gate)
	линейкой := врач(t, gate)
	никак := врач(t, gate)

	группа := вГруппу(t, gate, slug, audience.Rule{}, audience.ModeOpen)
	if err := audience.NewStore(gate).AddMember(ctx, группа, группой, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx, `
		INSERT INTO entitlements (account_id, kind, pack_id, origin)
		VALUES ($1, 'pack', $2, 'grant')`, покупкой, packID); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx, `
		INSERT INTO entitlements (account_id, kind, origin)
		VALUES ($1, 'subscription', 'grant')`, линейкой); err != nil {
		t.Fatal(err)
	}

	for _, случай := range []struct {
		имя     string
		account int64
		open    bool
		by      string
	}{
		{"член группы", группой, true, packs.ByGroup},
		{"купивший", покупкой, true, packs.ByPurchase},
		{"подписчик", линейкой, true, packs.ByLine},
		{"посторонний", никак, false, ""},
	} {
		state, err := access.StateEach(ctx, случай.account, []string{slug}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		got := state[slug]
		if got.Open != случай.open || got.By != случай.by {
			t.Errorf("%s: витрина отвечает open=%v by=%q, ждали open=%v by=%q",
				случай.имя, got.Open, got.By, случай.open, случай.by)
		}
	}
}
