package gen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"curator/server/internal/llm"
	"curator/server/internal/llmusage"
	"curator/server/internal/source"
)

// Разбор документа моделью: первый узел сквозного пути.
//
// До этого узла источник наполнялся руками: ручка черновика была, студия её
// не звала, и в пояснении к ней так и стояло — «пока генерация не приехала,
// черновик кладут руками». Приехала.
//
// # Два правила держат затею, и оба исполняются заслоном, а не просьбой
//
//   - Извлечённое ложится ЧЕРНОВИКОМ (source_draft_*), не в источник: в
//     единицы источника переносит только человек, отдельным решением.
//   - Непонятое не применяется: единица без метки или названия, положение
//     без текста или с меткой, которой нет среди единиц, отбрасываются со
//     счётом; ответ вовсе без годных единиц — целиком и вслух.
//
// # Почему документ идёт частями
//
// Не «по N килобайт»: часть — это подряд идущие куски документа, пока они
// укладываются в потолок. Куски режет разбор текста по заголовкам
// (source.SplitText), то есть по тем местам, где сам автор документа сказал
// «здесь новая мысль»; положение, разрезанное посередине, не разберёт никто.
//
// Платой за нерезаный документ у донора был боевой отказ: рекомендация в
// 289 КБ ушла одним обращением, не уложилась в срок, и после отказа не
// осталось НИЧЕГО — ни разобранной половины, ни возможности продолжить.
// Поэтому каждая часть пишется в черновик сразу, как разобралась: всё, что
// записано, обрыв уже не отнимет.

// MaxPartChars — сколько знаков документа уходит модели одним обращением.
//
// Считается от ПОТОЛКА ОТВЕТА, а не от длины контекста, и это не
// осторожность. Положения переносятся дословно, то есть ответ возвращает
// почти весь поданный текст, разложенный по полям JSON, — вместе с
// обрамлением он выходит ДЛИННЕЕ входа. Потолок ответа у этого узла 16000
// токенов, русский текст — примерно два с половиной знака на токен, и
// сорока тысяч знаков ответу хватает впритык. Двадцать тысяч на вход
// оставляют запас на обрамление и на то, что модель считает токены не так,
// как мы.
//
// Упереться в потолок ответа здесь дороже, чем кажется: обрыв оплачен
// целиком и не даёт ничего (см. llm.ErrTruncated).
const MaxPartChars = 20000

// ParsePlan — заказ разбора, разрешённый при заказе.
//
// Словарь источника и смысл вложенности едут в задании: модель размечает
// приказ словами приказа, а не словами первого источника. Разрешается это
// при заказе и по тому же доводу, что и план задачи (см. шапку order.go):
// второе чтение разошлось бы с первым молча.
type ParsePlan struct {
	SourceID   int64 `json:"sourceId"`
	DocumentID int64 `json:"documentId"`

	Title         string `json:"title"`
	UnitWord      string `json:"unitWord"`
	StatementWord string `json:"statementWord"`
	Hierarchy     string `json:"hierarchy"`

	// Filename — имя принесённого файла: им разбор назван в списке заданий.
	// Номер документа человеку ничего не говорит.
	Filename string `json:"filename,omitempty"`

	// Model — просьба разобрать этой моделью. Пусто — моделью узла.
	Model string `json:"model,omitempty"`
}

// ResolveParse собирает заказ разбора из документа и его источника.
//
// Источник берётся У ДОКУМЕНТА, а не приходит доводом снаружи. Разница не
// формальная: черновик ложится ПРИ документе, а принимается В источник, и
// заказ, назвавший источник сам, перенёс бы чужой приказ в открытый
// составителем источник. Ровно этот отказ уже был у донора, и ответ при
// нём выглядел успешным — заметить подмену было нечем.
//
// Отказывает, а не додумывает: документа нет, документ ни к какому
// источнику не привязан, источника нет — разбирать нечего и класть некуда.
func (r *Resolver) ResolveParse(ctx context.Context, documentID int64, model string) (ParsePlan, error) {
	plan := ParsePlan{DocumentID: documentID, Model: strings.TrimSpace(model)}

	var sourceID *int64
	err := r.gate.QueryRow(ctx,
		`SELECT filename, source_id FROM source_documents WHERE id = $1`, documentID).
		Scan(&plan.Filename, &sourceID)
	if err != nil {
		return ParsePlan{}, fmt.Errorf("документа %d нет", documentID)
	}
	if sourceID == nil || *sourceID == 0 {
		return ParsePlan{}, errors.New(
			"документ не привязан к источнику: разобранное будет некуда принимать")
	}
	plan.SourceID = *sourceID

	err = r.gate.QueryRow(ctx,
		`SELECT title, unit_word, statement_word, hierarchy
		   FROM sources WHERE id = $1`, plan.SourceID).
		Scan(&plan.Title, &plan.UnitWord, &plan.StatementWord, &plan.Hierarchy)
	if err != nil {
		return ParsePlan{}, fmt.Errorf("источник %d не найден: %w", plan.SourceID, err)
	}
	return plan, nil
}

// DraftKeeper — то, куда ложится разобранное.
//
// Интерфейсом, а не хранилищем источника: подставной хранитель в проверках
// запоминает положенное, и весь узел проверяется без базы. Ему же довод,
// почему методов три, а не один: черновик чистится ОДИН раз в начале, а
// пишется по частям — всё, что записано, обрыв уже не отнимает.
type DraftKeeper interface {
	Fragments(ctx context.Context, documentID int64) ([]source.Fragment, error)
	ClearDraft(ctx context.Context, documentID int64) error
	AppendDraft(ctx context.Context, documentID int64, units []source.Unit, statements []source.Statement) error
}

// ParseRunner ведёт разбор документа по частям.
type ParseRunner struct {
	jobs    *Jobs
	prompts *Prompts
	talker  Talker
	keeper  DraftKeeper

	ledger *llmusage.Store
	prices llm.Prices
}

// NewParseRunner собирает исполнителя разбора.
func NewParseRunner(jobs *Jobs, prompts *Prompts, talker Talker, keeper DraftKeeper) *ParseRunner {
	return &ParseRunner{jobs: jobs, prompts: prompts, talker: talker, keeper: keeper}
}

// WithLedger добавляет учёт расхода.
func (r *ParseRunner) WithLedger(ledger *llmusage.Store, prices llm.Prices) *ParseRunner {
	r.ledger, r.prices = ledger, prices
	return r
}

// ParseResult — чем кончился разбор.
type ParseResult struct {
	JobID      int64
	DocumentID int64

	Parts      int
	PartsDone  int
	Units      int
	Statements int

	// Dropped — сколько непонятого отброшено заслоном. Числом и вслух:
	// молча отброшенная половина ответа выглядит как «документ беден», и
	// объяснить это нечем.
	Dropped int
}

// RunNext берёт следующий разбор и проводит его по частям.
//
// Второй ответ — нашлось ли задание. Пустая очередь не отказ, и
// исполнитель, принявший её за отказ, начал бы её чинить.
func (r *ParseRunner) RunNext(ctx context.Context) (ParseResult, bool, error) {
	job, ok, err := r.jobs.Take(ctx, KindParse)
	if err != nil || !ok {
		return ParseResult{}, false, err
	}
	result, err := r.run(ctx, job)
	if err != nil {
		if failErr := r.jobs.Fail(ctx, job.ID, err.Error()); failErr != nil {
			return ParseResult{}, true, fmt.Errorf("%w (и отказ не записан: %v)", err, failErr)
		}
		return ParseResult{}, true, err
	}
	if err := r.jobs.Done(ctx, job.ID); err != nil {
		return result, true, err
	}
	return result, true, nil
}

func (r *ParseRunner) run(ctx context.Context, job Job) (ParseResult, error) {
	plan := job.ParsePlan
	fragments, err := r.keeper.Fragments(ctx, plan.DocumentID)
	if err != nil {
		return ParseResult{}, err
	}
	parts := SplitParts(fragments)
	if len(parts) == 0 {
		return ParseResult{}, errors.New("в документе нет ни одного куска: разбирать нечего")
	}

	// Чистка — ОДИН раз и до первого обращения: части пишутся по мере
	// разбора, и чистка перед каждой стирала бы разобранное соседями.
	// Прежний черновик при этом уходит целиком, а не дополняется: два
	// разбора, слитые в один, — это разбор, которого не делал никто.
	if err := r.keeper.ClearDraft(ctx, plan.DocumentID); err != nil {
		return ParseResult{}, err
	}

	result := ParseResult{JobID: job.ID, DocumentID: plan.DocumentID, Parts: len(parts)}

	// Метки, уже принятые заслоном. Копятся по всем частям, а не внутри
	// одной: положение из части третьей ссылается на единицу из части
	// первой сплошь и рядом, и заслон, помнящий только свою часть,
	// отбрасывал бы исправное.
	known := map[string]bool{}

	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			// Отмена проверяется между частями, а не посреди обращения:
			// оборванное обращение оплачено так же, как доведённое до конца.
			return result, err
		}
		step := fmt.Sprintf("часть %d из %d", i+1, len(parts))
		if err := r.jobs.Step(ctx, job.ID, step); err != nil {
			return result, err
		}

		units, statements, dropped, err := r.parsePart(ctx, job, plan, part, i, len(parts))
		result.Dropped += dropped
		if err != nil {
			// Отброшена ЭТА часть, а не работа целиком, и отброшенное
			// названо: какая часть и почему. Соседние идут дальше — они
			// уже оплачены или ещё будут оплачены, и терять их из-за
			// одной негодной незачем.
			r.note(ctx, job.ID, fmt.Sprintf("%s (%s) не разобралась: %v", step, part.Title, err))
			continue
		}

		units, statements = keepNew(units, statements, known)
		if err := r.keeper.AppendDraft(ctx, plan.DocumentID, units, statements); err != nil {
			r.note(ctx, job.ID, fmt.Sprintf("%s разобралась, но не сохранилась: %v", step, err))
			continue
		}
		result.PartsDone++
		result.Units += len(units)
		result.Statements += len(statements)
		if dropped > 0 {
			r.note(ctx, job.ID, fmt.Sprintf(
				"%s (%s): отброшено непонятого из ответа модели: %d", step, part.Title, dropped))
		}
	}

	if result.PartsDone == 0 {
		// Ни одной разобранной части — это неудача, а не «выполнено»:
		// зелёная отметка над пустым черновиком врёт. Причины уже названы
		// замечаниями по частям, и второй раз их не пишем.
		return result, errors.New(
			"ни одна часть документа не разобралась — смотрите замечания к заданию")
	}
	return result, nil
}

// parsePart — одно обращение к модели по одной части документа.
func (r *ParseRunner) parsePart(ctx context.Context, job Job, plan ParsePlan,
	part Part, at, total int) ([]source.Unit, []source.Statement, int, error) {

	prompt, err := r.prompts.ForNode(ctx, NodeParse)
	if err != nil {
		return nil, nil, 0, err
	}

	// Словарь источника подставляется общим Render'ом, документ и место —
	// здесь: это не свойства заказа, а то, что подаётся модели сейчас.
	// Попади они в общий список переменных — оказались бы доступны и узлу
	// написания, то есть задание написания показывало бы модели документ.
	vars := Plan{
		Title: plan.Title, UnitWord: plan.UnitWord,
		StatementWord: plan.StatementWord, Hierarchy: plan.Hierarchy,
	}
	user := Render(prompt.UserMd, vars)
	user = strings.ReplaceAll(user, "{место}", placeLine(part, at, total))
	user = strings.ReplaceAll(user, "{документ}", part.Body)

	answer, err := r.ask(ctx, job, llm.Prompt{
		System: Render(prompt.SystemMd, vars),
		User:   user,
		// Разбор длинный: документ возвращается почти целиком,
		// разложенный по положениям. Обычного потолка ему мало.
		MaxTokens: 16000,
		// Разбору нужна дословность, а не разнообразие: положение
		// переносится как написано.
		Temperature: 0.1,
		Schema:      parseSchema,
		SchemaName:  "source_parse",
	})
	if err != nil {
		return nil, nil, 0, err
	}
	return SanitizeParsed(answer)
}

// ask — одно обращение к модели с записью в учёт.
func (r *ParseRunner) ask(ctx context.Context, job Job, prompt llm.Prompt) (string, error) {
	started := time.Now()
	answer, usage, err := r.talker.Generate(ctx, prompt, job.ParsePlan.Model)
	record(ctx, r.ledger, r.prices, job.ID, NodeParse, job.ParsePlan.Model,
		prompt, answer, usage, time.Since(started), err)
	if err != nil {
		if errors.Is(err, llm.ErrTruncated) {
			// Обрыв здесь лечится не повтором, а меньшей частью, и сказать
			// об этом прямо полезнее, чем показать «ошибку разбора».
			return "", errors.New("часть не уместилась в потолок ответа — она слишком велика для одного обращения")
		}
		return "", err
	}
	return answer, nil
}

// note записывает замечание, не роняя работу.
//
// Замечание — рассказ о сделанном, и потерять из-за него сделанное значит
// заплатить дважды. Но и молчать нельзя: в журнале службы след остаётся.
func (r *ParseRunner) note(ctx context.Context, jobID int64, text string) {
	if err := r.jobs.Note(ctx, jobID, text); err != nil {
		log.Printf("разбор: замечание к заданию %d не записано: %v", jobID, err)
	}
}

// Part — часть документа, уходящая модели одним обращением.
type Part struct {
	// Title — заголовок, по которому человек находит часть в документе.
	// Пусто у документа без заголовков: там весь текст — одна часть.
	Title string
	Body  string
}

// SplitParts собирает куски документа в части под потолком.
//
// Подряд идущие куски, пока укладываются: так часть остаётся связным
// местом документа, а не набором отрывков из разных мест. Кусок, который
// сам не укладывается в потолок, идёт частью в одиночку и не режется —
// резать его можно только посередине положения, а положение, разрезанное
// посередине, не разберёт никто. Такая часть, скорее всего, упрётся в
// потолок ответа; это честный отказ по одной части, и соседние от него не
// страдают.
func SplitParts(fragments []source.Fragment) []Part {
	out := []Part{}
	var cur strings.Builder
	title := ""

	flush := func() {
		if body := strings.TrimSpace(cur.String()); body != "" {
			out = append(out, Part{Title: title, Body: body})
		}
		cur.Reset()
		title = ""
	}

	for _, f := range fragments {
		body := fragmentText(f)
		if body == "" {
			continue
		}
		// Часть закрывается ПЕРЕД тем, как переполнится, а не после:
		// проверка после добавления оставила бы кусок за потолком ровно
		// на его длину.
		if cur.Len() > 0 && cur.Len()+len(body)+2 > MaxPartChars {
			flush()
		}
		if cur.Len() == 0 {
			title = f.Title
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(body)
	}
	flush()
	return out
}

// fragmentText собирает кусок обратно в текст вместе с заголовком.
//
// Заголовок уходит модели вместе с телом намеренно: без него кусок теряет
// то единственное, что говорит, о чём он, — и модель дописывает это сама,
// то есть выдумывает. То же правило, что у сохранения кусков
// (source.SaveFragments); написано здесь второй раз потому, что читается
// здесь другое — кусок в памяти, а не строка базы.
func fragmentText(f source.Fragment) string {
	head := strings.TrimSpace(f.Title)
	if head != "" && f.Level > 0 {
		head = strings.Repeat("#", f.Level) + " " + head
	}
	body := strings.TrimSpace(f.Body)
	switch {
	case head == "":
		return body
	case body == "":
		return head
	default:
		return head + "\n\n" + body
	}
}

// placeLine — какое место документа перед моделью.
//
// Сказать это обязательно: не зная, что перед ней часть, модель
// достраивает недостающее по памяти — ровно то, что запрещено ей первым
// правилом задания. У документа из одной части строка пуста: сказать
// «часть 1 из 1» значит заставить модель искать соседние части, которых
// нет.
func placeLine(part Part, at, total int) string {
	if total <= 1 {
		return ""
	}
	place := fmt.Sprintf("Перед вами часть %d из %d", at+1, total)
	if title := strings.TrimSpace(part.Title); title != "" {
		place += fmt.Sprintf(" — «%s»", title)
	}
	return place + ". Размечайте только её."
}

// parsedAnswer — как модель обязана оформить ответ.
type parsedAnswer struct {
	Units []struct {
		Label  string `json:"label"`
		Title  string `json:"title"`
		Parent string `json:"parent"`
		Group  bool   `json:"group"`
	} `json:"units"`
	Statements []struct {
		Unit        string `json:"unit"`
		Designation string `json:"designation"`
		Text        string `json:"text"`
		Place       string `json:"place"`
	} `json:"statements"`
}

// SanitizeParsed — заслон разбора: непонятое отбрасывается со счётом.
//
// Правила закрытые и машинные, не на глаз:
//   - единица без метки или названия — не единица; повтор метки внутри
//     одного ответа — второй ответ модели о том же месте, берётся первый;
//   - положение без текста — не положение; положение с меткой, которой нет
//     среди единиц этого ответа, подтверждать не рядом с чем;
//   - ответ вовсе без годных единиц отбрасывается целиком: это не разбор
//     части, а его отсутствие, и класть его черновиком значило бы
//     подменить документ пустотой молча.
//
// Порядок единиц и положений — порядок ответа, а не поле ord из него.
// Номер, названный моделью, — это её счёт, и у части третьей он начнётся
// заново с нуля; порядок же в черновике сквозной, и считает его тот, кто
// складывает части (AppendDraft).
func SanitizeParsed(answer string) ([]source.Unit, []source.Statement, int, error) {
	text := strings.TrimSpace(answer)
	if text == "" {
		return nil, nil, 0, errors.New("модель вернула пустой ответ")
	}
	if fenced := unfence(text); fenced != "" {
		text = fenced
	}

	var parsed parsedAnswer
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		// Текст ответа в отказ не подклеивается: он уезжает в журнал
		// обращений целиком, а здесь мешал бы читать причину.
		return nil, nil, 0, fmt.Errorf("ответ разбора не разобран: %w", err)
	}

	dropped := 0
	seen := map[string]bool{}
	units := []source.Unit{}
	for _, u := range parsed.Units {
		label := strings.TrimSpace(u.Label)
		title := strings.TrimSpace(u.Title)
		if label == "" || title == "" || seen[label] {
			dropped++
			continue
		}
		seen[label] = true
		kind := "entry"
		if u.Group {
			// Род доезжает до приёмки через черновик. Без него приёмка
			// ставила бы всем «запись», и групп не бывало бы вовсе — а без
			// них справочник в девятьсот строк остаётся без входа.
			kind = "group"
		}
		units = append(units, source.Unit{
			Label:       label,
			ParentLabel: strings.TrimSpace(u.Parent),
			Title:       title,
			Kind:        kind,
		})
	}
	if len(units) == 0 {
		return nil, nil, dropped, errors.New(
			"модель не выделила ни одной единицы — разбор отброшен, документ не тронут")
	}

	statements := []source.Statement{}
	for _, s := range parsed.Statements {
		unit := strings.TrimSpace(s.Unit)
		body := strings.TrimSpace(s.Text)
		if unit == "" || body == "" || !seen[unit] {
			dropped++
			continue
		}
		statements = append(statements, source.Statement{
			UnitLabel:   unit,
			Designation: strings.TrimSpace(s.Designation),
			Body:        body,
			PlaceRef:    strings.TrimSpace(s.Place),
		})
	}
	return units, statements, dropped, nil
}

// keepNew отсеивает то, что уже пришло из прежних частей.
//
// Метка единицы у черновика одна на документ (указатель базы), и вторая
// часть, назвавшая ту же единицу, — это её второе упоминание, а не вторая
// единица. Берётся первое: оно из того места документа, где единица
// объявлена, а повтор — из места, где на неё сослались.
//
// Положения при этом НЕ отсеиваются по единице: положение из этой части
// ссылается и на единицу из прежней, и это обычный случай. Отсеивается
// только то, чья единица не встречалась нигде — заслон своей части такое
// уже убрал, а здесь убирается то, что своя часть пропустила.
func keepNew(units []source.Unit, statements []source.Statement, known map[string]bool) ([]source.Unit, []source.Statement) {
	fresh := make([]source.Unit, 0, len(units))
	for _, u := range units {
		if known[u.Label] {
			continue
		}
		known[u.Label] = true
		fresh = append(fresh, u)
	}
	keep := make([]source.Statement, 0, len(statements))
	for _, st := range statements {
		if !known[st.UnitLabel] {
			continue
		}
		keep = append(keep, st)
	}
	return fresh, keep
}

// ParseWork разбирает очередь разбора, пока не отменят.
//
// Свой цикл, а не общий с написанием задач, и это не удвоение. Разбор идёт
// минутами и держит одно задание надолго; написание задач, стоящее за ним
// в общей очереди, ждало бы конца разбора восьмидесяти частей. Родам,
// которые ждут друг друга без причины, общая очередь — не бережливость, а
// затор.
func ParseWork(ctx context.Context, runner *ParseRunner) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		result, took, err := runner.RunNext(ctx)
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return
		case err != nil:
			log.Printf("разбор: задание не выполнено: %v", err)
		case took:
			log.Printf("разбор: задание %d, документ %d: частей %d из %d, единиц %d, положений %d, отброшено %d",
				result.JobID, result.DocumentID, result.PartsDone, result.Parts,
				result.Units, result.Statements, result.Dropped)
		}
		if took {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(Idle):
		}
	}
}

// parseSchema — строгая схема ответа разбора.
//
// В required перечислены все поля: строгий режим OpenAI-совместимых шлюзов
// требует полного перечня, а пустые значения разбору не мешают — заслон
// SanitizeParsed отбрасывает негодное сам.
var parseSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"units": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"label": {"type": "string"},
					"title": {"type": "string"},
					"parent": {"type": "string"},
					"group": {"type": "boolean"}
				},
				"required": ["label", "title", "parent", "group"],
				"additionalProperties": false
			}
		},
		"statements": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"unit": {"type": "string"},
					"designation": {"type": "string"},
					"text": {"type": "string"},
					"place": {"type": "string"}
				},
				"required": ["unit", "designation", "text", "place"],
				"additionalProperties": false
			}
		}
	},
	"required": ["units", "statements"],
	"additionalProperties": false
}`)

// Хранилище источника как хранитель черновика: проверка того, что
// source.Store и правда умеет всё, чего ждёт узел. Без неё расхождение
// нашлось бы только в main, где его чинить дороже.
var _ DraftKeeper = (*source.Store)(nil)
