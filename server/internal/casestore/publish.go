package casestore

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"unicode"
)

// Правила публикации: что мешает задаче поехать к обучающемуся.
//
// Проверяются здесь, а не в хранилище: правило, написанное и в проверке, и
// в запросе, расходится с собой молча. Хранилище только выполняет решение.

// Fault — что не так с задачей.
type Fault struct {
	Where string
	What  string
}

func (f Fault) Error() string { return f.Where + ": " + f.What }

// Faults — все замечания разом.
//
// Разом, а не по первому: составитель правит задачу в одном заходе, и
// отказ, называющий одну беду из четырёх, заставляет его ходить по кругу
// — ровно четыре раза.
type Faults []Fault

func (f Faults) Error() string {
	parts := make([]string, 0, len(f))
	for _, one := range f {
		parts = append(parts, one.Error())
	}
	return strings.Join(parts, "; ")
}

// CheckPublishable перечисляет всё, что мешает раздавать задачу.
//
// Пустой список означает «можно». Проверки те же по смыслу, что у
// черновика, но применяются к другому: черновик проверяется на то, что
// модель ответила понятным, а задача — на то, что её можно показать
// человеку и зачесть его ответ.
func CheckPublishable(c Case, known map[string]bool) Faults {
	var out Faults

	if strings.TrimSpace(c.Body.Title) == "" {
		out = append(out, Fault{"название", "пустое, а это первое, что видно в списке"})
	}
	if strings.TrimSpace(c.Body.Condition()) == "" {
		out = append(out, Fault{"условие", "пустое: показывать нечего"})
	}
	if len(c.Body.Options) < 2 {
		out = append(out, Fault{"варианты", "меньше двух: выбирать не из чего"})
	}
	if _, ok := c.Body.CorrectOption(); !ok {
		out = append(out, Fault{"ответ", "не совпадает ни с одним вариантом — зачесть ответ будет нечем"})
	}

	// Вид задачи и форма вариантов проверяются здесь, а не только у
	// черновика: черновик приходит от модели, а задачу после этого правит
	// человек, и правка мимо вида не встречает на пути ни одной проверки.
	//
	// Устройство такую задачу выбрасывает целиком — и молча, одну из
	// ленты. Выяснялось это раньше так: составитель видит задачу
	// изданной, врач не видит её вовсе, и сказать об этом некому. Отказ
	// здесь стоит минуты правки, а не потерянной задачи.
	switch c.Body.Kind {
	case KindRecognise:
		for i, o := range c.Body.Options {
			if strings.TrimSpace(o.Label) == "" {
				out = append(out, Fault{
					fmt.Sprintf("вариант %d", i+1),
					"без метки единицы, а у узнавания вариант — это единица источника: сверять ответ будет не с чем",
				})
			}
		}
	case KindAction:
		for i, o := range c.Body.Options {
			if strings.TrimSpace(o.Label) != "" {
				out = append(out, Fault{
					fmt.Sprintf("вариант %d", i+1),
					"с меткой единицы, а у выбора действия вариант — это действие, а не единица источника",
				})
			}
		}
	case "":
		out = append(out, Fault{"вид задачи", "не указан, а без него неизвестно, чем сверять ответ"})
	default:
		out = append(out, Fault{
			"вид задачи",
			fmt.Sprintf("«%s» — ни узнавание, ни выбор действия; третьего вида нет", c.Body.Kind),
		})
	}
	if strings.TrimSpace(c.Body.Explanation) == "" {
		// Разбор — половина ценности задачи: без него обучающийся видит
		// вердикт, но не видит, чем он обоснован.
		out = append(out, Fault{"разбор", "пустой: обучающийся увидит вердикт без обоснования"})
	}
	if c.UnitLabel == "" {
		out = append(out, Fault{"единица источника", "не указана: задача повиснет вне подбора"})
	}

	// Обозначения разметки обязаны существовать в источнике. Ссылка на
	// «абз. 7», которого нет, показывается обучающемуся как обоснование —
	// и проверить её он пойдёт в первоисточник, где ничего не найдёт.
	seen := map[string]bool{}
	for i, seg := range c.Body.Segments {
		for _, designation := range seg.Statements {
			if known != nil && !known[designation] {
				out = append(out, Fault{
					fmt.Sprintf("фрагмент %d", i+1),
					fmt.Sprintf("ссылается на «%s», а такого положения у единицы нет", designation),
				})
			}
			seen[designation] = true
		}
	}
	if len(seen) == 0 && len(c.Body.Segments) > 0 {
		out = append(out, Fault{
			"разметка",
			"ни один фрагмент не подтверждает положения — задача проверяет догадку, а не чтение источника",
		})
	}

	// Подсказки: метка и название единицы в условии превращают задачу в
	// проверку внимательности.
	condition := strings.ToLower(c.Body.Condition())
	if c.UnitLabel != "" && strings.Contains(condition, strings.ToLower(c.UnitLabel)) {
		out = append(out, Fault{
			"условие",
			fmt.Sprintf("содержит метку «%s» — это подсказка, и задача проверяет чтение, а не рассуждение", c.UnitLabel),
		})
	}

	return out
}

// NewID выдаёт номер задачи.
//
// Номер не порядковый и не производный от источника. Порядковый называет
// вслух, сколько задач написано, а производный от метки единицы ломается
// на втором источнике с той же нумерацией пунктов — ровно та беда, от
// которой уходит вся эта модель. Основание 32 без похожих знаков: номер
// попадает в отчёты об отказах, и его переписывают с экрана руками.
func NewID() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("номер задачи не выдан: %w", err)
	}
	coder := base32.NewEncoding("abcdefghjkmnpqrstuvwxyz23456789_").WithPadding(base32.NoPadding)
	return "c-" + coder.EncodeToString(raw)[:16], nil
}

// ValidID отвечает, годится ли номер как номер задачи.
//
// Нужен при ввозе из старой базы: там номера свои, и переписывать их —
// значит потерять связь с попытками, которые ввозятся сводкой.
func ValidID(id string) bool {
	if len(id) < 3 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if r == '-' || r == '_' {
			continue
		}
		if unicode.IsLetter(r) && r < unicode.MaxASCII {
			continue
		}
		if unicode.IsDigit(r) && r < unicode.MaxASCII {
			continue
		}
		return false
	}
	return true
}
