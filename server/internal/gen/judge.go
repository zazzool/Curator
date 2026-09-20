package gen

import (
	"fmt"
	"sort"
	"strings"

	"curator/server/internal/rules"
)

// Судья: прогон машинных проверок свода по готовому черновику.
//
// # Почему отдельным узлом, а не внутри разбора черновика
//
// Разбор мерит черновик ПЛАНОМ ЗАКАЗА — тем, что известно заранее и
// одинаково для всех. Судья мерит его СВОДОМ, то есть тем, что менялось
// вчера и поменяется завтра, и меняет это составитель. Слей их — и
// правка свода роняла бы задание вместо того, чтобы поставить замечание:
// правило, написанное сегодня, начало бы отбивать написанное по нему же
// час назад.
//
// Поэтому находки судьи — ПОМЕТКА, а не брак. Что с ними делать, решает
// составитель, глядя на задачу: свод растёт из живой работы, и правило,
// добравшееся до кворума, ещё не значит, что каждое его срабатывание
// верно.
//
// # Отчего он не зовёт модель
//
// Не зовёт вовсе, как и детектор подсказок. Предикат сличает текст с
// закрытым списком слов и чисел; ни одного вопроса наружу, ни копейки
// расхода. Тем и ценен: правило, доведённое до предиката, перестаёт
// зависеть от сговорчивости модели.

// RuleFinding — одна находка судьи: чьё правило и что оно нашло.
type RuleFinding struct {
	// RuleID и Title — чьё правило сработало. Названием, а не одним
	// опознавателем: «builtin:one-person» составителю ничего не говорит,
	// а «Один человек на всё условие» говорит.
	RuleID string `json:"ruleId"`
	Title  string `json:"title"`

	// Kind — род правила. По нему составитель решает, чинить сейчас или
	// потом: существо дела и слог правятся по-разному.
	Kind string `json:"kind"`

	// Where — где нашлось: «segments[2]», «title», «explanationMd».
	Where   string `json:"where"`
	Message string `json:"message"`
}

// RuleCheckResult — итог судьи, тремя состояниями.
//
// Те же три, что у всех прочих узлов, и по той же причине: поле
// отсутствует — судья не ходил вовсе; Done: false с причиной — ходил и
// судить не смог; Done: true — прошёл. Слей мы «не ходил» и «ничего не
// нашёл», и непроверенная задача читалась бы как чистая.
type RuleCheckResult struct {
	Done bool   `json:"done"`
	Note string `json:"note,omitempty"`

	// Checked — сколько правил с проверками прогнано. Числом и вслух:
	// «нарушений нет» при нуле прогнанных правил и при двадцати — разные
	// сведения, а выглядят одинаково.
	Checked int `json:"checked"`

	// Findings — найденное. Пустой список — прогнали и чисто.
	Findings []RuleFinding `json:"findings"`

	// Remark — что сказать составителю, его словами. Считается ПРИ
	// ЧТЕНИИ и не хранится: перепиши мы формулировку, и записанные прежде
	// черновики остались бы со старой.
	Remark string `json:"remark,omitempty"`
}

// Clean — судья прошёл и ничего не нашёл.
func (c RuleCheckResult) Clean() bool { return c.Done && len(c.Findings) == 0 }

// Remarks — замечание составителю.
//
// Пустая строка — сказать нечего. Несостоявшийся суд говорит о себе
// ВСЛУХ: молчание составитель примет за «правила соблюдены» и отпустит
// задачу непроверенной.
func (c RuleCheckResult) Remarks() string {
	if !c.Done {
		if c.Note == "" {
			return "Проверки свода не прогонялись."
		}
		return "Проверки свода не прогонялись: " + c.Note
	}
	if len(c.Findings) == 0 {
		if c.Checked == 0 {
			// Ноль прогнанных правил — не чистота, а её отсутствие.
			return "Ни одно правило свода машинной проверки не несёт."
		}
		return ""
	}
	names := make([]string, 0, len(c.Findings))
	seen := map[string]bool{}
	for _, f := range c.Findings {
		if seen[f.RuleID] {
			continue
		}
		seen[f.RuleID] = true
		names = append(names, fmt.Sprintf("«%s»", f.Title))
	}
	return fmt.Sprintf("Нарушены правила свода: %s.", strings.Join(names, ", "))
}

// Judge прогоняет проверки правил, действующих для этого заказа.
//
// Правила берутся тем же отбором, каким они уходят в задание написания
// (Book.Applicable): проверяется то же, чего требовали. Разойдись отборы
// — и задача получала бы замечание по правилу, которого ей не ставили.
//
// Узел в контексте отбора назван написанием, и это не описка: правило
// действует НА НАПИСАНИЕ, а судья лишь смотрит, вышло ли. Назови мы здесь
// свой узел — область «только для написания» перестала бы проверяться, и
// заметили бы это по тому, что проверки молчат.
func Judge(book rules.Book, plan Plan, draft Draft) RuleCheckResult {
	out := RuleCheckResult{Done: true, Findings: []RuleFinding{}}
	for _, rule := range book.Applicable(rules.Context{
		SourceID: plan.SourceID,
		UnitPath: plan.Unit.Path,
		TaskKind: plan.TaskKind,
		Node:     NodeCompose,
	}) {
		if rule.Check == nil {
			continue
		}
		out.Checked++
		for _, finding := range RunCheck(draft, rule.Check) {
			out.Findings = append(out.Findings, RuleFinding{
				RuleID:  rule.ID,
				Title:   rule.Title,
				Kind:    string(rule.Kind),
				Where:   finding.Where,
				Message: finding.Message,
			})
		}
	}
	// Порядок находок — по месту в задаче, а не по порядку правил:
	// составитель читает их, идя по условию сверху вниз, и прыгающий по
	// фрагментам список заставляет искать место каждой заново.
	sort.SliceStable(out.Findings, func(i, j int) bool {
		return out.Findings[i].Where < out.Findings[j].Where
	})
	return out
}
