package gen

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Задания модели: узлы конвейера и их промпты.
//
// Формулировка задания — предмет врачебной работы, а не деталь
// реализации: от неё зависит, получится ли достоверное условие. Поэтому
// промпты живут в базе и правятся в студии, а в коде лежат только
// затравки — то, с чего начинают, пока их не правили.

// Узлы конвейера. Словарь закрыт: узел, появившийся строкой по месту, не
// получит ни промпта, ни модели — то есть пойдёт умолчанием, и заметят
// это по счёту, а не по работе.
const (
	// NodeCompose — написание условия, вариантов, разметки и разбора.
	NodeCompose = "compose"

	// NodeVerify — слепая сверка: сходится ли условие с заказанным
	// ответом, если смотреть на него глазами обучающегося.
	NodeVerify = "verify"
)

// Nodes — узлы по порядку прохождения.
var Nodes = []string{NodeCompose, NodeVerify}

// NodeWord — узел по-русски. Читает это составитель, и «compose» ему
// ничего не говорит.
func NodeWord(node string) string {
	switch node {
	case NodeCompose:
		return "написание"
	case NodeVerify:
		return "слепая сверка"
	default:
		return node
	}
}

// Prompt — задание модели.
type Prompt struct {
	ID       string
	Name     string
	Node     string
	SystemMd string
	UserMd   string
	Revision int
}

// Prompts — хранилище заданий.
type Prompts struct {
	gate *dbgate.Gate
}

func NewPrompts(gate *dbgate.Gate) *Prompts { return &Prompts{gate: gate} }

// ForNode отдаёт задание узла: то, что отмечено умолчанием.
//
// Умолчание ровно одно на узел — двух конвейер не разберёт, и держит это
// указатель базы. Нет ни одного — отказ, а не пустое задание: модель,
// получившая пустое задание, ответит чем угодно, и ответ этот будет
// выглядеть работой.
func (p *Prompts) ForNode(ctx context.Context, node string) (Prompt, error) {
	var out Prompt
	err := p.gate.QueryRow(ctx,
		`SELECT id, name, node, system_md, user_md, revision
		   FROM prompts
		  WHERE node = $1 AND is_default
		  LIMIT 1`, node).
		Scan(&out.ID, &out.Name, &out.Node, &out.SystemMd, &out.UserMd, &out.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return Prompt{}, fmt.Errorf("у узла «%s» нет задания по умолчанию", NodeWord(node))
	}
	if err != nil {
		return Prompt{}, fmt.Errorf("задание узла «%s» не прочитано: %w", NodeWord(node), err)
	}
	return out, nil
}

// Seed кладёт затравки тех узлов, у которых заданий ещё нет.
//
// Только недостающие: правленое в студии задание затирать накатом нельзя —
// правка задания это врачебная работа, и потерять её значит потерять
// неделю чьей-то настройки. Поэтому ON CONFLICT DO NOTHING, а не UPSERT.
func (p *Prompts) Seed(ctx context.Context) error {
	return p.gate.InTx(ctx, func(tx pgx.Tx) error {
		for _, seed := range seeds() {
			_, err := tx.Exec(ctx,
				`INSERT INTO prompts (id, name, node, system_md, user_md, is_default)
				 VALUES ($1, $2, $3, $4, $5, TRUE)
				 ON CONFLICT (id) DO NOTHING`,
				seed.ID, seed.Name, seed.Node, seed.SystemMd, seed.UserMd)
			if err != nil {
				return fmt.Errorf("затравка %q не положена: %w", seed.ID, err)
			}
		}
		return nil
	})
}

// Render подставляет в задание переменные заказа.
//
// Переменные названы по-русски и в фигурных скобках: задание правит
// составитель, а не программист, и {единица} он прочтёт, а {{.UnitWord}}
// — нет. Незнакомая переменная остаётся в тексте как есть: молча
// вычищенная, она превратила бы опечатку в задании в тихую потерю смысла.
func Render(text string, plan Plan) string {
	vars := map[string]string{
		"источник":    plan.Title,
		"единица":     plan.UnitWord,
		"положение":   plan.StatementWord,
		"метка":       plan.Unit.Label,
		"название":    plan.Unit.Title,
		"положения":   plan.StatementsMd,
		"вложенность": hierarchyWord(plan.Hierarchy),
		"круг":        siblingsList(plan),
		"обозначения": strings.Join(plan.Designations(), ", "),
		"вид":         KindWord(plan.TaskKind),
	}
	if plan.Target != nil {
		vars["эталон"] = plan.Target.Body
	} else {
		vars["эталон"] = plan.Unit.Title
	}

	out := text
	for name, value := range vars {
		out = strings.ReplaceAll(out, "{"+name+"}", value)
	}
	return out
}

// hierarchyWord — смысл вложенности словами.
//
// Модели это говорится прямо: у «часть целого» сосед по родителю — другая
// часть того же целого, у «разновидности» — другая разновидность того же
// рода. Подразумевать одно устройство для всех источников значит писать
// неверные варианты наугад.
func hierarchyWord(hierarchy string) string {
	switch hierarchy {
	case "is-a":
		return "вложенные — разновидности вышестоящего"
	case "part-of":
		return "вложенные — части вышестоящего"
	default:
		return "вложенные просто сгруппированы под вышестоящим"
	}
}

// siblingsList — круг различения строками «метка — название».
func siblingsList(plan Plan) string {
	if len(plan.Siblings) == 0 {
		return "(соседей у этой единицы нет)"
	}
	var b strings.Builder
	for i, s := range plan.Siblings {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- ")
		b.WriteString(s.Label)
		if s.Title != "" {
			b.WriteString(" — ")
			b.WriteString(s.Title)
		}
	}
	return b.String()
}

// seeds — затравки заданий.
//
// Написаны так, будто их читает врач: тексты интерфейса и заданий в этом
// проекте пишутся по-русски и без жаргона. Модель при этом получает их
// как есть — она читает то же, что читает составитель, и спор о задаче
// разрешается тем, ЧТО ЧИТАЛА МОДЕЛЬ.
func seeds() []Prompt {
	return []Prompt{{
		ID:   "compose-default",
		Name: "Написание условия — затравка",
		Node: NodeCompose,
		SystemMd: `Вы пишете учебную ситуационную задачу по источнику «{источник}».

В этом источнике {вложенность}. Единица источника зовётся «{единица}», ` +
			`положение — «{положение}».

Условие пишется так, чтобы по нему можно было узнать заказанную единицу,
опираясь только на положения источника. Ничего сверх положений в условие
не добавляйте: задача поверяется этими же положениями, и добавленное вами
проверить будет нечем.

В условии не должно быть ни метки единицы, ни её названия, ни цитат из
положений дословно: это подсказки, и задача с ними проверяет чтение, а не
рассуждение.

Условие разбивайте на фрагменты. Каждый фрагмент, который подтверждает
положение, пометьте его обозначением. Фрагменты фона оставляйте без
пометок — без фона условие не читается.

Ответ — JSON без пояснений вокруг.`,
		UserMd: `Вид задачи: {вид}.

Заказанная единица: {метка} — {название}.

Положения этой единицы:
{положения}

Ссылаться в разметке можно только на эти обозначения: {обозначения}.

Круг различения (из него берите неверные варианты, других не придумывайте):
{круг}`,
	}, {
		ID:   "verify-default",
		Name: "Слепая сверка — затравка",
		Node: NodeVerify,
		SystemMd: `Вам дают условие учебной задачи и список вариантов ответа.
Выберите тот вариант, который условие подтверждает лучше остальных, и
объясните, чем именно.

Вы не знаете, какой вариант считается верным, и знать не должны: в этом
смысл сверки. Если условие не позволяет выбрать один вариант — так и
скажите, назвав, чего в нём не хватает.

Ответ — JSON: {"answer": "метка или текст варианта", "why": "чем подтверждается",
"sure": true или false}.`,
		UserMd: `Условие:
{условие}

Варианты:
{варианты}`,
	}}
}
