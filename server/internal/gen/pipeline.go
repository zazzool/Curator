package gen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"
	"time"

	"curator/server/internal/llm"
	"curator/server/internal/llmusage"
	"curator/server/internal/rules"
)

// Конвейер: что происходит с заданием от очереди до черновика.
//
// Узлов пока два — написание и слепая сверка. Порядок их не случаен и не
// переставляется настройкой: сверять нечего, пока не написано, а писать
// после сверки значит сверять прошлый черновик.

// Talker — то, через что конвейер обращается к моделям. Интерфейсом, а не
// llm.Chain, ровно ради проверок: подставная модель отвечает заготовленным,
// и весь конвейер проверяется без сети.
type Talker interface {
	Generate(ctx context.Context, p llm.Prompt, model string) (string, llm.Usage, error)
}

// Runner ведёт задание по узлам.
type Runner struct {
	jobs    *Jobs
	prompts *Prompts
	talker  Talker

	// ledger и prices — учёт. Пусто — не записываем; так конвейер
	// проверяется без базы учёта, но на бою пусто не бывает: обращение,
	// нигде не записанное, невозможно ни разобрать, ни посчитать.
	ledger *llmusage.Store
	prices llm.Prices

	// rules — свод правил. Пусто — свода нет вовсе, и конвейер работает
	// как до него; так он проверяется без базы свода. На бою пусто не
	// бывает.
	//
	// Различие между «свода нет» и «свод не прочитался» здесь — не
	// придирка, а то же правило, что у детектора подсказок: задача,
	// написанная без свода, выглядит обычной работой, и заметить утрату
	// можно только по тому, что ошибки, от которых свод и заведён,
	// вернулись все разом.
	rules *rules.Store

	// shuffle — перемешивание вариантов перед слепой сверкой. Полем ради
	// проверок: сверка, у которой верный вариант всегда первый, проверяет
	// не то, что надо.
	shuffle func(n int) []int
}

// NewRunner собирает конвейер.
func NewRunner(jobs *Jobs, prompts *Prompts, talker Talker) *Runner {
	return &Runner{jobs: jobs, prompts: prompts, talker: talker, shuffle: rand.Perm}
}

// WithRules подключает свод правил.
func (r *Runner) WithRules(store *rules.Store) *Runner {
	r.rules = store
	return r
}

// WithLedger добавляет учёт расхода.
func (r *Runner) WithLedger(ledger *llmusage.Store, prices llm.Prices) *Runner {
	r.ledger, r.prices = ledger, prices
	return r
}

// Result — чем кончилось задание.
type Result struct {
	JobID   int64
	DraftID int64
	Draft   Draft

	// Verdict — что сказала слепая сверка. Пусто — сверка не дошла до
	// ответа; это не то же самое, что «сверка не согласна», и смешивать
	// их нельзя: несогласие означает работу, а несостоявшаяся сверка —
	// что задачу никто не проверял.
	Verdict *Verdict

	// Check — то же самое вместе с причиной, по которой сверки не было.
	// Verdict рядом оставлен намеренно: по нему ходит прежний вызывающий
	// код, и менять его ради переименования значит править то, что
	// работает, ради красоты.
	Check Check

	// Siblings — итоги различающей сверки по каждому неверному варианту.
	Siblings []SiblingCheck

	// Cues — итог детектора подсказок.
	Cues CueCheck

	// Rules — итог судьи: машинные проверки свода.
	Rules RuleCheckResult

	// Proofread — итог вычитки: что правлено и что отклонено заслоном.
	Proofread Proofread
}

// Check — что известно о слепой сверке черновика.
//
// Отдельным от Verdict типом, потому что вопросов два, и ответ на первый
// не выражается вторым: сверка СОСТОЯЛАСЬ или нет, и если состоялась —
// сошлась или нет. Держи мы одно поле, несостоявшаяся сверка была бы
// неотличима от несогласной либо от согласной, и оба смешения врут в
// худшую сторону: первое поднимает тревогу там, где её нет, второе
// выдаёт непроверенное за чистое.
type Check struct {
	// Done — сверка дошла до ответа. Ложь — Verdict пуст, а Note говорит
	// почему.
	Done bool `json:"done"`

	// Note — почему сверка не состоялась. Кончились деньги у поставщика,
	// отменили задание, модель вернула не тот JSON — на глаз одинаково
	// пустой разбор у сотни задач подряд, а делать надо разное.
	Note string `json:"note,omitempty"`

	Verdict *Verdict `json:"verdict,omitempty"`

	// Arbitration — разбор расхождения. Пусто — не разбирали: либо
	// сверка сошлась и разбирать было нечего, либо черновик старше узла.
	//
	// Стоит РЯДОМ с вердиктом, а не заменяет его. Перепиши разбор
	// Verdict.Agrees — и исчезла бы единственная запись о том, что
	// слепая сверка вообще возражала; спор, о котором нельзя узнать,
	// что он был, неотличим от отсутствия спора.
	Arbitration *Arbitration `json:"arbitration,omitempty"`

	// ArbitrationNote — почему разбора не было при расхождении. Пусто,
	// когда разбор состоялся или когда разбирать было нечего.
	ArbitrationNote string `json:"arbitrationNote,omitempty"`
}

// Agrees — сошлось ли в итоге.
//
// Несостоявшаяся сверка не согласна и не не согласна: она молчит.
// Отвечать за неё «нет» значило бы звать составителя разбирать спор,
// которого не было.
//
// Разбор расхождения учитывается ЗДЕСЬ, а не переписыванием вердикта:
// итог у задачи один, а записей о том, как к нему пришли, две, и обе
// нужны. Слепая сверка могла рассудить верно и отметить не тот вариант —
// такое расхождение снимается, и снимается оно разбором, а не тем, что
// кто-то переписал ответ сверки.
func (c Check) Agrees() bool {
	if !c.Done || c.Verdict == nil {
		return false
	}
	if c.Verdict.Agrees {
		return true
	}
	return c.Arbitration != nil && c.Arbitration.Clears()
}

// Disputed — расхождение есть и разбор его не снял.
//
// Отдельно от !Agrees(): несостоявшаяся сверка тоже не Agrees, но спора
// в ней нет. Разные новости — разные слова.
func (c Check) Disputed() bool {
	return c.Done && c.Verdict != nil && !c.Verdict.Agrees && !c.Agrees()
}

// Verdict — ответ слепой сверки.
type Verdict struct {
	Answer string `json:"answer"`
	Why    string `json:"why"`
	Sure   bool   `json:"sure"`

	// Agrees — сошлась ли сверка с заказанным ответом. Считаем это мы, а
	// не модель: модель не знает, что заказывали, и знать не должна.
	Agrees bool `json:"agrees"`
}

// RunNext берёт следующее задание и проводит его по узлам.
//
// Второй ответ — нашлось ли задание. Пустая очередь не отказ, и
// исполнитель, принявший её за отказ, начал бы её чинить.
func (r *Runner) RunNext(ctx context.Context) (Result, bool, error) {
	job, ok, err := r.jobs.Take(ctx, KindCase)
	if err != nil || !ok {
		return Result{}, false, err
	}
	result, err := r.run(ctx, job)
	if err != nil {
		// Отказ задания записывается всегда и с причиной: это
		// единственный след того, почему задача не написалась. Ошибку
		// самой записи прячем внутрь — наружу уезжает та, из-за которой
		// всё началось.
		if failErr := r.jobs.Fail(ctx, job.ID, err.Error()); failErr != nil {
			return Result{}, true, fmt.Errorf("%w (и отказ не записан: %v)", err, failErr)
		}
		return Result{}, true, err
	}
	if err := r.jobs.Done(ctx, job.ID); err != nil {
		return result, true, err
	}
	return result, true, nil
}

func (r *Runner) run(ctx context.Context, job Job) (Result, error) {
	// Круг вариантов собирается ОДИН раз на задание и читается всеми:
	// заданием написания, телом черновика и различающей сверкой. Собери
	// его каждый заново — и задача спрашивала бы одно, а сверка мерила
	// другое; расходились бы они молча, потому что обе части выглядели бы
	// исправными.
	//
	// И собирается он ДО обращения к модели: источник, из которого круга
	// не набирается, отказывает бесплатно.
	set, err := job.Plan.AnswerSet()
	if err != nil {
		return Result{}, err
	}

	if err := r.jobs.Step(ctx, job.ID, NodeCompose); err != nil {
		return Result{}, err
	}
	draft, err := r.compose(ctx, job, set)
	if err != nil {
		return Result{}, err
	}

	// Вычитка идёт ДО записи черновика и до обеих сверок. Поставь её
	// после — и сверено было бы одно, а показано обучающемуся другое:
	// вычитка правит условие, и мерить надо то, что уйдёт ему. Записывать
	// же черновик дважды (до и после) значит хранить текст, которого
	// никто не заказывал.
	if err := r.jobs.Step(ctx, job.ID, NodeProofread); err != nil {
		return Result{}, err
	}
	draft, proof := r.runProofread(ctx, job, draft)

	draftID, err := r.jobs.SaveDraft(ctx, job, draft)
	if err != nil {
		return Result{}, err
	}
	result := Result{JobID: job.ID, DraftID: draftID, Draft: draft, Proofread: proof}
	if err := r.jobs.SaveProofread(ctx, draftID, proof); err != nil {
		// Итог вычитки — пометка к задаче, а не сама задача: потерять её
		// обидно, уронить из-за неё готовую задачу глупо. В журнал, как и
		// у сверок.
		log.Printf("задание %d: итог вычитки не записан в черновик %d: %v", job.ID, draftID, err)
	}

	if err := r.jobs.Step(ctx, job.ID, NodeVerify); err != nil {
		return result, err
	}
	verdict, err := r.verify(ctx, job, draft)
	if err != nil {
		// Несостоявшаяся сверка задание не роняет: задача написана, и
		// выбрасывать её из-за того, что проверить не удалось, дороже,
		// чем показать составителю непроверенной. Но и «чистой» она не
		// называется — Done остаётся ложью, и это видно.
		//
		// Причина записывается в сам черновик, а не только в журнал:
		// журнал выкатки составитель не читает и читать не обязан, а
		// решать «почему у этой задачи нет сверки» приходится ему.
		log.Printf("задание %d: слепая сверка не состоялась: %v", job.ID, err)
		result.Check = Check{Note: err.Error()}
		r.keepCheck(ctx, job.ID, draftID, result.Check)
		return result, nil
	}
	result.Check = Check{Done: true, Verdict: verdict}
	result.Verdict = verdict
	r.runArbitrate(ctx, job, draft, &result.Check)
	r.keepCheck(ctx, job.ID, draftID, result.Check)

	r.runSiblings(ctx, job, draftID, draft, set, &result)
	r.runCueCheck(ctx, job, draftID, draft, &result)
	r.runJudge(ctx, job, draftID, draft, &result)
	return result, nil
}

// runArbitrate — второй, зрячий вопрос по расхождению.
//
// Зовётся ТОЛЬКО при расхождении: сошедшаяся сверка второго вопроса не
// требует и денег не стоит. Отказ узла черновик не роняет — разбор
// уточняет уже оплаченную проверку, — но и не молчит: причина ложится в
// сам черновик. Молчание составитель примет за «разобрали и подтвердили».
func (r *Runner) runArbitrate(ctx context.Context, job Job, draft Draft, check *Check) {
	if check.Verdict == nil || check.Verdict.Agrees {
		return
	}
	if err := r.jobs.Step(ctx, job.ID, NodeArbitrate); err != nil {
		// Отметка о шаге — то, что видит составитель в ходе работы, и
		// потерять её обидно; бросить из-за неё сам разбор глупо: он
		// уточняет уже оплаченную проверку, а не рисует полоску.
		log.Printf("задание %d: шаг разбора расхождения не записан: %v", job.ID, err)
	}
	out, err := r.arbitrate(ctx, job, draft)
	if err != nil {
		log.Printf("задание %d: расхождение не разобрано: %v", job.ID, err)
		check.ArbitrationNote = err.Error()
		return
	}
	check.Arbitration = out
}

// runJudge — судья: машинные проверки свода.
//
// Последним и без обращения к модели, как и детектор подсказок. Идёт он
// ПОСЛЕ обучения не случайно: замечание детектора может в эту самую
// минуту дорастить правило до кворума, и судья должен мерить свод таким,
// каким он стал. Обратный порядок дал бы задачу, написанную по одному
// своду, а сужденную по другому — и разошлись бы они ровно на том
// правиле, которое эта задача и подтвердила.
//
// Отказ узла задание не роняет: задача написана и сверена, а
// несостоявшийся суд — пометка, а не брак. Но и «чисто» он не значит:
// Done остаётся ложью, и это видно.
func (r *Runner) runJudge(ctx context.Context, job Job, draftID int64, draft Draft, result *Result) {
	if r.rules == nil {
		// Свода нет вовсе — судить нечем, и сказано это вслух. Молчание
		// составитель примет за «правила соблюдены».
		result.Rules = RuleCheckResult{Note: "свод правил не подключён"}
	} else if book, err := r.rules.Book(ctx); err != nil {
		result.Rules = RuleCheckResult{Note: "свод правил не прочитан: " + err.Error()}
	} else {
		result.Rules = Judge(book, job.Plan, draft)
	}
	if err := r.jobs.SaveRuleCheck(ctx, draftID, result.Rules); err != nil {
		log.Printf("задание %d: итог судьи не записан в черновик %d: %v",
			job.ID, draftID, err)
	}
}

// runCueCheck — детектор подсказок.
//
// Последним и без обращения к модели: он ничего не спрашивает, а сличает
// условие с положениями самого источника. Денег не стоит, а ловит то,
// чего не ловит ни одна сверка: условие, которое называет ответ прямо,
// обе сверки подтверждают охотнее всего — оно и правда ведёт к эталону,
// только учит при этом не тому.
//
// Отказ узла задание не роняет: задача написана и сверена, а ненайденные
// подсказки — пометка, а не брак.
func (r *Runner) runCueCheck(ctx context.Context, job Job, draftID int64, draft Draft, result *Result) {
	lex, err := r.jobs.SourceLexicon(ctx, job.Plan.SourceID)
	if err != nil {
		// Словаря нет — детектор отказывается СУДИТЬ, а не объявляет
		// чисто: молчание составитель примет за «подсказок нет».
		result.Cues = CueCheck{Note: "словарь источника не прочитан: " + err.Error()}
	} else {
		result.Cues = LintDraft(draft, job.Plan, lex)
	}
	if err := r.jobs.SaveCueCheck(ctx, draftID, result.Cues); err != nil {
		log.Printf("задание %d: итог детектора подсказок не записан в черновик %d: %v",
			job.ID, draftID, err)
	}

	// Обучение идёт ПОСЛЕ записи найденного, а не вместо неё: составитель
	// обязан увидеть замечание, даже если свод в эту минуту недоступен.
	r.learn(ctx, job, result.Cues)
}

// runSiblings — узел различающей сверки.
//
// Идёт ПОСЛЕ слепой и отдельным узлом, а не внутри неё: вопросы разные, и
// стоят они разных денег. Слепая спрашивает «ведёт ли условие к
// заказанному ответу», эта — «не ведёт ли оно с тем же успехом к соседу»;
// у задачи с двумя верными ответами первая сходится, и без второй никто
// не спросит.
//
// Отказ узла задание не роняет: задача написана и сверена, а не
// состоявшаяся различающая сверка — это пометка, а не брак.
func (r *Runner) runSiblings(ctx context.Context, job Job, draftID int64, draft Draft, set AnswerSet, result *Result) {
	if err := r.jobs.Step(ctx, job.ID, NodeSiblings); err != nil {
		log.Printf("задание %d: шаг различающей сверки не записан: %v", job.ID, err)
		return
	}
	checks := r.checkSiblings(ctx, job, draft, set)
	result.Siblings = checks
	if err := r.jobs.SaveSiblingChecks(ctx, draftID, checks); err != nil {
		log.Printf("задание %d: итоги различающей сверки не записаны в черновик %d: %v",
			job.ID, draftID, err)
	}
}

// keepCheck записывает итог сверки в черновик.
//
// Отказ записи задание не роняет и наружу не уезжает: задача написана и
// сверена, платить за неё второй раз из-за неудавшейся записи итога
// незачем. Но и молчать нельзя — черновик без итога при состоявшейся
// сверке выглядит как «сверки не было», а это другой случай и другие
// действия.
func (r *Runner) keepCheck(ctx context.Context, jobID, draftID int64, check Check) {
	if err := r.jobs.SaveCheck(ctx, draftID, check); err != nil {
		log.Printf("задание %d: итог сверки не записан в черновик %d: %v", jobID, draftID, err)
	}
}

// compose — узел написания.
//
// Круг приходит готовым: модель пишет прозу, а варианты в черновик
// ставит сборка (Composed.Draft). Выбрать вариант со стороны она больше
// не может — не потому, что мы это ловим, а потому, что её об этом не
// спрашивают.
func (r *Runner) compose(ctx context.Context, job Job, set AnswerSet) (Draft, error) {
	prompt, err := r.prompts.ForNode(ctx, NodeCompose)
	if err != nil {
		return Draft{}, err
	}
	plan := job.Plan

	block, err := r.ruleBlock(ctx, plan, NodeCompose)
	if err != nil {
		return Draft{}, err
	}

	// Свод дописывается ПОСЛЕ подстановки переменных, а не переменной в
	// самом задании. Переменную составитель может стереть, правя задание
	// в студии, — и свод перестал бы уходить молча, а задачи стали бы
	// хуже без единого следа в журнале.
	answer, err := r.ask(ctx, job, prompt, llm.Prompt{
		System:     RenderSet(prompt.SystemMd, plan, set) + block,
		User:       RenderSet(prompt.UserMd, plan, set),
		Schema:     ComposedSchema(plan),
		SchemaName: "case_draft",
	})
	if err != nil {
		return Draft{}, err
	}

	composed, err := ParseComposed(answer)
	if err != nil {
		return Draft{}, err
	}
	if err := composed.Validate(plan); err != nil {
		return Draft{}, err
	}
	return composed.Draft(set), nil
}

// ruleBlock — блок свода для узла, готовый к дописыванию в задание.
//
// Отказ чтения свода роняет задание, а не проходит пустотой. Пустой
// блок и непрочитанный свод — разные вещи ровно в том же смысле, в каком
// «подсказок нет» и «судить не могу»: задача, написанная без свода,
// выглядит обычной работой, и утрату замечают по тому, что ошибки,
// от которых свод заведён, вернулись все разом.
func (r *Runner) ruleBlock(ctx context.Context, plan Plan, node string) (string, error) {
	if r.rules == nil {
		return "", nil
	}
	book, err := r.rules.Book(ctx)
	if err != nil {
		return "", fmt.Errorf("свод правил не прочитан: %w", err)
	}
	block := book.Block(rules.Context{
		SourceID: plan.SourceID,
		UnitPath: plan.Unit.Path,
		TaskKind: plan.TaskKind,
		Node:     node,
	})
	if block == "" {
		return "", nil
	}
	return "\n\n" + block, nil
}

// verify — слепая сверка.
//
// # Что значит «слепая» у произвольного источника
//
// У донора сверка спрашивала диагноз по одному тексту условия: модель
// знает МКБ-10 и без нас, и назвать диагноз может свободно. У чужого
// приказа так нельзя — его меток модель не видела никогда, и «назовите
// пункт» вернуло бы выдумку.
//
// Поэтому сверке даётся ровно то, что видит обучающийся: условие и список
// вариантов. Слепой она остаётся в том, в чём это важно, — ей не
// говорится, какой вариант верен, не даются положения источника и не
// называется заказанная единица. Подскажи ей хоть чем-нибудь, и сверка
// выродится в самоподтверждение.
//
// Варианты перемешиваются: сверка, у которой верный всегда первый,
// проверяет порядок, а не задачу.
func (r *Runner) verify(ctx context.Context, job Job, draft Draft) (*Verdict, error) {
	prompt, err := r.prompts.ForNode(ctx, NodeVerify)
	if err != nil {
		return nil, err
	}

	options := make([]Option, len(draft.Options))
	for to, from := range r.shuffle(len(draft.Options)) {
		options[to] = draft.Options[from]
	}

	var list strings.Builder
	for i, o := range options {
		if i > 0 {
			list.WriteString("\n")
		}
		list.WriteString("- ")
		if o.Label != "" {
			list.WriteString(o.Label)
			list.WriteString(" — ")
		}
		list.WriteString(o.Text)
	}

	// Задание сверки собирается по ОСЛЕПЛЁННОМУ плану, а не по обычному.
	//
	// Иначе слепота держалась бы на дисциплине того, кто правит задание в
	// студии: вписал {положения} — и сверка стала самоподтверждением, а
	// выглядит она при этом ровно так же, как работала. Ослепление здесь
	// делает эту ошибку невыразимой: подставлять в задание сверки просто
	// нечего.
	blind := job.Plan.Blinded()
	system := Render(prompt.SystemMd, blind)
	user := Render(prompt.UserMd, blind)
	// Условие и варианты подставляются здесь, а не в Render: это не
	// свойства заказа, а то, что написала модель на прошлом узле, и
	// попади они в общий список переменных — оказались бы доступны и
	// узлу написания, то есть задание написания показывало бы модели
	// условие, которого ещё нет.
	user = strings.ReplaceAll(user, "{условие}", draft.Condition())
	user = strings.ReplaceAll(user, "{варианты}", list.String())

	answer, err := r.ask(ctx, job, prompt, llm.Prompt{
		System: system,
		User:   user,
		// Жар выборки ниже обычного: сверке нужна повторяемость — один и
		// тот же текст обязан давать один и тот же вердикт, иначе
		// спорить с ней нельзя.
		Temperature: 0.2,
		SchemaName:  "verdict",
	})
	if err != nil {
		return nil, err
	}

	var verdict Verdict
	text := strings.TrimSpace(answer)
	if fenced := unfence(text); fenced != "" {
		text = fenced
	}
	if err := json.Unmarshal([]byte(text), &verdict); err != nil {
		return nil, fmt.Errorf("ответ сверки не разобран: %w", err)
	}
	verdict.Agrees = agrees(job.Plan, draft, verdict.Answer)
	return &verdict, nil
}

// agrees — сошлась ли сверка с заказанным.
//
// Считаем это мы, а не модель: модель не знает, что заказывали, и знать
// не должна. Сравнение по метке у узнавания и по тексту у действия — то
// же различие, по которому мерится сам черновик.
func agrees(plan Plan, draft Draft, answer string) bool {
	answer = strings.TrimSpace(answer)
	if plan.TaskKind == KindAction {
		return strings.EqualFold(answer, strings.TrimSpace(draft.Answer))
	}
	if strings.EqualFold(answer, plan.Unit.Label) {
		return true
	}
	// Сверка могла назвать вариант его текстом, а не меткой: задание
	// просит метку, но требовать от неё формы ответа сильнее, чем от
	// обучающегося, незачем.
	for _, o := range draft.Options {
		if strings.EqualFold(strings.TrimSpace(o.Text), answer) {
			return strings.EqualFold(strings.TrimSpace(o.Label), plan.Unit.Label)
		}
	}
	return false
}

// ask — одно обращение к модели с записью в учёт.
//
// Узел приходит СВОИМ ЗАДАНИЕМ, а не одним именем, и это не удобство
// записи. Имя узла и модель узла — два свойства одной строки базы, и
// передай мы их порознь, ничто не помешало бы спросить моделью одного
// узла, записав в учёт имя другого: расход по узлам — то, по чему
// решают, где менять модель, и разойдись он с правдой, менять стали бы
// не там. Здесь спутать их попросту нечем.
func (r *Runner) ask(ctx context.Context, job Job, node Prompt, prompt llm.Prompt) (string, error) {
	model := modelFor(node, job.Plan.Model)
	started := time.Now()
	answer, usage, err := r.talker.Generate(ctx, prompt, model)
	// В учёт уходит ТА модель, которой спросили, а не та, которую
	// заказали: расход считается по моделям, и записанная не та превращает
	// счёт поставщика в загадку — сумма сходится, а по строкам не сходится
	// ничего.
	record(ctx, r.ledger, r.prices, job.ID, node.Node, model,
		prompt, answer, usage, time.Since(started), err)
	if err != nil {
		if errors.Is(err, llm.ErrTruncated) {
			// Обрыв лечится потолком ответа, а не повтором: сказать об
			// этом прямо полезнее, чем показать «ошибку разбора».
			return "", fmt.Errorf("узел «%s»: модель не уложилась в потолок ответа", NodeWord(node.Node))
		}
		return "", fmt.Errorf("узел «%s»: %w", NodeWord(node.Node), err)
	}
	return answer, nil
}

// modelFor — какой моделью спрашивать этот узел.
//
// Порядок старшинства: названная в заказе, потом модель узла, потом
// модель поставщика. Заказ старше узла потому, что называют его руками и
// на один раз: составитель, выбравший модель этой задаче, просит сравнить
// — и настройка конвейера, молча переспорившая его выбор, сделала бы
// сравнение невозможным, оставаясь на вид работающей.
//
// Модель поставщика последняя и именем сюда не переносится: записанное у
// нас имя устареет молча, когда поставщик сменит своё умолчание.
func modelFor(node Prompt, ordered string) string {
	if ordered = strings.TrimSpace(ordered); ordered != "" {
		return ordered
	}
	return strings.TrimSpace(node.Model)
}

// record — одна запись в учёт, общая на все узлы конвейера.
//
// Общая потому, что узлов становится много, а правило у всех одно и оно не
// про узел: обращение состоялось — строка расхода обязана быть, чем бы
// обращение ни кончилось. Вторая такая запись рядом с первой разошлась бы
// с ней молча — и расхождение читалось бы как «этот узел бесплатный».
//
// Пустой учёт — это проверка без базы, и она законна: на бою пусто не
// бывает, потому что обращение, нигде не записанное, невозможно ни
// разобрать, ни посчитать.
func record(ctx context.Context, ledger *llmusage.Store, prices llm.Prices,
	jobID int64, node, model string, prompt llm.Prompt, answer string,
	usage llm.Usage, took time.Duration, err error) {

	if ledger == nil {
		return
	}
	// Ошибку записи в учёт наверх не несём: работа сделана, и ронять её
	// из-за того, что не записалась строка расхода, значит платить дважды.
	// Но и молчать нельзя — запись об этом остаётся в журнале службы.
	recordErr := ledger.Record(ctx, llmusage.Call{
		JobID: jobID, Node: node, Model: model,
		Usage: usage, Latency: took, Err: err,
		Request: prompt.System + "\n\n" + prompt.User,
		Answer:  answer,
	}, prices)
	if recordErr != nil {
		logAccountFailure(jobID, node, recordErr)
	}
}

// logAccountFailure — отдельной функцией, чтобы журнал службы не
// перемешивался с работой конвейера и его легко было найти по имени.
func logAccountFailure(jobID int64, node string, err error) {
	log.Printf("задание %d, узел %s: расход не записан: %v", jobID, node, err)
}
