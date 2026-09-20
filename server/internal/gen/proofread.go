package gen

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"curator/server/internal/llm"
)

// Литературная вычитка написанной задачи.
//
// Модель пишет условие за один проход и следит одновременно за существом
// дела, за положениями источника и за форматом ответа. Язык при этом идёт
// последним, и это видно в готовых задачах: «Пациент» в заголовке и
// «Пациентка» в третьей фразе, опечатки, канцелярит.
//
// Часть этого снимает словарь врачебных форм (gender.go), но он закрытый
// по замыслу: всё, чего в нём нет, остаётся как есть, потому что молча
// переписывать врачебный текст опаснее, чем оставить в нём огрех. Именно
// поэтому он и не закрывает задачу — «уволился с должности инженера» и
// рассогласованный причастный оборот в словарь не войдут никогда.
//
// Здесь — этап без словаря: отдельный проход модели, которая не пишет
// задачу, а вычитывает уже написанную. Независимый ровно в том же смысле,
// что и слепая сверка: своим запросом, иначе автор согласится сам с собой.
//
// # Главное в этом файле не задание модели, а заслон на выходе
//
// Редактору с полной свободой править врачебный текст доверять нельзя:
// там, где он «улучшит» дозу, срок или добавит от себя обстоятельство,
// верным станет другой ответ. Поэтому принимается не всё, что он вернул,
// а только то, что прошло проверку, — а отклонённое показывается
// составителю, чтобы он применил правку рукой.

// ProofreadChange — одна правка: было и стало.
//
// У отклонённых заполнен ещё и Reason. Показывать их обязательно:
// отклонённая правка часто верна по сути и не прошла только по заслону,
// и составитель применит её рукой — если увидит.
type ProofreadChange struct {
	// Field — что правилось: "title" или порядковый номер фрагмента.
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
	Reason string `json:"reason,omitempty"`
}

// Proofread — итог вычитки.
//
// Done отделён от содержимого по тому же доводу, что у слепой сверки:
// «вычитки не было» и «вычитана, замечаний нет» — разные вещи, и пустой
// список правок выражает только вторую.
type Proofread struct {
	Done bool   `json:"done"`
	Note string `json:"note,omitempty"`

	// Gender — род, к которому приводился текст.
	Gender Gender `json:"gender,omitempty"`

	Changed  []ProofreadChange `json:"changed,omitempty"`
	Rejected []ProofreadChange `json:"rejected,omitempty"`

	// Remark — что сказать составителю, его словами. Считается при
	// чтении и не хранится — тот же довод, что у SiblingCheck.Remark.
	Remark string `json:"remark,omitempty"`
}

// Note — замечание составителю по итогу вычитки.
//
// Пустая строка — сказать нечего: вычитка прошла, отклонённого нет.
// Невычитанное условие публиковать можно, оно просто хуже, — поэтому это
// замечание, а не отказ.
func (p Proofread) Remarks() string {
	if !p.Done {
		note := strings.TrimSpace(p.Note)
		if note == "" {
			note = "причина не записана"
		}
		return "условие не вычитано: " + note
	}
	if len(p.Rejected) == 0 {
		return ""
	}
	fields := make([]string, 0, len(p.Rejected))
	for _, r := range p.Rejected {
		fields = append(fields, fmt.Sprintf("%s (%s)", r.Field, r.Reason))
	}
	return "правки редактора отклонены заслоном, посмотрите их глазами: " +
		strings.Join(fields, "; ")
}

// segmentID — как фрагмент назван в задании редактору.
//
// У фрагмента Curator своего опознавателя нет — он опознаётся порядком в
// условии. Значит номер выдаётся здесь и здесь же разбирается обратно, а
// незнакомый номер отбивает ответ целиком: редактор, который путает
// фрагменты, перекроил условие, а не вычитал его.
func segmentID(i int) string { return "s" + strconv.Itoa(i+1) }

// segmentIndex — обратный разбор. Второе значение false, если номер не наш.
func segmentIndex(id string, n int) (int, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	if !strings.HasPrefix(id, "s") {
		return 0, false
	}
	num, err := strconv.Atoi(id[1:])
	if err != nil || num < 1 || num > n {
		return 0, false
	}
	return num - 1, true
}

// editedDraft — ответ редактора.
type editedDraft struct {
	Title    string `json:"title"`
	Segments []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"segments"`
}

// proofreadSchema — строгая схема ответа редактора.
//
// Пустой title законен и означает «заголовок не правил»; пустой список
// фрагментов — «замечаний нет». Возвращаются ТОЛЬКО правленые фрагменты:
// нетронутый фрагмент не перечисляется вовсе. Это и дешевле — иначе
// редактор перепечатывал бы всё условие в каждом ответе, самыми дорогими
// выходными токенами, — и целее: непересказанный фрагмент не может быть
// перевран, а его разметка положений остаётся при нём по построению.
func proofreadSchema() json.RawMessage {
	return json.RawMessage(`{
	"type": "object",
	"properties": {
		"title": {"type": "string"},
		"segments": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"id": {"type": "string"},
					"text": {"type": "string"}
				},
				"required": ["id", "text"],
				"additionalProperties": false
			}
		}
	},
	"required": ["title", "segments"],
	"additionalProperties": false
}`)
}

// genderInstruction — как в задании называется требование к роду.
//
// Когда род известен, его называют прямо. Когда нет — просят определить
// по тексту: сказать «согласуй» и не сказать с чем значит оставить
// редактора ровно в том положении, из-за которого текст и рассогласовался.
func genderInstruction(want Gender) string {
	switch want {
	case GenderMale:
		return "Человек, о котором идёт речь, — мужчина."
	case GenderFemale:
		return "Человек, о котором идёт речь, — женщина."
	}
	return "Определи по тексту, мужчина это или женщина, и держись этого решения."
}

// runProofread вычитывает черновик и возвращает принятое заслоном.
//
// Ошибок не возвращает намеренно, как и слепая сверка: этап встроен в уже
// оплаченную генерацию, и ронять готовую задачу из-за отказа корректора
// нельзя — но и промолчать нельзя, поэтому неудача это Done: false с
// причиной.
//
// Идёт ДО записи черновика и до обеих сверок. Иначе сверки мерили бы
// текст, которого обучающийся не увидит: вычитка меняет условие, а
// проверять надо то, что уйдёт ему.
func (r *Runner) runProofread(ctx context.Context, job Job, draft Draft) (Draft, Proofread) {
	text := draft.Title + " " + draft.Condition()
	want := DominantGender(text)
	report := Proofread{Gender: want}

	if len(draft.Segments) == 0 {
		report.Note = "пустое условие"
		return draft, report
	}

	prompt, err := r.prompts.ForNode(ctx, NodeProofread)
	if err != nil {
		report.Note = "задание вычитки не прочитано: " + err.Error()
		return draft, report
	}

	blind := job.Plan.Blinded()
	user := Render(prompt.UserMd, blind)
	// Род, заголовок и фрагменты подставляются здесь, а не в Render: это
	// не свойства заказа, а то, что написала модель на прошлом узле.
	// Попади они в общий список переменных — стали бы доступны и узлу
	// написания, то есть задание написания показывало бы модели условие,
	// которого ещё нет.
	user = strings.ReplaceAll(user, "{род}", genderInstruction(want))
	user = strings.ReplaceAll(user, "{заголовок}", draft.Title)
	user = strings.ReplaceAll(user, "{фрагменты}", segmentsForProofread(draft))

	answer, err := r.ask(ctx, job, prompt, llm.Prompt{
		System: Render(prompt.SystemMd, blind),
		// Корректору нужна повторяемость: тот же текст — те же правки.
		Temperature: 0.2,
		Schema:      proofreadSchema(),
		SchemaName:  "proofread",
	})
	if err != nil {
		report.Note = err.Error()
		return draft, report
	}

	raw := strings.TrimSpace(answer)
	if fenced := unfence(raw); fenced != "" {
		raw = fenced
	}
	var edited editedDraft
	if err := json.Unmarshal([]byte(raw), &edited); err != nil {
		report.Note = "ответ редактора не разобран: " + err.Error()
		return draft, report
	}

	out, changed, rejected, err := acceptProofread(draft, edited)
	if err != nil {
		// Заслон отверг ответ целиком: считать это вычиткой нельзя.
		report.Note = err.Error()
		return draft, report
	}
	report.Done = true
	report.Changed = changed
	report.Rejected = rejected

	// Сторона могла перемениться: редактор вправе привести к женскому роду
	// текст, начатый мужским. Верим тому, что вышло, а не тому, с чем
	// входили, — иначе словарь тут же увёл бы текст обратно.
	if after := DominantGender(out.Title + " " + out.Condition()); after == GenderMale || after == GenderFemale {
		report.Gender = after
	}
	out.agreeGender(report.Gender)
	return out, report
}

// agreeGender приводит заголовок и условие к нужному роду.
//
// Разбор не трогаем: он написан про существо дела, а не про человека, и
// «убеждена» там может относиться к чему угодно — цена ошибочной правки в
// разборе выше пользы. Варианты ответа тоже: это названия единиц
// источника, и род в них не наш.
func (d *Draft) agreeGender(want Gender) {
	if want != GenderMale && want != GenderFemale {
		return
	}
	d.Title = AgreeGender(d.Title, want)
	for i := range d.Segments {
		d.Segments[i].Text = AgreeGender(d.Segments[i].Text, want)
	}
}

// segmentsForProofread — фрагменты условия для задания.
//
// Номер рядом с текстом: по нему правка ложится обратно на своё место, и
// по нему же заслон видит, что редактор не перекроил разбивку.
func segmentsForProofread(d Draft) string {
	var b strings.Builder
	for i, s := range d.Segments {
		fmt.Fprintf(&b, "%s: %s\n", segmentID(i), s.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Границы, за которыми правка перестаёт быть правкой.
//
// Вычитка меняет длину: «было отмечено наличие снижения настроения» →
// «настроение снижено» короче вдвое. Но за этими же границами прячется и
// дописанное обстоятельство, и выброшенная половина фразы, а отличить
// одно от другого по тексту нельзя.
//
// Границы намеренно узкие. Законная правка, не прошедшая заслон, не
// пропадает: она показывается составителю и применяется рукой. Дописанный
// моделью признак, прошедший молча, не заметит никто — и задача начнёт
// учить другому.
const (
	proofreadMinRatio = 0.6
	proofreadMaxRatio = 1.7

	// proofreadSlack — допуск для коротких фрагментов. На фразе в
	// двадцать знаков доля ничего не значит: запятая и одна буква
	// окончания уже выводят за границу.
	proofreadSlack = 20
)

// acceptProofread складывает правки редактора с исходным текстом,
// принимая только то, что прошло заслон.
//
// Редактор возвращает только правленые фрагменты; непересказанный
// фрагмент остаётся исходным по построению — вместе со своей разметкой
// положений. Потерять фрагмент поэтому невозможно, а перекроить — значит
// назвать чужой номер, и такая правка отбрасывается вся: доверять
// остальному от редактора, который путает фрагменты, нельзя.
//
// Ошибка означает отказ целиком. Пофрагментные отказы возвращаются
// списком rejected, и текст в этих местах остаётся исходным.
func acceptProofread(original Draft, edited editedDraft) (Draft, []ProofreadChange, []ProofreadChange, error) {
	byIndex := make(map[int]string, len(edited.Segments))
	for _, s := range edited.Segments {
		i, ok := segmentIndex(s.ID, len(original.Segments))
		if !ok {
			return original, nil, nil, fmt.Errorf(
				"редактор вернул фрагмент %q, которого в условии нет, — он перекроил условие, а не вычитал его",
				strings.TrimSpace(s.ID))
		}
		if _, seen := byIndex[i]; seen {
			return original, nil, nil, fmt.Errorf(
				"редактор вернул фрагмент %s дважды — разметка положений не пережила бы такую правку",
				segmentID(i))
		}
		byIndex[i] = s.Text
	}

	out := original
	out.Segments = append([]Segment(nil), original.Segments...)

	changed := []ProofreadChange{}
	rejected := []ProofreadChange{}
	take := func(field, before, after string) string {
		after = strings.TrimSpace(after)
		if after == "" || after == strings.TrimSpace(before) {
			return before
		}
		if reason := refuseProofread(before, after); reason != "" {
			rejected = append(rejected, ProofreadChange{
				Field: field, Before: before, After: after, Reason: reason,
			})
			return before
		}
		changed = append(changed, ProofreadChange{Field: field, Before: before, After: after})
		return after
	}

	// Заголовок вычитывается наравне с условием: рассогласование чаще
	// всего начинается именно с него. Пустой заголовок в ответе означает
	// «без замечаний» — take оставит исходный сам.
	out.Title = take("title", original.Title, edited.Title)
	for i := range out.Segments {
		if after, ok := byIndex[i]; ok {
			out.Segments[i].Text = take(segmentID(i), original.Segments[i].Text, after)
		}
	}
	return out, changed, rejected, nil
}

// refuseProofread — почему правку принимать нельзя. Пусто означает «можно».
func refuseProofread(before, after string) string {
	if numbersIn(after) != numbersIn(before) {
		// Сроки, доли, количества и ссылки на пункты — то, чего вычитка
		// не касается вовсе. Изменившееся число означает, что редактор
		// правил не язык.
		return "изменились числа"
	}

	was, now := len([]rune(before)), len([]rune(after))
	if now+proofreadSlack < int(float64(was)*proofreadMinRatio) {
		return "фрагмент укоротился больше чем на треть — похоже на выброшенное обстоятельство"
	}
	if now > int(float64(was)*proofreadMaxRatio)+proofreadSlack {
		return "фрагмент вырос почти вдвое — похоже на дописанное обстоятельство"
	}
	return ""
}

// numbersIn — все числа текста подряд, в порядке появления.
//
// Сравниваются как строка: и состав, и порядок. «31 год, доза 10 мг» и
// «10 лет, доза 31 мг» — разные тексты, хотя числа в них одни и те же.
func numbersIn(text string) string {
	groups := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsDigit(r) })
	return strings.Join(groups, ",")
}
