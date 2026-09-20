package rules

import (
	"context"
	"testing"
	"time"

	"curator/server/internal/llm"
)

// положить — действующее правило прямо в базе.
func положить(t *testing.T, store *Store, id string, src Source, text string, over func(*Rule)) Rule {
	t.Helper()
	r := Rule{
		ID: id, Title: "Правило " + id, Text: text,
		Kind: KindLanguage, Source: src, Status: Active, Pinned: true,
	}
	if over != nil {
		over(&r)
	}
	saved, err := store.Save(context.Background(), r)
	if err != nil {
		t.Fatalf("правило %s не записано: %v", id, err)
	}
	return saved
}

func взять(t *testing.T, store *Store, id string) Rule {
	t.Helper()
	list, err := store.All(context.Background())
	if err != nil {
		t.Fatalf("свод не прочитан: %v", err)
	}
	for _, r := range list {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("правила %s нет в своде", id)
	return Rule{}
}

func TestPgСлитоеЗакрываетсяДатойСоСсылкой(t *testing.T) {
	// Не удаляется: вопрос «чего мы требовали на прошлой неделе» обязан
	// иметь ответ, а слияние — как раз то действие, после которого его и
	// задают. И закрытое без ссылки неотличимо от пропавшего.
	ctx := context.Background()
	store := NewStore(testGate(t))

	keepID, victimID := свойID("уплот-жив"), свойID("уплот-слит")
	положить(t, store, keepID, FromLint, "Не пиши канцелярских оборотов в условии задачи.", nil)
	положить(t, store, victimID, FromLint, "Пиши условие простыми предложениями без оборотов.", nil)

	plan := CompactionPlan{Groups: []CompactionGroup{{
		KeepID: keepID, MergeIDs: []string{victimID},
		Text: "Пиши просто, без канцелярита.", Why: "оба про слог",
	}}}
	done, failed, err := store.ApplyCompaction(ctx, plan, time.Now().UTC())
	if err != nil || done != 1 || len(failed) != 0 {
		t.Fatalf("слияние не прошло: применено %d, отказы %v, ошибка %v", done, failed, err)
	}

	victim := взять(t, store, victimID)
	if victim.Status != Merged {
		t.Fatalf("слитое правило в состоянии %q, а не «слито»", victim.Status)
	}
	if victim.MergedInto != keepID {
		t.Fatalf("у слитого нет ссылки на выжившее: %q", victim.MergedInto)
	}
	if victim.ValidTo == nil {
		t.Fatal("слитое правило не закрыто датой")
	}
	if victim.InForce() {
		t.Fatal("слитое правило продолжает действовать")
	}
	if victim.Text == "" {
		t.Fatal("текст слитого правила стёрт: читать «чего требовали» станет нечего")
	}

	keep := взять(t, store, keepID)
	if keep.Text != "Пиши просто, без канцелярита." {
		t.Fatalf("выживший не принял сводную формулировку: %q", keep.Text)
	}
}

func TestPgПодтверждениеДоезжаетДоВыжившего(t *testing.T) {
	// Уплотнение не должно рвать петлю самообучения. Засчитай мы
	// подтверждение закрытому правилу — детектор продолжал бы находить
	// своё, счётчик рос бы у мёртвого, а живое выглядело бы никем не
	// подтверждённым. Ссылка слияния для того и заведена.
	ctx := context.Background()
	store := NewStore(testGate(t))

	keepID, victimID := свойID("петля-жив"), свойID("петля-слит")
	положить(t, store, keepID, FromLint, "Не пиши канцелярских оборотов в условии задачи.", nil)
	положить(t, store, victimID, FromLint, "Пиши условие простыми предложениями без оборотов.", nil)

	plan := CompactionPlan{Groups: []CompactionGroup{{
		KeepID: keepID, MergeIDs: []string{victimID},
		Text: "Пиши просто, без канцелярита.", Why: "оба про слог",
	}}}
	if done, _, err := store.ApplyCompaction(ctx, plan, time.Now().UTC()); err != nil || done != 1 {
		t.Fatalf("слияние не прошло: %d, %v", done, err)
	}

	before := взять(t, store, keepID).Confirmations
	if _, counted, err := store.Confirm(ctx, victimID, 424242); err != nil || !counted {
		t.Fatalf("подтверждение слитого не зачлось: %v, %v", counted, err)
	}

	keep := взять(t, store, keepID)
	if keep.Confirmations != before+1 {
		t.Fatalf("подтверждение не доехало до выжившего: было %d, стало %d", before, keep.Confirmations)
	}
	if !hasInt(keep.SeenJobs, 424242) {
		t.Fatalf("задание не записано выжившему: %v", keep.SeenJobs)
	}
	victim := взять(t, store, victimID)
	if hasInt(victim.SeenJobs, 424242) {
		t.Fatalf("задание записано закрытому правилу: %v", victim.SeenJobs)
	}
}

func TestPgПодтвержденияСлитыхБерутсяПоМаксимумуАНеСуммой(t *testing.T) {
	// Подтверждение считает РАЗНЫЕ задания, а множества заданий у слитых
	// правил пересекаются неизвестно как. Сумма объявила бы кворум там,
	// где его нет: два правила по два подтверждения одними и теми же
	// заданиями — это два разных задания, а не четыре.
	ctx := context.Background()
	store := NewStore(testGate(t))

	keepID, victimID := свойID("кворум-жив"), свойID("кворум-слит")
	общие := []int64{9001, 9002}
	положить(t, store, keepID, FromLint, "Не пиши канцелярских оборотов в условии задачи.", func(r *Rule) {
		r.Confirmations, r.SeenJobs = 2, append([]int64{}, общие...)
	})
	положить(t, store, victimID, FromLint, "Пиши условие простыми предложениями без оборотов.", func(r *Rule) {
		r.Confirmations, r.SeenJobs = 2, append([]int64{}, общие...)
	})

	plan := CompactionPlan{Groups: []CompactionGroup{{
		KeepID: keepID, MergeIDs: []string{victimID},
		Text: "Пиши просто, без канцелярита.", Why: "оба про слог",
	}}}
	if done, _, err := store.ApplyCompaction(ctx, plan, time.Now().UTC()); err != nil || done != 1 {
		t.Fatalf("слияние не прошло: %d, %v", done, err)
	}

	keep := взять(t, store, keepID)
	if keep.Confirmations != 2 {
		t.Fatalf("подтверждения сложены, а не взяты по максимуму: %d вместо 2", keep.Confirmations)
	}
	if len(keep.SeenJobs) != 2 {
		t.Fatalf("задания сложены, а не объединены: %v", keep.SeenJobs)
	}
}

func TestPgМножестваЗаданийОбъединяютсяПриСлиянии(t *testing.T) {
	// Обратная сторона того же: у слитых правил разные задания, и
	// объединение их должно дотянуть счётчик. Возьми мы просто максимум —
	// правило, дозревшее СОВМЕСТНО, осталось бы кандидатом.
	ctx := context.Background()
	store := NewStore(testGate(t))

	keepID, victimID := свойID("союз-жив"), свойID("союз-слит")
	положить(t, store, keepID, FromLint, "Не пиши канцелярских оборотов в условии задачи.", func(r *Rule) {
		r.Confirmations, r.SeenJobs = 2, []int64{8001, 8002}
	})
	положить(t, store, victimID, FromLint, "Пиши условие простыми предложениями без оборотов.", func(r *Rule) {
		r.Confirmations, r.SeenJobs = 2, []int64{8003, 8004}
	})

	plan := CompactionPlan{Groups: []CompactionGroup{{
		KeepID: keepID, MergeIDs: []string{victimID},
		Text: "Пиши просто, без канцелярита.", Why: "оба про слог",
	}}}
	if done, _, err := store.ApplyCompaction(ctx, plan, time.Now().UTC()); err != nil || done != 1 {
		t.Fatalf("слияние не прошло: %d, %v", done, err)
	}

	keep := взять(t, store, keepID)
	if len(keep.SeenJobs) != 4 {
		t.Fatalf("задания не объединены: %v", keep.SeenJobs)
	}
	if keep.Confirmations != 4 {
		t.Fatalf("счётчик не дотянут до объединения: %d вместо 4", keep.Confirmations)
	}
}

func TestPgУплотнениеИдётЧерезПравоИПроходитЗаслон(t *testing.T) {
	// Принятый план возвращается из браузера, и составитель мог его
	// править. Заслон стоит ЗДЕСЬ, а не там, где план составляли: иначе
	// достаточно было бы послать свою группу, чтобы переписать встроенное
	// правило или слить чужое требование.
	fake := &подставнаяМодель{}
	srv, token, store, _ := newDeskWith(t, fake, "prompts")

	keepID, victimID := свойID("ручка-жив"), свойID("ручка-слит")
	положить(t, store, keepID, FromLint, "Не пиши канцелярских оборотов в условии задачи.", nil)
	положить(t, store, victimID, FromCurator, "Пиши условие простыми предложениями.", nil)

	// Слить правило составителя в выведенное заслон не даст, и ответ
	// скажет об этом числами: просили одну группу, прошло ноль.
	status, body := call(t, srv, token, "POST", "/admin/api/rules/compaction/apply", map[string]any{
		"groups": []map[string]any{{
			"keepId": keepID, "mergeIds": []string{victimID},
			"text": "Пиши просто.", "why": "оба про слог",
		}},
	})
	if status != 200 {
		t.Fatalf("применение отказало: %d %v", status, body)
	}
	if body["asked"] != float64(1) || body["merged"] != float64(0) {
		t.Fatalf("заслон пропустил слияние правила составителя: %v", body)
	}
	if got := взять(t, store, victimID); got.Status == Merged {
		t.Fatal("правило составителя слито через ручку")
	}
}

// подставнаяМодель отвечает заготовленным: живая модель в проверках не
// нужна, а её отсутствие не должно прятать проверку самой ручки.
type подставнаяМодель struct{ answer string }

func (m *подставнаяМодель) Generate(context.Context, llm.Prompt, string) (string, llm.Usage, error) {
	return m.answer, llm.Usage{}, nil
}
