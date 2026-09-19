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

	// shuffle — перемешивание вариантов перед слепой сверкой. Полем ради
	// проверок: сверка, у которой верный вариант всегда первый, проверяет
	// не то, что надо.
	shuffle func(n int) []int
}

// NewRunner собирает конвейер.
func NewRunner(jobs *Jobs, prompts *Prompts, talker Talker) *Runner {
	return &Runner{jobs: jobs, prompts: prompts, talker: talker, shuffle: rand.Perm}
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
	job, ok, err := r.jobs.Take(ctx)
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
	if err := r.jobs.Step(ctx, job.ID, NodeCompose); err != nil {
		return Result{}, err
	}
	draft, err := r.compose(ctx, job)
	if err != nil {
		return Result{}, err
	}
	draftID, err := r.jobs.SaveDraft(ctx, job, draft)
	if err != nil {
		return Result{}, err
	}
	result := Result{JobID: job.ID, DraftID: draftID, Draft: draft}

	if err := r.jobs.Step(ctx, job.ID, NodeVerify); err != nil {
		return result, err
	}
	verdict, err := r.verify(ctx, job, draft)
	if err != nil {
		// Несостоявшаяся сверка задание не роняет: задача написана, и
		// выбрасывать её из-за того, что проверить не удалось, дороже,
		// чем показать составителю непроверенной. Но и «чистой» она не
		// называется — Verdict остаётся пустым, и это видно.
		return result, nil
	}
	result.Verdict = verdict
	return result, nil
}

// compose — узел написания.
func (r *Runner) compose(ctx context.Context, job Job) (Draft, error) {
	prompt, err := r.prompts.ForNode(ctx, NodeCompose)
	if err != nil {
		return Draft{}, err
	}
	plan := job.Plan

	answer, err := r.ask(ctx, job, NodeCompose, llm.Prompt{
		System:     Render(prompt.SystemMd, plan),
		User:       Render(prompt.UserMd, plan),
		Schema:     DraftSchema(plan),
		SchemaName: "case_draft",
	})
	if err != nil {
		return Draft{}, err
	}

	draft, err := ParseDraft(answer)
	if err != nil {
		return Draft{}, err
	}
	if err := draft.Validate(plan); err != nil {
		return Draft{}, err
	}
	return draft, nil
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

	answer, err := r.ask(ctx, job, NodeVerify, llm.Prompt{
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
func (r *Runner) ask(ctx context.Context, job Job, node string, prompt llm.Prompt) (string, error) {
	started := time.Now()
	answer, usage, err := r.talker.Generate(ctx, prompt, job.Plan.Model)
	r.account(ctx, job, node, prompt, answer, usage, time.Since(started), err)
	if err != nil {
		if errors.Is(err, llm.ErrTruncated) {
			// Обрыв лечится потолком ответа, а не повтором: сказать об
			// этом прямо полезнее, чем показать «ошибку разбора».
			return "", fmt.Errorf("узел «%s»: модель не уложилась в потолок ответа", NodeWord(node))
		}
		return "", fmt.Errorf("узел «%s»: %w", NodeWord(node), err)
	}
	return answer, nil
}

func (r *Runner) account(ctx context.Context, job Job, node string, prompt llm.Prompt,
	answer string, usage llm.Usage, took time.Duration, err error) {

	if r.ledger == nil {
		return
	}
	// Ошибку записи в учёт наверх не несём: задача написана, и ронять её
	// из-за того, что не записалась строка расхода, значит платить дважды.
	// Но и молчать нельзя — запись об этом остаётся в журнале службы.
	recordErr := r.ledger.Record(ctx, llmusage.Call{
		JobID: job.ID, Node: node, Model: job.Plan.Model,
		Usage: usage, Latency: took, Err: err,
		Request: prompt.System + "\n\n" + prompt.User,
		Answer:  answer,
	}, r.prices)
	if recordErr != nil {
		logAccountFailure(job.ID, node, recordErr)
	}
}

// logAccountFailure — отдельной функцией, чтобы журнал службы не
// перемешивался с работой конвейера и его легко было найти по имени.
func logAccountFailure(jobID int64, node string, err error) {
	log.Printf("задание %d, узел %s: расход не записан: %v", jobID, node, err)
}
