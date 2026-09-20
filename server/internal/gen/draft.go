package gen

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Черновик задачи: проза от модели, круг вариантов от сервера.
//
// # Что пишет модель, а что нет
//
// Модель пишет ПРОЗУ — заголовок, условие фрагментами, разметку и разбор
// (Composed). Круга вариантов она не пишет и не возвращает: его собрал
// сервер из данных источника ещё до обращения к ней (answers.go), и в
// задание он ушёл готовым. Прежде круг писала она, а разбор отбивал
// черновик, если вариант оказывался со стороны, — то есть мы платили за
// заход, чтобы узнать то, что и так знали.
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

// Composed — то, что модель возвращает: проза задачи и её разметка.
//
// Круга вариантов здесь нет намеренно, и его отсутствие держит схема
// ответа (ComposedSchema) вместе с DisallowUnknownFields при разборе:
// модель, приславшая варианты по привычке, получает отказ, а не тихо
// выброшенное поле. Тихо выброшенное, оно значило бы, что задание и
// схема разошлись, а узнали бы мы об этом по счёту за лишние заходы.
type Composed struct {
	Title         string    `json:"title"`
	Segments      []Segment `json:"segments"`
	ExplanationMd string    `json:"explanationMd"`

	// Difficulty — сложность, с которой задачу писали: 1 просто, 5 трудно.
	Difficulty int `json:"difficulty"`
}

// Draft — черновик задачи целиком: проза модели и круг сервера.
type Draft struct {
	Title    string    `json:"title"`
	Segments []Segment `json:"segments"`
	Options  []Option  `json:"options"`

	// Answer — верный вариант: метка единицы у узнавания, текст действия
	// у задачи-действия. Ставит его сервер по заказу, а не модель:
	// эталон выбирал составитель, и выбор этот не предмет переписки с
	// моделью.
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

// ParseComposed разбирает ответ модели.
//
// Ответ бывает обёрнут в ```json: модели делают это даже там, где схема
// запрещает, и ронять из-за обёртки готовую задачу незачем. Всё
// остальное — отказ.
func ParseComposed(answer string) (Composed, error) {
	text := strings.TrimSpace(answer)
	if text == "" {
		return Composed{}, errors.New("модель вернула пустой ответ")
	}
	if fenced := unfence(text); fenced != "" {
		text = fenced
	}

	var composed Composed
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&composed); err != nil {
		// Текст ответа в отказ не подклеивается: он уезжает в журнал
		// обращений целиком, а здесь мешал бы читать причину. Разбирают
		// такое по журналу, а не по строке отказа.
		return Composed{}, fmt.Errorf("ответ модели не разобран: %w", err)
	}
	return composed, nil
}

// Draft собирает черновик из написанного моделью и собранного сервером.
//
// Единственное место, где эти две половины сходятся. Второе такое место
// завело бы задачу, у которой варианты не те, что уехали в задание, и
// разошлись бы они молча.
func (c Composed) Draft(set AnswerSet) Draft {
	return Draft{
		Title:         c.Title,
		Segments:      c.Segments,
		Options:       set.Options(),
		Answer:        set.Answer,
		ExplanationMd: c.ExplanationMd,
		Difficulty:    c.Difficulty,
	}
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
func (c Composed) Validate(plan Plan) error {
	var faults []string
	add := func(format string, args ...any) { faults = append(faults, fmt.Sprintf(format, args...)) }

	if strings.TrimSpace(c.Title) == "" {
		add("у задачи нет заголовка")
	}
	if c.Difficulty < 1 || c.Difficulty > 5 {
		// Сложность вне шкалы — признак того, что модель заполняла поле
		// наугад, и доверять соседним полям того же ответа нет причин.
		add("сложность %d вне шкалы от 1 до 5", c.Difficulty)
	}
	if len(c.Segments) == 0 {
		add("условие не разбито на фрагменты")
	}
	if strings.TrimSpace(c.ExplanationMd) == "" {
		// Разбор — то, ради чего задача решается второй раз. Задача без
		// него учит угадывать, а не рассуждать.
		add("у задачи нет разбора")
	}

	faults = append(faults, c.checkStatements(plan)...)

	if len(faults) == 0 {
		return nil
	}
	// Все несоответствия разом, а не первое попавшееся: отбивать черновик
	// по одному замечанию значит гонять модель столько раз, сколько в
	// ответе ошибок, — и платить за каждый заход.
	return fmt.Errorf("черновик не принят: %s", strings.Join(faults, "; "))
}

// checkStatements — ссылки разметки.
func (c Composed) checkStatements(plan Plan) []string {
	known := map[string]bool{}
	for _, designation := range plan.Designations() {
		known[strings.ToLower(designation)] = true
	}

	var faults []string
	marked := 0
	seen := map[string]bool{}
	for i, s := range c.Segments {
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

// ComposedSchema — схема ответа модели для этого заказа.
//
// Схема строится под заказ, а не берётся общей: обозначения положений у
// каждого заказа свои, и перечисленные в схеме значения не дают модели
// сослаться на несуществующее положение. Разбор всё равно проверяет то же
// самое: схему понимают не все шлюзы, и там, где её сняли, мерилом
// остаётся Validate.
//
// Вариантов ответа в схеме нет, и это главное её изменение: круг собрал
// сервер и прислал модели готовым. Оставь мы поле — модель заполняла бы
// его старательно, мы бы его выбрасывали, и платили бы за выброшенное.
func ComposedSchema(plan Plan) json.RawMessage {
	statements := plan.Designations()
	sort.Strings(statements)

	schema := map[string]any{
		"type":     "object",
		"required": []string{"title", "segments", "explanationMd", "difficulty"},
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
