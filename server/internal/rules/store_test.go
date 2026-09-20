package rules

import (
	"context"
	"fmt"
	"math/rand"
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
	gate, err := dbgate.Open(context.Background(), dsn, dbgate.Options{})
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

// свойID — опознаватель, не совпадающий с чужим: база одна на весь
// прогон, и две проверки, взявшие одно имя, ловили бы друг друга за руку
// через раз.
func свойID(prefix string) string {
	return fmt.Sprintf("%s:%d-%d", prefix, time.Now().UnixNano(), rand.Intn(1000))
}

func TestPgПравилоЗаписываетсяИЧитаетсяЦеликом(t *testing.T) {
	ctx := context.Background()
	store := NewStore(testGate(t))

	id := свойID("проба")
	saved, err := store.Save(ctx, Rule{
		ID:            id,
		Title:         "Сроки этого приказа считаются рабочими днями",
		Text:          "В задачах по третьему разделу считай сроки рабочими днями.",
		Why:           "Дважды приходилось править календарные дни на рабочие.",
		Kind:          KindSubstance,
		Source:        FromLint,
		Scope:         Scope{Sources: []int64{4242}, Units: []string{"3"}, Nodes: []string{"compose"}},
		Confirmations: Quorum,
		SeenJobs:      []int64{11, 22, 33},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != Active {
		t.Fatalf("правило с кворумом записано как «%s»", saved.Status)
	}

	list, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got *Rule
	for i := range list {
		if list[i].ID == id {
			got = &list[i]
		}
	}
	if got == nil {
		t.Fatal("записанное правило не прочиталось")
	}
	// Область — единственное поле, уезжающее в базу как JSON: разойдись
	// запись с чтением, правило молча стало бы действовать всегда.
	if len(got.Scope.Sources) != 1 || got.Scope.Sources[0] != 4242 {
		t.Fatalf("область источников не доехала: %+v", got.Scope)
	}
	if len(got.Scope.Units) != 1 || got.Scope.Units[0] != "3" {
		t.Fatalf("область единиц не доехала: %+v", got.Scope)
	}
	if len(got.SeenJobs) != 3 {
		t.Fatalf("подтвердившие задания не доехали: %+v", got.SeenJobs)
	}
}

func TestPgПустаяОбластьНеСтановитсяNULL(t *testing.T) {
	// Пустой срез Go уезжает в JSON как null, а в колонку с NOT NULL —
	// как NULL, и вставка падает. Ловушка одна, мест два, и второе —
	// подтвердившие задания.
	ctx := context.Background()
	store := NewStore(testGate(t))

	id := свойID("пустое")
	if _, err := store.Save(ctx, Rule{
		ID:     id,
		Title:  "Правило без области и без подтверждений",
		Text:   "Не пиши в условии метку единицы.",
		Kind:   KindStructure,
		Source: Builtin,
	}); err != nil {
		t.Fatalf("правило с пустой областью не записалось: %v", err)
	}

	list, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.ID != id {
			continue
		}
		if !r.Scope.always() {
			t.Fatalf("пустая область прочиталась как %+v", r.Scope)
		}
		if r.SeenJobs == nil {
			return // пустой массив базы читается как пустой срез — это и нужно
		}
		if len(r.SeenJobs) != 0 {
			t.Fatalf("у нового правила уже есть подтвердившие задания: %+v", r.SeenJobs)
		}
		return
	}
	t.Fatal("записанное правило не прочиталось")
}

func TestPgЗатравкаНеЗатираетПравленоеСоставителем(t *testing.T) {
	// Правка свода — работа составителя, и потерять её значит потерять
	// неделю настройки. Поэтому встроенное кладётся только недостающее.
	ctx := context.Background()
	store := NewStore(testGate(t))

	if err := store.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var первое *Rule
	for i := range list {
		if list[i].Source == Builtin {
			первое = &list[i]
			break
		}
	}
	if первое == nil {
		t.Fatal("затравка не положила ни одного встроенного правила")
	}

	правленое := *первое
	правленое.Text = "Правленный составителем текст этого правила."
	правленое.Pinned = true
	if _, err := store.Save(ctx, правленое); err != nil {
		t.Fatal(err)
	}

	// Второй накат: правленое обязано остаться правленым.
	if err := store.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range after {
		if r.ID != правленое.ID {
			continue
		}
		if r.Text != правленое.Text {
			t.Fatalf("затравка затёрла правку составителя: %q", r.Text)
		}
		return
	}
	t.Fatal("правленое правило исчезло после повторной затравки")
}

func TestPgСводЧитаетсяСнимкомИОтдаётБлок(t *testing.T) {
	// Снимок, а не живое чтение: написание задачи идёт минутами, за
	// которые правило может дозреть до кворума на соседнем задании, и
	// задача получила бы замечание о правиле, которого в её задании не
	// было.
	ctx := context.Background()
	store := NewStore(testGate(t))
	if err := store.Seed(ctx); err != nil {
		t.Fatal(err)
	}

	book, err := store.Book(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if book.Len() == 0 {
		t.Fatal("снимок свода пуст после затравки")
	}
	block := book.Block(Context{SourceID: 1, Node: "compose"})
	if block == "" {
		t.Fatal("свод со встроенными правилами дал пустой блок написанию")
	}
}

func TestPgОдноЗаданиеПодтверждаетПравилоОдинРаз(t *testing.T) {
	// Задача, где признак назван трижды, — всё равно одна задача.
	// Считай её за три, и кворум выдавался бы в одиночку: правило
	// уходило бы в задание с ПЕРВОГО замечания, то есть кворума не
	// существовало бы вовсе, а выглядело бы это исправной работой.
	ctx := context.Background()
	store := NewStore(testGate(t))

	id := свойID("кворум")
	if _, _, err := store.Propose(ctx, Rule{
		ID:     id,
		Title:  "Слово «явка» в условии",
		Text:   "Не пиши в условии слово «явка» и однокоренные.",
		Kind:   KindSubstance,
		Source: FromLint,
		Scope:  Scope{Sources: []int64{4242}},
	}, 100); err != nil {
		t.Fatal(err)
	}

	// То же задание ещё дважды: счётчик не должен тронуться.
	for i := 0; i < 2; i++ {
		r, counted, err := store.Confirm(ctx, id, 100)
		if err != nil {
			t.Fatal(err)
		}
		if counted {
			t.Fatal("то же задание подтвердило правило второй раз")
		}
		if r.Confirmations != 1 {
			t.Fatalf("подтверждений %d после повтора одного задания", r.Confirmations)
		}
		if r.Status == Active {
			t.Fatal("правило дозрело на одном задании")
		}
	}

	// Разные задания: на третьем правило обязано дозреть.
	for _, job := range []int64{101, 102} {
		if _, counted, err := store.Confirm(ctx, id, job); err != nil {
			t.Fatal(err)
		} else if !counted {
			t.Fatalf("задание %d не зачлось", job)
		}
	}
	r, _, err := store.Confirm(ctx, id, 103)
	if err != nil {
		t.Fatal(err)
	}
	if r.Confirmations < Quorum || r.Status != Active {
		t.Fatalf("правило с %d подтверждениями осталось «%s»", r.Confirmations, r.Status)
	}
}

func TestPgЗаведениеНеПерезаписываетПравленое(t *testing.T) {
	// Составитель мог поправить текст правила или погасить его. Накат
	// обучения, переписывающий существующее, затёр бы его работу — и
	// погашенное правило вернулось бы в задание само собой.
	ctx := context.Background()
	store := NewStore(testGate(t))

	id := свойID("правленое")
	proposal := Rule{
		ID:     id,
		Title:  "Слово «явка» в условии",
		Text:   "Первоначальный текст правила.",
		Kind:   KindSubstance,
		Source: FromLint,
		Scope:  Scope{Sources: []int64{4242}},
	}
	if _, _, err := store.Propose(ctx, proposal, 200); err != nil {
		t.Fatal(err)
	}

	погашено := proposal
	погашено.Status = Muted
	погашено.Pinned = true
	погашено.Text = "Правленный составителем текст."
	if _, err := store.Save(ctx, погашено); err != nil {
		t.Fatal(err)
	}

	// Обучение приносит то же правило снова — и ещё дважды, до кворума.
	for _, job := range []int64{201, 202, 203} {
		if _, _, err := store.Propose(ctx, proposal, job); err != nil {
			t.Fatal(err)
		}
	}

	list, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.ID != id {
			continue
		}
		if r.Text != погашено.Text {
			t.Fatalf("обучение затёрло текст составителя: %q", r.Text)
		}
		if r.Status != Muted {
			t.Fatalf("погашенное правило воскресло как «%s» на %d подтверждениях",
				r.Status, r.Confirmations)
		}
		return
	}
	t.Fatal("правило исчезло")
}
