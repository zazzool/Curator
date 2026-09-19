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

// All отдаёт все задания по порядку узлов.
//
// Порядок — прохождения конвейера, а не алфавита: составитель читает
// список сверху вниз как путь задачи, и «слепая сверка» выше «написания»
// сбила бы его с того, что за чем идёт.
func (p *Prompts) All(ctx context.Context) ([]Prompt, error) {
	rows, err := p.gate.Query(ctx,
		`SELECT id, name, node, system_md, user_md, revision
		   FROM prompts
		  ORDER BY node, id`)
	if err != nil {
		return nil, fmt.Errorf("задания не прочитаны: %w", err)
	}
	defer rows.Close()

	byNode := map[string][]Prompt{}
	for rows.Next() {
		var one Prompt
		if err := rows.Scan(&one.ID, &one.Name, &one.Node, &one.SystemMd,
			&one.UserMd, &one.Revision); err != nil {
			return nil, fmt.Errorf("задание не прочитано: %w", err)
		}
		byNode[one.Node] = append(byNode[one.Node], one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("задания не дочитаны: %w", err)
	}

	// Пустой список — [], а не nil: студия ходит по нему циклом, и на
	// свежей установке, где заданий ещё нет, nil уехал бы наружу как null.
	out := make([]Prompt, 0, len(byNode))
	for _, node := range Nodes {
		out = append(out, byNode[node]...)
		delete(byNode, node)
	}
	// Задание неизвестного узла не прячется: оно уже лежит в базе, и
	// спрятанное от составителя, оно продолжит работать незамеченным.
	for _, left := range byNode {
		out = append(out, left...)
	}
	return out, nil
}

// Save правит задание, сверяя редакцию.
//
// Редакция сверяется условием самого UPDATE, а не чтением перед записью:
// между чтением и записью успевает вклиниться второй составитель, и
// проверка «сперва прочитали — совпало» пропустит ровно тот случай, ради
// которого заведена. Прежний текст уезжает в историю в той же транзакции:
// сравнить «до» и «после» нужно тогда, когда качество задач поехало, а
// помнить, что меняли неделю назад, уже некому.
func (p *Prompts) Save(ctx context.Context, edit Prompt, login string) (Prompt, error) {
	if strings.TrimSpace(edit.SystemMd) == "" && strings.TrimSpace(edit.UserMd) == "" {
		return Prompt{}, errors.New("задание пустым не сохраняется: модель на пустое задание ответит чем угодно")
	}

	var out Prompt
	err := p.gate.InTx(ctx, func(tx pgx.Tx) error {
		var prevSystem, prevUser string
		var prevRevision int
		err := tx.QueryRow(ctx,
			`SELECT system_md, user_md, revision FROM prompts WHERE id = $1 FOR UPDATE`,
			edit.ID).Scan(&prevSystem, &prevUser, &prevRevision)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("задания %q нет", edit.ID)
		}
		if err != nil {
			return fmt.Errorf("задание %q не прочитано: %w", edit.ID, err)
		}
		if edit.Revision != prevRevision {
			return fmt.Errorf(
				"задание правили, пока вы его открывали: у вас редакция %d, в студии уже %d — перечитайте и внесите правку заново",
				edit.Revision, prevRevision)
		}

		// В историю уходит ПРЕЖНИЙ текст под прежним номером: так номер
		// редакции в истории означает «что было в этой редакции», а не
		// «что её сменило».
		if _, err := tx.Exec(ctx,
			`INSERT INTO prompt_revisions (prompt_id, revision, system_md, user_md, saved_by)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (prompt_id, revision) DO NOTHING`,
			edit.ID, prevRevision, prevSystem, prevUser, login); err != nil {
			return fmt.Errorf("прежняя редакция задания %q не сохранена: %w", edit.ID, err)
		}

		name := strings.TrimSpace(edit.Name)
		err = tx.QueryRow(ctx,
			`UPDATE prompts
			    SET name = COALESCE(NULLIF($2, ''), name),
			        system_md = $3, user_md = $4,
			        revision = revision + 1, updated_at = NOW()
			  WHERE id = $1 AND revision = $5
			 RETURNING id, name, node, system_md, user_md, revision`,
			edit.ID, name, edit.SystemMd, edit.UserMd, prevRevision).
			Scan(&out.ID, &out.Name, &out.Node, &out.SystemMd, &out.UserMd, &out.Revision)
		if err != nil {
			return fmt.Errorf("задание %q не сохранено: %w", edit.ID, err)
		}
		return nil
	})
	if err != nil {
		return Prompt{}, err
	}
	return out, nil
}
