package gen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"curator/server/internal/llm"
)

// Различающая сверка: не подтверждает ли условие ещё и неверный вариант.
//
// # Зачем она, если слепая сверка уже есть
//
// Слепая сверка отвечает на другой вопрос — «ведёт ли условие к
// заказанному ответу». Она сошлась, и это ничего не говорит о соседе:
// условие «заявление подано, срок считается рабочими днями» одинаково
// подтверждает и 3.1, и 3.2, а слепая сверка выбрала из них заказанный и
// объявила согласие. Задача с двумя верными ответами проходит её
// насквозь — и находит её потом врач, решая задачу правильно и получая
// «неверно».
//
// Поэтому вопрос задаётся отдельно и по каждому неверному варианту:
// подтверждает ли то же самое условие положения ЭТОЙ единицы.
//
// # Слепота та же
//
// Модель видит условие и положения одной единицы, и ничего сверх: ни
// метки, ни названия, ни того, что вариант считается неверным. Скажи ей
// «проверьте, что сюда не подходит» — и получим «не подходит» на всё
// подряд, то есть самоподтверждение наоборот.

// SiblingCheck — итог различающей сверки по одному неверному варианту.
type SiblingCheck struct {
	// Label и Title — чей это вариант. Названием, а не одной меткой:
	// «3.2» составителю ничего не говорит, а «3.2 (Отказ)» говорит.
	Label string `json:"label"`
	Title string `json:"title,omitempty"`

	// Check — состоялась ли сверка и что она сказала. Тот же тип, что у
	// слепой сверки, и это не экономия: вопросы «сверяли ли» и «что
	// вышло» здесь ровно те же, и второй тип для них разошёлся бы с
	// первым молча.
	Check Check `json:"check"`

	// Remark — что сказать про этот вариант составителю, его словами.
	//
	// Заполняется ПРИ ЧТЕНИИ и не хранится (см. Jobs.SaveSiblingChecks):
	// перепиши мы формулировку, и записанные прежде черновики остались бы
	// со старой, а два места для одних слов расходятся молча. Считает их
	// сервер, а не студия, по тому же правилу, что и всякий другой текст
	// отказа: разойдись они, составитель читал бы про один порок задачи,
	// а сверка нашла бы другой.
	Remark string `json:"remark,omitempty"`

	// Confirms — условие подтверждает и этот вариант, то есть у задачи
	// выходит второй верный ответ.
	//
	// Считается здесь, а не берётся у модели: модель отвечает про
	// положения, а «второй верный ответ» — это вывод про задачу, и
	// делает его тот, кто знает, что вариант неверный. Модель не знает.
	Confirms bool `json:"confirms"`
}

// Note — замечание составителю по итогу одного варианта.
//
// Пустая строка — сказать нечего: условие соседа не подтверждает, то есть
// вариант работает как задумано.
//
// Несверенный вариант говорит о себе ВСЛУХ, и это то же правило, что у
// несостоявшейся слепой сверки: молчание составитель примет за «соседи
// чисты» и отпустит задачу к врачу непроверенной.
func (s SiblingCheck) Note() string {
	name := s.Label
	if title := strings.TrimSpace(s.Title); title != "" {
		name = fmt.Sprintf("%s (%s)", s.Label, title)
	}
	switch {
	case !s.Check.Done:
		note := s.Check.Note
		if note == "" {
			note = "причина не записана"
		}
		return fmt.Sprintf("вариант «%s» не сверен: %s", name, note)
	case s.Confirms:
		why := ""
		if s.Check.Verdict != nil {
			why = strings.TrimSpace(s.Check.Verdict.Why)
		}
		out := fmt.Sprintf("условие подтверждает и «%s» — у задачи выходит второй верный ответ", name)
		if why != "" {
			out += ": " + why
		}
		return out
	default:
		return ""
	}
}

// siblingVerdict — ответ модели по одному варианту.
type siblingVerdict struct {
	Verdict string `json:"verdict"`
	Why     string `json:"why"`
}

// siblingSchema — закрытый словарь ответа.
//
// Закрытый намеренно: «скорее подтверждает» разобрать нечем, а
// придуманное моделью слово пришлось бы толковать на глаз — то есть
// решать за неё.
func siblingSchema() json.RawMessage {
	raw, err := json.Marshal(map[string]any{
		"type":     "object",
		"required": []string{"verdict", "why"},
		"properties": map[string]any{
			"verdict": map[string]any{
				"type": "string",
				"enum": []string{"confirms", "contradicts", "insufficient"},
			},
			"why": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	})
	if err != nil {
		// Схема собрана из литералов и в JSON не превращается только при
		// поломке самой библиотеки: молчать об этом нельзя, а вернуть
		// половину схемы значит снять словарь, не сказав об этом.
		panic("схема различающей сверки не собралась: " + err.Error())
	}
	return raw
}

// checkSiblings прогоняет условие против положений каждого неверного
// варианта.
//
// Сверяются неверные варианты круга — ровно те, что уйдут обучающемуся:
// круг собрал сервер, и в черновике лежит он же. Прежде варианты брались
// из ответа модели и сличались с планом, потому что модель могла назвать
// единицу со стороны; теперь назвать её некому, и сличать нечего.
//
// Задача-действие сверяется теперь ТОЖЕ, и это не мелочь: прежде её
// варианты были текстами без единицы за ними, и различающая сверка
// пропускала такую задачу целиком — молча и полностью. Вариантом там
// стало положение документа, и мерится он этим самым положением.
//
// Вариант, у которого положений не нашлось, называется несверенным, а не
// молчится — см. Note.
//
// Отказ по одному варианту не роняет остальных и не роняет задание: за
// задачу уже заплачено, и выбрасывать её из-за того, что одного соседа не
// удалось спросить, дороже, чем показать составителю с честной пометкой.
func (r *Runner) checkSiblings(ctx context.Context, job Job, draft Draft, set AnswerSet) []SiblingCheck {
	rivals := set.Refs()
	if len(rivals) == 0 {
		// Пустой список — [], а не nil: он уедет в ответ ручки, и null
		// вместо списка роняет студию на исправном случае — на задаче,
		// у которой сверять нечего.
		return []SiblingCheck{}
	}

	prompt, err := r.prompts.ForNode(ctx, NodeSiblings)
	if err != nil {
		// Задания нет — не сверен ни один, и сказано это про каждый:
		// один общий отказ пришлось бы разворачивать глазами обратно в
		// список вариантов.
		out := make([]SiblingCheck, 0, len(rivals))
		for _, rival := range rivals {
			out = append(out, SiblingCheck{
				Label: rival.Label, Title: rival.Title,
				Check: Check{Note: "задание различающей сверки не прочитано"},
			})
		}
		return out
	}

	blind := job.Plan.Blinded()
	system := Render(prompt.SystemMd, blind)
	text := draft.Condition()

	out := make([]SiblingCheck, 0, len(rivals))
	for _, rival := range rivals {
		one := SiblingCheck{Label: rival.Label, Title: rival.Title}
		statements := strings.TrimSpace(rival.StatementsMd)
		switch {
		case strings.TrimSpace(text) == "":
			one.Check = Check{Note: "условие пусто — сверять нечего"}
		case statements == "":
			one.Check = Check{Note: "у единицы нет положений — сверить не с чем"}
		default:
			one.Check = r.askSibling(ctx, job, prompt, system, blind, text, statements)
		}
		// Второй верный ответ — это ПОДТВЕРЖДЕНИЕ соседа, и только оно.
		// «Недостаточно сказано» у неверного варианта — замысел задачи на
		// различение, а не порок: условие и не обязано его подтверждать.
		one.Confirms = one.Check.Done && one.Check.Verdict != nil &&
			one.Check.Verdict.Answer == verdictConfirms
		out = append(out, one)
	}
	return out
}

// Значения закрытого словаря ответа различающей сверки.
const (
	verdictConfirms     = "confirms"
	verdictContradicts  = "contradicts"
	verdictInsufficient = "insufficient"
)

func (r *Runner) askSibling(
	ctx context.Context, job Job, node Prompt, system string, blind Plan, text, statements string,
) Check {
	user := Render(node.UserMd, blind)
	// Условие и сверяемое подставляются здесь, а не в Render, по той же
	// причине, что у слепой сверки: это не свойства заказа, а то, что
	// написала модель на прошлом узле. Попади они в общий список
	// переменных — стали бы доступны и узлу написания, то есть задание
	// написания показывало бы модели условие, которого ещё нет.
	user = strings.ReplaceAll(user, "{условие}", text)
	user = strings.ReplaceAll(user, "{сверяемое}", statements)
	user = strings.ReplaceAll(user, "{Положение}", titleWord(blind.StatementWord))

	answer, err := r.ask(ctx, job, node, llm.Prompt{
		System: system,
		User:   user,
		// Жар тот же низкий, что у слепой сверки, и по той же причине:
		// один и тот же текст обязан давать один и тот же вердикт, иначе
		// спорить с ним нельзя.
		Temperature: 0.2,
		Schema:      siblingSchema(),
		SchemaName:  "sibling_verdict",
	})
	if err != nil {
		return Check{Note: err.Error()}
	}

	raw := strings.TrimSpace(answer)
	if fenced := unfence(raw); fenced != "" {
		raw = fenced
	}
	var parsed siblingVerdict
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return Check{Note: "ответ различающей сверки не разобран: " + err.Error()}
	}
	verdict := strings.TrimSpace(strings.ToLower(parsed.Verdict))
	switch verdict {
	case verdictConfirms, verdictContradicts, verdictInsufficient:
	default:
		// Непонятое не применяется: вердикт не из словаря нельзя ни
		// засчитать за подтверждение, ни объявить чистым — и то и другое
		// было бы догадкой о том, что модель имела в виду.
		return Check{Note: fmt.Sprintf("непонятный вердикт %q", parsed.Verdict)}
	}
	return Check{
		Done:    true,
		Verdict: &Verdict{Answer: verdict, Why: strings.TrimSpace(parsed.Why)},
	}
}

// titleWord — слово источника с заглавной буквы, для подписи в задании.
//
// Своей крошечной функцией, а не strings.Title: та объявлена устаревшей и
// ломает многобайтные буквы, а словарь источника пишется по-русски.
func titleWord(word string) string {
	word = strings.TrimSpace(word)
	if word == "" {
		return "Положение"
	}
	runes := []rune(word)
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}
