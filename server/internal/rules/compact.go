package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"curator/server/internal/llm"
)

// Уплотнение свода: слить сказанное дважды, сжать многословное.
//
// # Зачем
//
// Свод растёт сам. Правка составителя даёт правило, замечание детектора
// подсказок даёт правило, разбор дуэли даёт правило — и три источника,
// работая неделями, неминуемо приходят к одному и тому же разными
// словами: «не вписывай слово из положения», «показывай проявление, а не
// ярлык», «не называй признак его именем из источника». Все три верны.
// Все три уходят в каждый запрос.
//
// Место в блоке ограничено (coreChars, scopedChars, blockLimit), и
// упирается оно МОЛЧА: правило, не поместившееся в блок, выглядит
// действующим и не действует. Составитель видит в списке двадцать шесть
// правил и не знает, что до модели доехало шестнадцать. Уплотнение — не
// уборка ради красоты, а возврат места тем правилам, которые из блока
// вытеснены.
//
// # Четыре решения, которые нельзя ломать
//
// ПЕРВОЕ. Правило составителя не сливается ни в какое другое. Оно может
// стать выжившим в группе, но исчезнуть — нет: формулировка составителя
// есть требование, а не заготовка для пересказа. Модель, которой
// позволено переписать требование человека «покороче», однажды перепишет
// его во что-то другое, и заметить это будет негде.
//
// ВТОРОЕ. Правило с машинной проверкой не сливается тоже. Проверка
// привязана к своей формулировке: предикат про «супруга» стоит у правила
// про супруга. Слей его в общее правило о согласованности — и проверка
// уедет вместе со слитым правилом в закрытые, то есть перестанет
// работать, не сказав об этом. Выжившим такое правило быть может: его
// проверка при этом остаётся при нём.
//
// ТРЕТЬЕ. Область действия и род у группы общие. Слияние частного
// правила в общее расширило бы область молча — правило про один приказ
// начало бы уезжать во все, — а обратное молча сузило бы её. Род же
// задаёт вес в блоке: слив правило о существе дела в правило о слоге,
// мы перевели бы его в тот разряд, которым блок жертвует первым.
// Разошлись область или род — значит, модель собрала в группу не одно
// требование, а два соседних, и группа отклоняется целиком.
//
// ЧЕТВЁРТОЕ. Слитое закрывается датой со ссылкой на выжившее, а не
// удаляется. Свод дрейфует, вопрос «чего мы требовали на прошлой неделе»
// обязан иметь ответ, и слияние — как раз то действие, после которого
// его задают.
//
// # Почему план показывается составителю
//
// Уплотнение меняет то, что уходит модели, то есть меняет качество всех
// будущих задач. Делать это молча нельзя, и потому ручек две: одна
// предлагает план с числами «до» и «после», вторая проводит принятое.
// Между ними стоит человек.

// CompactionGroup — одна группа: что во что сливается.
type CompactionGroup struct {
	// KeepID — правило, которое остаётся. Из него же берутся счётчики и
	// происхождение: слияние не заводит новую сущность, а укрупняет
	// имеющуюся.
	KeepID string `json:"keepId"`

	// MergeIDs — правила, которые в него сливаются и закрываются датой.
	// Пустой список законен: так помечают многословное правило, которое
	// стоит сжать, не сливая ни с чем.
	MergeIDs []string `json:"mergeIds"`

	// Title и Text — сводные имя и формулировка. Имя пусто — прежнее
	// подходит.
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`

	// Why — чем группа обоснована. Читает составитель, и по этой строке
	// он принимает или отклоняет: «оба про слова из положений»
	// проверяемо, «похожи» — нет.
	Why string `json:"why"`

	// Saved — сколько байт блока освобождает группа. Считается КОДОМ, а
	// не моделью: числа модели проверять нечем.
	Saved int `json:"saved"`
}

// CompactionPlan — что предлагается сделать со сводом.
type CompactionPlan struct {
	Groups []CompactionGroup `json:"groups"`

	// Before и After — сколько байт занимают правила до и после
	// уплотнения. Ради этой пары чисел всё и затевается.
	Before int `json:"before"`
	After  int `json:"after"`

	// Limit — сколько байт вмещает блок, оба ведра вместе. Стоит рядом с
	// Before: «2600 из 3500» и «2600 из 2000» — разные новости, а число
	// одно.
	Limit int `json:"limit"`

	// Dropped — сколько правил не доезжает до модели СЕЙЧАС. То самое
	// молчаливое усечение, ради которого уплотнение и нужно.
	Dropped int `json:"dropped"`

	// Note — что модель сказала сверх групп. Model — кто отвечал.
	Note  string `json:"note,omitempty"`
	Model string `json:"model,omitempty"`
}

// BlockLimit — вместимость блока задания в байтах, оба ведра.
func BlockLimit() int { return coreChars + scopedChars }

// blockBytes — сколько байт занимают правила, уходящие в задание, и
// сколько их.
//
// Считается по тем же признакам, что и отбор в блок: действующее правило
// с непустым текстом. Считать иначе значило бы показать составителю одно
// число, а модели отправить другое.
func blockBytes(live []Rule) (bytes, count int) {
	for _, r := range live {
		bytes += ruleCost(r)
		count++
	}
	return bytes, count
}

// ruleCost — во сколько байт блока обходится правило. Одно место для
// этой арифметики: разойдись она с pick, и «освобождено 300 байт»
// перестало бы значить освобождённые байты.
func ruleCost(r Rule) int { return len(r.Text) + 3 } // «- » и перевод строки

// inForce — правила снимка, которые могут уйти в задание хоть куда-нибудь.
//
// Обстоятельства пустые намеренно: правило, вытесненное на одной задаче и
// прошедшее на другой, вытесненным не числится.
func (b Book) inForce() []Rule {
	out := make([]Rule, 0, len(b.rules))
	for _, r := range b.rules {
		if !r.InForce() || r.faded(b.now) || r.Text == "" {
			continue
		}
		out = append(out, r)
	}
	sortRules(out)
	return out
}

// Dropped — сколько действующих правил не доедет до модели при нынешнем
// своде.
//
// Считается тем же отбором, что и настоящий блок: спрашивать «сколько
// вытеснено» и отвечать по другому правилу значило бы успокаивать
// составителя числом, к делу не относящимся.
func (b Book) Dropped() int {
	live := b.inForce()
	scoped, core := b.pick(Context{})
	return len(live) - len(scoped) - len(core)
}

const compactSystemPrompt = `Ты уплотняешь свод правил, по которым пишутся учебные ситуационные задачи.

Свод растёт сам: правки составителя, замечания проверок и разборы дают правила неделями и неминуемо приходят к одному и тому же разными словами. Все такие правила уходят в каждый запрос к модели и занимают место, которого не хватает остальным.

Тебе дают действующие правила: опознаватель, происхождение, род, область действия, размер и текст. Найди группы, говорящие ОБ ОДНОМ, и предложи слить каждую в одно правило.

Незыблемые правила:
1. Сливать можно только то, что говорит об одном требовании. Два правила про разные требования, пусть и про соседние вещи, — это два правила. Свод, из которого вычеркнули половину смысла, компактнее и бесполезнее.
2. У всех правил группы обязаны совпадать род и область действия. Правило про один источник и правило, действующее всегда, — разные правила, даже если требуют похожего.
3. Если в группе есть правило составителя (происхождение «curator»), выжившим обязано быть оно. Его формулировку можно дополнить, но не заменить своей.
4. Сводный текст должен покрывать ВСЁ, что покрывали слитые. Потерянное требование — это ошибка, которая вернётся через неделю.
5. Сводный текст короче суммы слитых, иначе слияние бессмысленно. Пиши повелительно и без предисловий: это указание модели, а не пояснение человеку.
6. Не трогай встроенные правила (происхождение «builtin») и правила с пометкой «проверяется машиной»: у первых своя причина существовать, у вторых к формулировке привязана проверка.
7. Группа из одного правила законна: так помечают многословное правило, которое стоит сжать, не сливая ни с чем. Тогда mergeIds пуст, а text короче прежнего.
8. Ничего не нашёл — верни пустой список. Уплотнение ради уплотнения хуже разросшегося свода: оно теряет требования.
9. В "why" одной фразой скажи, ЧТО общего у правил группы. Составитель принимает или отклоняет по этой строке.

Отвечай одним объектом JSON без пояснений и без ограждения кодом.`

const compactUserPrompt = `ДЕЙСТВУЮЩИЕ ПРАВИЛА:

{{правила}}

Блок задания вмещает {{сколько}} байт. Сейчас правила занимают {{счёт}} байт.`

var compactSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"groups": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"keepId": {"type": "string"},
					"mergeIds": {"type": "array", "items": {"type": "string"}},
					"title": {"type": "string"},
					"text": {"type": "string"},
					"why": {"type": "string"}
				},
				"required": ["keepId", "mergeIds", "title", "text", "why"],
				"additionalProperties": false
			}
		},
		"note": {"type": "string"}
	},
	"required": ["groups", "note"],
	"additionalProperties": false
}`)

// Talker — тот, кто спрашивает модель. Объявлен здесь, а не взят из gen:
// gen сам ходит в rules, и обратная стрелка замкнула бы круг.
type Talker interface {
	Generate(ctx context.Context, p llm.Prompt, model string) (string, llm.Usage, error)
}

// SuggestCompaction просит модель найти, что слить.
//
// Своего задания в затравках у уплотнения нет намеренно. Затравки правит
// составитель, а это обращение решает, какие правила ему покажут на
// слияние; правленое задание позволило бы подсказать модели, что именно
// сливать, — то есть переписать свод, минуя тот самый показ, ради
// которого план и показывают.
func SuggestCompaction(ctx context.Context, talker Talker, model string, book Book) (CompactionPlan, error) {
	live := book.inForce()
	limit := BlockLimit()
	if len(live) < 2 {
		// Не отказ, а исход, и разница видна составителю: отказ приезжает
		// красной полосой «сбой шлюза», а исход — фразой о том, что делать
		// нечего. Свод из одного правила уплотнять действительно незачем.
		before, _ := blockBytes(live)
		return CompactionPlan{
			Groups: []CompactionGroup{},
			Before: before, After: before, Limit: limit,
			Note: "уплотнять нечего: в задание уходит меньше двух правил",
		}, nil
	}

	var list strings.Builder
	for _, r := range live {
		checked := ""
		if r.Check != nil {
			checked = ", проверяется машиной"
		}
		fmt.Fprintf(&list, "%s [%s, %s, область: %s%s, %d б.]: %s\n",
			r.ID, r.Source, r.Kind, scopeWords(r.Scope), checked, len(r.Text), r.Text)
	}
	before, count := blockBytes(live)

	replace := strings.NewReplacer(
		"{{правила}}", list.String(),
		"{{сколько}}", fmt.Sprint(limit),
		"{{счёт}}", fmt.Sprint(before),
	)

	answer, _, err := talker.Generate(ctx, llm.Prompt{
		System: compactSystemPrompt,
		User:   replace.Replace(compactUserPrompt),
		// Потолок ответа растёт со сводом: сводная формулировка на
		// правило плюс обоснование. Обрыв здесь стоил бы обращения
		// целиком — разобрать половину JSON нечем.
		MaxTokens: 600 + 200*count,
		// Слияние требований — работа на точность: выдумка здесь
		// означает потерянное требование.
		Temperature: 0.2,
		Schema:      compactSchema,
		SchemaName:  "rule_compaction",
	}, model)
	if err != nil {
		return CompactionPlan{}, err
	}

	var raw struct {
		Groups []CompactionGroup `json:"groups"`
		Note   string            `json:"note"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stripFence(answer))), &raw); err != nil {
		return CompactionPlan{}, fmt.Errorf("ответ модели не разобран: %w", err)
	}

	plan := Sanitize(raw.Groups, book)
	plan.Note = strings.TrimSpace(raw.Note)
	plan.Model = model
	return plan, nil
}

// stripFence снимает ограждение кодом, если модель его всё же поставила.
//
// Схема ответа просит голый JSON, и шлюз со строгим режимом его и
// вернёт. Но режим этот есть не у всякого поставщика, и обращение,
// отказавшее из-за трёх обратных кавычек, выглядит как отказ модели
// понять задание.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSuffix(strings.TrimSpace(s), "```")
}

// scopeWords — область действия словами, для списка правил в задании.
func scopeWords(s Scope) string {
	if s.always() {
		return "всегда"
	}
	var parts []string
	if len(s.Sources) > 0 {
		parts = append(parts, fmt.Sprintf("источники %v", s.Sources))
	}
	if len(s.Units) > 0 {
		parts = append(parts, "единицы "+strings.Join(s.Units, ", "))
	}
	if len(s.TaskKinds) > 0 {
		parts = append(parts, "виды "+strings.Join(s.TaskKinds, ", "))
	}
	if len(s.Nodes) > 0 {
		parts = append(parts, "узлы "+strings.Join(s.Nodes, ", "))
	}
	return strings.Join(parts, "; ")
}

// sameScope — две области совпадают дословно.
//
// Сравнение идёт по нормализованным областям, потому что порядок меток
// внутри области ничего не значит, а несравнимые по порядку списки
// объявили бы разными две одинаковые области — и уплотнение перестало
// бы находить что бы то ни было, оставаясь на вид работающим.
func sameScope(a, b Scope) bool {
	a.normalize()
	b.normalize()
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// Sanitize проверяет план по своду и считает числа сам.
//
// Всё, что модель сказала о размерах и о правах, пересчитывается здесь:
// числа модели проверять нечем, а права — единственное, что стоит между
// сводом и его молчаливой переписью. Через ту же дверь проходит план,
// вернувшийся из браузера: составитель мог отклонить часть групп, но
// дописать себе прав не мог.
func Sanitize(groups []CompactionGroup, book Book) CompactionPlan {
	live := book.inForce()
	byID := make(map[string]Rule, len(live))
	for _, r := range live {
		byID[r.ID] = r
	}

	before, _ := blockBytes(live)
	plan := CompactionPlan{
		Groups: []CompactionGroup{},
		Before: before,
		Limit:  BlockLimit(),
	}
	plan.Dropped = book.Dropped()

	// Правило не может попасть в две группы: слитое дважды исчезло бы
	// вместе с требованием, которое несло.
	taken := map[string]bool{}
	saved := 0

	for _, g := range groups {
		clean, ok := cleanGroup(g, byID, taken)
		if !ok {
			continue
		}
		taken[clean.KeepID] = true
		for _, id := range clean.MergeIDs {
			taken[id] = true
		}
		saved += clean.Saved
		plan.Groups = append(plan.Groups, clean)
	}

	// Порядок — по освобождённому месту: составитель читает список
	// сверху и бросает, когда надоест, а бросить он должен на мелочи.
	sort.SliceStable(plan.Groups, func(i, j int) bool {
		return plan.Groups[i].Saved > plan.Groups[j].Saved
	})
	plan.After = plan.Before - saved
	return plan
}

// cleanGroup выверяет одну группу. Негодная отбрасывается целиком, а не
// чинится: починенная группа — это уже не то, что предложила модель, и
// обоснование в Why относилось бы к другому слиянию.
func cleanGroup(g CompactionGroup, byID map[string]Rule, taken map[string]bool) (CompactionGroup, bool) {
	keep, ok := byID[strings.TrimSpace(g.KeepID)]
	if !ok || taken[keep.ID] || keep.Source == Builtin {
		return CompactionGroup{}, false
	}

	clean := CompactionGroup{
		KeepID:   keep.ID,
		MergeIDs: []string{},
		Title:    strings.TrimSpace(g.Title),
		Text:     strings.TrimSpace(g.Text),
		Why:      strings.TrimSpace(g.Why),
	}
	if clean.Text == "" || len(clean.Text) > TextMax || len(clean.Title) > TitleMax {
		// Сливать, не сказав чем, нельзя: выжившее правило должно
		// покрыть всё, что покрывали слитые. Потолки те же, что у
		// правила вообще: слияние — не повод их обойти.
		return CompactionGroup{}, false
	}

	// Названный слитым, но не слитый, роняет ВСЮ группу.
	//
	// Так, а не пропуском одного участника, и это куплено проверкой.
	// Пропуск превращал группу «слей А и Б» в группу «перепиши А» — и
	// она проходила, потому что сводный текст короче исходного. Итог:
	// правило Б остаётся в силе как было, а правило А переписано
	// формулировкой, которая сочинялась для них двоих. Ни составитель,
	// ни модель такого не предлагали, а выглядело это применённым
	// уплотнением.
	for _, id := range g.MergeIDs {
		id = strings.TrimSpace(id)
		victim, ok := byID[id]
		if !ok || id == keep.ID || taken[id] {
			return CompactionGroup{}, false
		}
		switch {
		case victim.Source == Builtin:
			// У встроенного своя причина существовать.
			return CompactionGroup{}, false
		case victim.Source == FromCurator:
			// Правило составителя может быть выжившим, но не слитым.
			return CompactionGroup{}, false
		case victim.Check != nil:
			// Проверка привязана к формулировке: слив её носителя, мы
			// погасили бы проверку молча.
			return CompactionGroup{}, false
		case victim.Kind != keep.Kind || !sameScope(victim.Scope, keep.Scope):
			// Разошлись род или область — значит, в группе не одно
			// требование, а два соседних.
			return CompactionGroup{}, false
		}
		clean.MergeIDs = append(clean.MergeIDs, id)
	}

	// Группа из одного правила законна только как сжатие: текст обязан
	// стать короче. Иначе это предложение переписать правило без причины.
	if len(clean.MergeIDs) == 0 && len(clean.Text) >= len(keep.Text) {
		return CompactionGroup{}, false
	}

	// Сколько байт блока это освобождает: сумма слитых минус прирост
	// (или плюс экономия) выжившего.
	clean.Saved = len(keep.Text) - len(clean.Text)
	for _, id := range clean.MergeIDs {
		clean.Saved += ruleCost(byID[id])
	}
	if clean.Saved <= 0 {
		// Уплотнение, не освобождающее места, — это переписывание свода
		// без причины.
		return CompactionGroup{}, false
	}
	return clean, true
}

// ApplyCompaction проводит план по своду.
//
// Возвращает, сколько групп применено. Одна непослушная группа не валит
// остальные: уплотнение — редакционное действие, и провалить его целиком
// из-за одной строки хуже, чем применить остальное. Но и молчать нельзя,
// и потому отказ по группе возвращается списком.
func (s *Store) ApplyCompaction(ctx context.Context, plan CompactionPlan, now time.Time) (int, []string, error) {
	done := 0
	var failed []string

	for _, g := range plan.Groups {
		if err := s.applyGroup(ctx, g, now); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", g.KeepID, err))
			continue
		}
		done++
	}
	return done, failed, nil
}

// applyGroup сливает одну группу.
//
// Счётчики слитых переходят выжившему по МАКСИМУМУ, а не по сумме:
// подтверждение считает РАЗНЫЕ задания, а множества заданий у слитых
// правил пересекаются неизвестно как, и сумма объявила бы кворум там,
// где его нет. Множества виденных заданий при этом объединяются, и
// подтверждения дотягиваются до размера объединения, если он больше.
func (s *Store) applyGroup(ctx context.Context, g CompactionGroup, now time.Time) error {
	all, err := s.All(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]Rule, len(all))
	for _, r := range all {
		byID[r.ID] = r
	}

	keep, ok := byID[g.KeepID]
	if !ok {
		return fmt.Errorf("правила больше нет в своде")
	}

	seen := map[int64]bool{}
	for _, id := range keep.SeenJobs {
		seen[id] = true
	}
	best := keep.Confirmations
	for _, id := range g.MergeIDs {
		victim, ok := byID[id]
		if !ok {
			continue
		}
		for _, job := range victim.SeenJobs {
			seen[job] = true
		}
		if victim.Confirmations > best {
			best = victim.Confirmations
		}
	}

	keep.Text = g.Text
	if g.Title != "" {
		keep.Title = g.Title
	}
	keep.SeenJobs = sortedJobs(seen)
	if n := len(keep.SeenJobs); n > best {
		best = n
	}
	keep.Confirmations = best
	if _, err := s.Save(ctx, keep); err != nil {
		return err
	}

	// Слитое закрывается датой со ссылкой на выжившее. Не удаляется:
	// вопрос «чего мы требовали на прошлой неделе» обязан иметь ответ.
	for _, id := range g.MergeIDs {
		victim, ok := byID[id]
		if !ok {
			continue
		}
		closed := now
		victim.Status = Merged
		victim.MergedInto = keep.ID
		victim.ValidTo = &closed
		victim.Pinned = true // закрытие решено человеком, счётчик его не отменяет
		if _, err := s.Save(ctx, victim); err != nil {
			return fmt.Errorf("правило %s не закрыто: %w", id, err)
		}
	}
	return nil
}

func sortedJobs(set map[int64]bool) []int64 {
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
