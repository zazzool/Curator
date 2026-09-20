package packs

import (
	"context"
	"crypto/ed25519"
	crypto_rand "crypto/rand"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
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

func лавка(t *testing.T) (*Store, *dbgate.Gate, ed25519.PublicKey) {
	t.Helper()
	gate := testGate(t)
	pub, priv, err := ed25519.GenerateKey(crypto_rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(gate, priv, "проверка"), gate, pub
}

// задача кладёт задачу и отдаёт её номер.
func задача(t *testing.T, gate *dbgate.Gate, status string) string {
	t.Helper()
	ctx := context.Background()
	slug := fmt.Sprintf("ист-%d-%d", time.Now().UnixNano(), rand.IntN(100000))

	var sourceID int64
	if err := gate.QueryRow(ctx,
		`INSERT INTO sources (slug, kind, title, unit_word, statement_word,
		                      purpose, hierarchy, completeness)
		 VALUES ($1, 'decree', 'Приказ', 'пункт', 'указание', 'legal', 'part-of', 'fragment')
		 RETURNING id`, slug).Scan(&sourceID); err != nil {
		t.Fatalf("источник не заведён: %v", err)
	}

	id := fmt.Sprintf("c-%d%d", time.Now().UnixNano(), rand.IntN(1000))
	body := `{"title":"Срок","kind":"recognition","segments":[],"options":[],` +
		`"answer":"","explanationMd":"","difficulty":2}`
	if _, err := gate.Exec(ctx,
		`INSERT INTO cases (id, source_id, unit_label, unit_path, status, body, published_at)
		 VALUES ($1, $2, 'п.1', 'п/п.1', $3, $4::jsonb, NOW())`,
		id, sourceID, status, body); err != nil {
		t.Fatalf("задача не заведена: %v", err)
	}
	return id
}

func набор(t *testing.T, store *Store) string {
	t.Helper()
	slug := fmt.Sprintf("nabor-%d-%d", time.Now().UnixNano(), rand.IntN(1000))
	if err := store.Create(context.Background(), slug, "Набор «"+slug+"»", "Описание"); err != nil {
		t.Fatalf("набор не заведён: %v", err)
	}
	return slug
}

func TestPgВыпускПодписанИПодписьСходится(t *testing.T) {
	store, gate, pub := лавка(t)
	ctx := context.Background()
	slug := набор(t, store)
	первая := задача(t, gate, "published")
	вторая := задача(t, gate, "published")

	if _, err := store.SetItems(ctx, slug, []string{первая, вторая}); err != nil {
		t.Fatal(err)
	}
	out, err := store.Publish(ctx, slug, time.Now())
	if err != nil {
		t.Fatalf("выпуск не собран: %v", err)
	}
	if out.Version != 1 {
		t.Errorf("первый выпуск получил версию %d", out.Version)
	}
	if err := Verify(out.Manifest, out.Signature, pub); err != nil {
		t.Errorf("своя же подпись не сошлась: %v", err)
	}
}

func TestPgПодписьСходитсяПослеЧтенияИзБазы(t *testing.T) {
	// Главная проверка этого куска. JSONB не хранит ни порядок ключей, ни
	// пробелы: postgres пересобирает объект по-своему. Подпиши мы то, что
	// достали из базы, — подпись не сошлась бы ни на одном устройстве, а
	// глазами оба текста одинаковы, и причину искали бы неделю.
	store, gate, pub := лавка(t)
	ctx := context.Background()
	slug := набор(t, store)
	if _, err := store.SetItems(ctx, slug, []string{задача(t, gate, "published")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, slug, time.Now()); err != nil {
		t.Fatal(err)
	}

	прочитан, err := store.Latest(ctx, slug)
	if err != nil {
		t.Fatalf("выпуск не прочитан: %v", err)
	}
	if err := Verify(прочитан.Manifest, прочитан.Signature, pub); err != nil {
		t.Errorf("подпись не сошлась после круга через базу: %v", err)
	}
}

func TestPgПравкаСоставаНеМеняетУжеВыпущенное(t *testing.T) {
	// Выпуск неизменяем: на устройствах стоит именно тот состав, который
	// подписан, и подменить его задним числом значит сделать подпись
	// бессмысленной.
	store, gate, pub := лавка(t)
	ctx := context.Background()
	slug := набор(t, store)
	первая := задача(t, gate, "published")
	if _, err := store.SetItems(ctx, slug, []string{первая}); err != nil {
		t.Fatal(err)
	}
	первый, err := store.Publish(ctx, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// Состав переписан, но выпуска не делали.
	if _, err := store.SetItems(ctx, slug, []string{первая, задача(t, gate, "published")}); err != nil {
		t.Fatal(err)
	}
	прежний, err := store.Latest(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(прежний.Manifest.Cases) != 1 {
		t.Errorf("в выпущенном составе стало %d задач", len(прежний.Manifest.Cases))
	}
	if err := Verify(прежний.Manifest, прежний.Signature, pub); err != nil {
		t.Errorf("подпись прежнего выпуска перестала сходиться: %v", err)
	}

	второй, err := store.Publish(ctx, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if второй.Version != первый.Version+1 {
		t.Errorf("версия второго выпуска %d, ожидалась %d", второй.Version, первый.Version+1)
	}
	if len(второй.Manifest.Cases) != 2 {
		t.Errorf("во втором выпуске %d задач, ожидалось 2", len(второй.Manifest.Cases))
	}
}

func TestPgЧерновикВВыпускНеПопадает(t *testing.T) {
	// Черновик, уехавший в пакет, — это задача, которую никто не
	// принимал, показанная врачу с нашей подписью.
	store, gate, _ := лавка(t)
	ctx := context.Background()
	slug := набор(t, store)
	готовая := задача(t, gate, "published")
	черновик := задача(t, gate, "draft")

	if _, err := store.SetItems(ctx, slug, []string{готовая, черновик}); err != nil {
		t.Fatal(err)
	}
	out, err := store.Publish(ctx, slug, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Manifest.Cases) != 1 {
		t.Fatalf("в выпуске %d задач, а опубликована одна", len(out.Manifest.Cases))
	}
	if out.Manifest.Cases[0].ID != готовая {
		t.Errorf("в выпуск попала не та задача: %s", out.Manifest.Cases[0].ID)
	}
}

func TestPgПустойНаборНеВыпускается(t *testing.T) {
	// Подписанный пустой пакет установится на устройство и покажет врачу
	// пустой экран, причём с нашей подписью.
	store, _, _ := лавка(t)
	slug := набор(t, store)
	if _, err := store.Publish(context.Background(), slug, time.Now()); err == nil {
		t.Error("пустой набор выпустился")
	}
}

func TestPgБезКлючаВыпускОтказываетСловами(t *testing.T) {
	gate := testGate(t)
	store := NewStore(gate, nil, "")
	if store.CanSign() {
		t.Fatal("склад без ключа считает, что может подписывать")
	}
	slug := набор(t, store)
	if _, err := store.Publish(context.Background(), slug, time.Now()); err == nil {
		t.Error("выпуск прошёл без ключа подписи")
	}
}

func TestPgНесуществующаяЗадачаВСоставеОтказывает(t *testing.T) {
	// Состав, принятый молча и без неё, выглядит собранным и таковым не
	// является.
	store, gate, _ := лавка(t)
	slug := набор(t, store)
	_, err := store.SetItems(context.Background(), slug,
		[]string{задача(t, gate, "published"), "c-такой-нет"})
	if err == nil {
		t.Error("состав с несуществующей задачей принят молча")
	}
}

func TestPgСтраницыЗадачНаборуПродолжаютсяСКурсора(t *testing.T) {
	// Набор на тысячу задач весит мегабайты, и телефон в метро не дотянет
	// одну большую закачку.
	store, gate, _ := лавка(t)
	ctx := context.Background()
	slug := набор(t, store)
	ids := []string{задача(t, gate, "published"), задача(t, gate, "published"),
		задача(t, gate, "published")}
	if _, err := store.SetItems(ctx, slug, ids); err != nil {
		t.Fatal(err)
	}

	first, next, err := store.Bodies(ctx, slug, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || next == "" {
		t.Fatalf("первая страница %d задач, курсор %q", len(first), next)
	}
	second, next, err := store.Bodies(ctx, slug, next, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 {
		t.Errorf("вторая страница %d задач, ожидалась одна", len(second))
	}
	if next != "" {
		t.Errorf("после последней страницы курсор не пуст: %q", next)
	}
}

func TestPgВитринаПоказываетТолькоВыпущенное(t *testing.T) {
	// Пакет, который нельзя скачать, в витрине это обещание, а не товар.
	store, gate, _ := лавка(t)
	ctx := context.Background()
	без := набор(t, store)
	с := набор(t, store)
	if _, err := store.SetItems(ctx, с, []string{задача(t, gate, "published")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, с, time.Now()); err != nil {
		t.Fatal(err)
	}

	shelf, err := store.Shelf(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, one := range shelf {
		seen[one.Slug] = true
	}
	if !seen[с] {
		t.Error("выпущенный набор не показан в витрине")
	}
	if seen[без] {
		t.Error("набор без выпуска показан в витрине")
	}
}

func TestPgМеткаНабораОтказываетВнятно(t *testing.T) {
	// Указатель базы отвечает на всё одинаково — «нарушено ограничение», —
	// и отказ «занята либо негодна» отправляет составителя гадать, что из
	// двух.
	store, _, _ := лавка(t)
	ctx := context.Background()

	err := store.Create(ctx, "Кириллица", "Название", "")
	if err == nil {
		t.Fatal("метка кириллицей принята")
	}
	if !contains(err.Error(), "латиницей") {
		t.Errorf("отказ не говорит, что не так с меткой: %q", err)
	}

	slug := набор(t, store)
	err = store.Create(ctx, slug, "Другое название", "")
	if err == nil {
		t.Fatal("метка заведена дважды")
	}
	if !contains(err.Error(), "уже заведён") {
		t.Errorf("отказ не говорит, что метка занята: %q", err)
	}
}

func TestPgСоставЧитаетсяНазваниямиЗадач(t *testing.T) {
	// Состав можно было только переписать целиком, но не прочесть: список
	// наборов знает лишь их число. Составитель, добавляющий одну задачу,
	// затирал бы этим всё, что собрали до него.
	store, gate, _ := лавка(t)
	ctx := context.Background()
	slug := набор(t, store)
	первая := задача(t, gate, "published")
	вторая := задача(t, gate, "published")
	if _, err := store.SetItems(ctx, slug, []string{вторая, первая}); err != nil {
		t.Fatal(err)
	}

	out, err := store.One(ctx, slug)
	if err != nil {
		t.Fatalf("состав не прочитан: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("в составе %d задач, а клали две", len(out.Items))
	}
	// Порядок — тот, который задал составитель, а не порядок номеров.
	if out.Items[0].ID != вторая || out.Items[1].ID != первая {
		t.Errorf("порядок состава не тот, что задали: %s, %s",
			out.Items[0].ID, out.Items[1].ID)
	}
	if out.Items[0].Title != "Срок" {
		t.Errorf("задача названа %q, а по одному номеру её не узнать", out.Items[0].Title)
	}
	if out.Version != 0 {
		t.Errorf("невыпущенный набор назвал выпуск номером %d", out.Version)
	}
}

func TestPgПустойСоставЭтоПустойСписок(t *testing.T) {
	// Только что заведённый набор пуст, и это исправный случай. Отдай он
	// null — экран состава упал бы именно на новом наборе, то есть у
	// всякого, кто заводит первый.
	store, _, _ := лавка(t)
	out, err := store.One(context.Background(), набор(t, store))
	if err != nil {
		t.Fatal(err)
	}
	if out.Items == nil {
		t.Fatal("пустой состав отдан пустым значением, а не пустым списком")
	}
	if len(out.Items) != 0 {
		t.Errorf("в новом наборе оказалось %d задач", len(out.Items))
	}
}

func TestPgСнятыйСВитриныНаборНеПоказывается(t *testing.T) {
	// Состояние «снят» стояло в схеме, а поставить его было нечем: набор,
	// который больше не продают, оставался на витрине навсегда.
	store, gate, _ := лавка(t)
	ctx := context.Background()
	slug := набор(t, store)
	if _, err := store.SetItems(ctx, slug, []string{задача(t, gate, "published")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, slug, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !наВитрине(t, store, slug) {
		t.Fatal("выпущенный набор не попал на витрину")
	}

	if err := store.Update(ctx, slug, "Набор", "", "retired"); err != nil {
		t.Fatalf("набор не снят: %v", err)
	}
	if наВитрине(t, store, slug) {
		t.Error("снятый набор остался на витрине")
	}

	// Выпуск при этом никуда не делся: подписанное остаётся подписанным.
	out, err := store.One(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 1 {
		t.Errorf("у снятого набора потерялся выпуск: версия %d", out.Version)
	}
}

func TestPgСостоянияКоторогоНетНаборНеПринимает(t *testing.T) {
	// Словарь состояний закрыт, и отказ называет само состояние: молча
	// принятое «удалён» не сняло бы набор ни с витрины, ни откуда-либо.
	store, _, _ := лавка(t)
	err := store.Update(context.Background(), набор(t, store), "Набор", "", "удалён")
	if err == nil {
		t.Fatal("состояние не из словаря принято")
	}
	if !strings.Contains(err.Error(), "удалён") {
		t.Errorf("отказ не назвал негодное состояние: %v", err)
	}
}

func наВитрине(t *testing.T, store *Store, slug string) bool {
	t.Helper()
	shelf, err := store.Shelf(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, one := range shelf {
		if one.Slug == slug {
			return true
		}
	}
	return false
}
