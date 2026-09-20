package gen

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Черновик задачи: то, что модель обязана вернуть, и то, что мы у неё
// принимаем.
//
// # Разбор отказывает, а не чинит
//
// Соблазн починить ответ модели велик: подставить пропущенный заголовок,
// выбросить ссылку на несуществующее положение, добить варианты до
// четырёх. Каждая такая починка делает задачу правдоподобной и неверной —
// а заметить это можно только прочитав её целиком, чего никто не делает с
// задачами, прошедшими конвейер. Поэтому непонятое не применяется:
// черновик, не сошедшийся с планом заказа, отбивается целиком, а задание
// уходит в отказ с причиной.
//
// # Почему условие разбито на фрагменты
//
// Разметка — половина ценности задачи: без неё обучающийся видит вердикт,
// но не видит, чем он обоснован. Фрагмент ссылается на положение
// источника его обозначением («абз. 1»), а не нашим порядковым номером:
// номер — наша выдумка, обозначение — язык первоисточника, и по нему врач
// находит положение в самом документе, а не только в нашей задаче.

// Segment — фрагмент условия.
type Segment struct {
	Text string `json:"text"`

	// Statements — обозначения положений, которые этот фрагмент
	// подтверждает. Пусто — фрагмент не подтверждает ничего, и это
	// законно: условие содержит и фон, без которого оно не читается.
	Statements []string `json:"statements,omitempty"`
}

// Option — вариант ответа.
type Option struct {
	// Label — метка единицы источника у задачи-узнавания. У
	// задачи-действия пусто: там вариант это текст действия, а не единица.
	Label string `json:"label,omitempty"`
	Text  string `json:"text"`
}

// Draft — черновик задачи, как его пишет модель.
type Draft struct {
	Title    string    `json:"title"`
	Segments []Segment `json:"segments"`
	Options  []Option  `json:"options"`

	// Answer — верный вариант: метка единицы у узнавания, текст действия
	// у задачи-действия.
	Answer string `json:"answer"`

	ExplanationMd string `json:"explanationMd"`

	// Difficulty — сложность, с которой задачу писали: 1 просто, 5 трудно.
	Difficulty int `json:"difficulty"`
}

// Stored — записанный черновик вместе с его опознавателем.
//
// Опознаватель не поле Draft, и это не дробность. Draft — то, что пишет
// модель, и он же ложится телом в case_drafts: заведи ID внутри него, и
// в записанном теле оказался бы номер строки, в которой оно лежит.
//
// Отдавать опознаватель студии при этом обязательно: принять черновик
// задачей — это `POST /admin/api/cases` с номером черновика, и без
// номера кнопке «Принять» не на чем стоять. Так дыра и держалась: ручка
// на сервере была, черновик студии показывался, а сослаться на него было
// нечем.
type Stored struct {
	ID int64 `json:"id"`
	Draft

	// Check — итог слепой сверки этого черновика. Пусто — сверки не было
	// (см. Check.Done); студия обязана показать это отдельно от
	// несогласия, иначе непроверенная задача читается как чистая.
	Check *Check `json:"check,omitempty"`

	// Siblings — итоги различающей сверки по неверным вариантам. Пусто —
	// сверки не было; ПУСТОЙ СПИСОК — сверять было нечего. Разные случаи,
	// и студия показывает их по-разному.
	Siblings *[]SiblingCheck `json:"siblings,omitempty"`

	// Proofread — итог вычитки. Пусто — вычитки не было вовсе; внутри с
	// done: false — попытка была и не удалась, и причина названа. Первое
	// значит, что язык задачи не смотрел никто.
	Proofread *Proofread `json:"proofread,omitempty"`

	// Cues — итог детектора подсказок. Пусто — детектор не дошёл; внутри
	// с done: false — дошёл и судить не смог, и причина названа.
	Cues *CueCheck `json:"cues,omitempty"`
}

// Rivals — неверные варианты черновика вместе с положениями их единиц.
//
// Берутся от ЧЕРНОВИКА, а не от плана: сверять надо то, что реально уйдёт
// обучающемуся. Круг плана — это кандидаты, и модель выбрала из них не
// обязательно всех; сверив кандидатов, мы проверили бы задачу, которой
// никто не увидит.
//
// Эталон отсеивается по метке у узнавания и по тексту у действия — то же
// различие, по которому мерится сам черновик. Вариант, которому в плане
// единицы не нашлось, возвращается БЕЗ положений, а не выбрасывается:
// несверенный вариант обязан назвать себя, иначе он неотличим от
// сверенного и чистого.
func (d Draft) Rivals(plan Plan) []UnitRef {
	answer := strings.TrimSpace(d.Answer)
	byLabel := make(map[string]UnitRef, len(plan.Siblings)+1)
	for _, s := range plan.Siblings {
		byLabel[strings.ToLower(s.Label)] = s
	}

	out := []UnitRef{}
	seen := map[string]bool{}
	for _, o := range d.Options {
		label := strings.TrimSpace(o.Label)
		text := strings.TrimSpace(o.Text)
		if plan.TaskKind == KindAction {
			// У действия вариант — это текст, и единицы за ним нет:
			// сверять его против положений соседа нечем. Такой круг
			// различающая сверка пропускает целиком, и это не пробел, а
			// разные предметы.
			continue
		}
		if label == "" || strings.EqualFold(label, answer) || strings.EqualFold(label, plan.Unit.Label) {
			continue
		}
		if seen[strings.ToLower(label)] {
			continue
		}
		seen[strings.ToLower(label)] = true
		ref, known := byLabel[strings.ToLower(label)]
		if !known {
			ref = UnitRef{Label: label, Title: text}
		}
		out = append(out, ref)
	}
	return out
}

// Condition — условие задачи целиком: склейка фрагментов.
//
// Отдельного поля с условием нет намеренно. Два места для одного текста
// расходятся молча, и разошлись бы они здесь тем хуже, что видно это
// только читающему задачу целиком: фрагменты правят при разметке, а поле
// с условием осталось бы прежним.
func (d Draft) Condition() string {
	parts := make([]string, 0, len(d.Segments))
	for _, s := range d.Segments {
		if text := strings.TrimSpace(s.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

// ParseDraft разбирает ответ модели.
//
// Ответ бывает обёрнут в ```json: модели делают это даже там, где схема
// запрещает, и ронять из-за обёртки готовую задачу незачем. Всё
// остальное — отказ.
func ParseDraft(answer string) (Draft, error) {
	text := strings.TrimSpace(answer)
	if text == "" {
		return Draft{}, errors.New("модель вернула пустой ответ")
	}
	if fenced := unfence(text); fenced != "" {
		text = fenced
	}

	var draft Draft
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&draft); err != nil {
		// Текст ответа в отказ не подклеивается: он уезжает в журнал
		// обращений целиком, а здесь мешал бы читать причину. Разбирают
		// такое по журналу, а не по строке отказа.
		return Draft{}, fmt.Errorf("ответ модели не разобран: %w", err)
	}
	return draft, nil
}

// unfence снимает ограду ```json, если она есть.
func unfence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return ""
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	if i := strings.LastIndex(text, "```"); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

// Validate сверяет черновик с планом заказа.
//
// Мерило здесь — план, а не источник: план собран при заказе и уехал в
// задание, а второе чтение источника разошлось бы с первым молча (см.
// шапку order.go). Всё, чего нет в плане, — выдумка модели.
func (d Draft) Validate(plan Plan) error {
	var faults []string
	add := func(format string, args ...any) { faults = append(faults, fmt.Sprintf(format, args...)) }

	if strings.TrimSpace(d.Title) == "" {
		add("у задачи нет заголовка")
	}
	if d.Difficulty < 1 || d.Difficulty > 5 {
		// Сложность вне шкалы — признак того, что модель заполняла поле
		// наугад, и доверять соседним полям того же ответа нет причин.
		add("сложность %d вне шкалы от 1 до 5", d.Difficulty)
	}
	if len(d.Segments) == 0 {
		add("условие не разбито на фрагменты")
	}
	if strings.TrimSpace(d.ExplanationMd) == "" {
		// Разбор — то, ради чего задача решается второй раз. Задача без
		// него учит угадывать, а не рассуждать.
		add("у задачи нет разбора")
	}

	faults = append(faults, d.checkStatements(plan)...)
	faults = append(faults, d.checkOptions(plan)...)

	if len(faults) == 0 {
		return nil
	}
	// Все несоответствия разом, а не первое попавшееся: отбивать черновик
	// по одному замечанию значит гонять модель столько раз, сколько в
	// ответе ошибок, — и платить за каждый заход.
	return fmt.Errorf("черновик не принят: %s", strings.Join(faults, "; "))
}

// checkStatements — ссылки разметки.
func (d Draft) checkStatements(plan Plan) []string {
	known := map[string]bool{}
	for _, designation := range plan.Designations() {
		known[strings.ToLower(designation)] = true
	}

	var faults []string
	marked := 0
	seen := map[string]bool{}
	for i, s := range d.Segments {
		if strings.TrimSpace(s.Text) == "" {
			faults = append(faults, fmt.Sprintf("фрагмент %d пуст", i+1))
		}
		for _, ref := range s.Statements {
			ref = strings.TrimSpace(ref)
			key := strings.ToLower(ref)
			if !known[key] {
				// Ссылка на несуществующее положение — выдумка, и молча
				// выброшенная она оставила бы фрагмент без разметки,
				// выглядящий размеченным.
				faults = append(faults, fmt.Sprintf(
					"фрагмент %d ссылается на %s %q, которого у единицы нет",
					i+1, plan.StatementWord, ref))
				continue
			}
			marked++
			seen[key] = true
		}
	}
	if marked == 0 && len(plan.Designations()) > 0 {
		// Разметка — половина ценности задачи: без неё обучающийся видит
		// вердикт, но не видит, чем он обоснован.
		faults = append(faults, "ни один фрагмент не размечен положениями")
	}
	return faults
}

// checkOptions — варианты ответа.
func (d Draft) checkOptions(plan Plan) []string {
	var faults []string
	if len(d.Options) < 3 {
		// Меньше трёх вариантов — это не выбор, а подсказка: угадать
		// верный можно, не читая условия.
		faults = append(faults, fmt.Sprintf("вариантов %d: меньше трёх — это не выбор", len(d.Options)))
	}

	texts := map[string]bool{}
	for i, o := range d.Options {
		text := strings.TrimSpace(o.Text)
		if text == "" {
			faults = append(faults, fmt.Sprintf("вариант %d пуст", i+1))
			continue
		}
		if texts[strings.ToLower(text)] {
			// Два одинаковых варианта сокращают выбор молча: обучающийся
			// видит четыре строки, а выбирает из трёх.
			faults = append(faults, fmt.Sprintf("вариант %q повторяется", text))
		}
		texts[strings.ToLower(text)] = true
	}

	answer := strings.TrimSpace(d.Answer)
	if answer == "" {
		return append(faults, "у задачи нет верного ответа")
	}

	if plan.TaskKind == KindAction {
		return append(faults, d.checkActionAnswer(plan, answer)...)
	}
	return append(faults, d.checkRecogniseAnswer(plan, answer)...)
}

// checkRecogniseAnswer — у задачи-узнавания эталон и варианты суть единицы
// источника.
func (d Draft) checkRecogniseAnswer(plan Plan, answer string) []string {
	var faults []string
	allowed := map[string]bool{strings.ToLower(plan.Unit.Label): true}
	for _, s := range plan.Siblings {
		allowed[strings.ToLower(s.Label)] = true
	}

	if !strings.EqualFold(answer, plan.Unit.Label) {
		// Эталон выбирает заказ, а не модель: задача, ответившая другой
		// единицей, отвечает не на тот вопрос, который заказывали.
		faults = append(faults, fmt.Sprintf(
			"верным назван %s %q, а заказан был %q", plan.UnitWord, answer, plan.Unit.Label))
	}

	answerAmong := false
	for i, o := range d.Options {
		label := strings.TrimSpace(o.Label)
		if label == "" {
			faults = append(faults, fmt.Sprintf("у варианта %d нет метки единицы", i+1))
			continue
		}
		if !allowed[strings.ToLower(label)] {
			// Круг различения собран при заказе из данных источника.
			// Единица со стороны — это либо выдумка, либо сосед, которого
			// источник соседом не считает.
			faults = append(faults, fmt.Sprintf(
				"вариант %q не из круга различения этого заказа", label))
		}
		if strings.EqualFold(label, answer) {
			answerAmong = true
		}
	}
	if !answerAmong {
		faults = append(faults, "верного ответа нет среди вариантов")
	}
	return faults
}

// checkActionAnswer — у задачи-действия эталон это текст действия.
func (d Draft) checkActionAnswer(plan Plan, answer string) []string {
	var faults []string
	for i, o := range d.Options {
		if strings.TrimSpace(o.Label) != "" {
			// Метка единицы у варианта-действия — признак того, что
			// модель написала задачу другого вида: варианты должны быть
			// действиями, а не единицами источника.
			faults = append(faults, fmt.Sprintf(
				"у варианта %d стоит метка единицы, а заказан был %s", i+1, KindWord(KindAction)))
		}
	}
	among := false
	for _, o := range d.Options {
		if strings.EqualFold(strings.TrimSpace(o.Text), answer) {
			among = true
		}
	}
	if !among {
		faults = append(faults, "верного ответа нет среди вариантов")
	}
	return faults
}

// DraftSchema — схема ответа модели для этого заказа.
//
// Схема строится под заказ, а не берётся общей: круг различения и
// обозначения положений у каждого заказа свои, и перечисленные в схеме
// значения не дают модели выдумать ни единицу со стороны, ни ссылку на
// несуществующее положение. Разбор всё равно проверяет то же самое:
// схему понимают не все шлюзы, и там, где её сняли, мерилом остаётся
// Validate.
func DraftSchema(plan Plan) json.RawMessage {
	statements := plan.Designations()
	sort.Strings(statements)

	option := map[string]any{
		"type":     "object",
		"required": []string{"text"},
		"properties": map[string]any{
			"text":  map[string]any{"type": "string"},
			"label": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}
	if plan.TaskKind != KindAction {
		labels := []string{plan.Unit.Label}
		for _, s := range plan.Siblings {
			labels = append(labels, s.Label)
		}
		option["required"] = []string{"text", "label"}
		option["properties"].(map[string]any)["label"] = map[string]any{
			"type": "string", "enum": labels,
		}
	}

	schema := map[string]any{
		"type":     "object",
		"required": []string{"title", "segments", "options", "answer", "explanationMd", "difficulty"},
		"properties": map[string]any{
			"title": map[string]any{"type": "string"},
			"segments": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items": map[string]any{
					"type":     "object",
					"required": []string{"text"},
					"properties": map[string]any{
						"text": map[string]any{"type": "string"},
						"statements": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string", "enum": statements},
						},
					},
					"additionalProperties": false,
				},
			},
			"options":       map[string]any{"type": "array", "minItems": 3, "items": option},
			"answer":        map[string]any{"type": "string"},
			"explanationMd": map[string]any{"type": "string"},
			"difficulty":    map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
		},
		"additionalProperties": false,
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		// Схема собрана из строк плана и в JSON не превращается только
		// при невозможном. Пустая схема означает «просим без схемы» —
		// хуже, чем просьба в задании, не бывает.
		return nil
	}
	return raw
}
