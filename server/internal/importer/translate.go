// Перевод данных прежней системы в модель Куратора.
//
// # Это перевод, а не копирование
//
// Две модели задачи разошлись не в мелочах. В прежней системе вариант
// ответа — это строка-метка кода МКБ, верный ответ назван отдельным полем
// TargetCode, а разметка фрагментов ссылается на критерии по выдуманным
// нами номерам. В Кураторе вариант — пара «метка и текст», задача знает
// свою единицу источника колонкой, а фрагмент ссылается на ОБОЗНАЧЕНИЕ
// положения в первоисточнике.
//
// Разница не случайна: прежняя модель знала про МКБ-10, нынешняя не знает
// ни про один источник. Поэтому перевод обязан быть явным и обязан
// отказывать на непонятом.
//
// # Непонятое не применяется
//
// Задача, которая не переводится целиком, не ввозится вовсе — и
// пересчитывается. Ввезённая наполовину задача выглядит исправной: у неё
// есть условие и варианты, просто верный ответ оказался не тем. Найти
// такую после ввоза нельзя ничем, кроме как решив её.
package importer

import (
	"encoding/json"
	"fmt"

	"curator/server/internal/casestore"
)

// OldCase — тело задачи прежней системы.
//
// Названы только те поля, которые переводятся. Остальные (genDifficulty,
// sourceNote, answerModes) не переносятся намеренно: это сведения о том,
// КАК задачу получили, а не о ней самой, и в новой системе им нет места.
type OldCase struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Difficulty    int            `json:"difficulty"`
	TargetCode    string         `json:"targetCode"`
	AcceptedCodes []string       `json:"acceptedCodes"`
	Options       []string       `json:"options"`
	Segments      []OldSegment   `json:"segments"`
	Criteria      []OldCriterion `json:"criteria"`
	ExplanationMd string         `json:"explanationMd"`

	// Answer — эталонное ДЕЙСТВИЕ у задачи-действия. Пусто у задачи на
	// узнавание, и именно этим два вида и различались: отдельного поля
	// «вид задачи» в прежней модели не было.
	Answer string `json:"answer"`
}

// OldSegment — фрагмент условия прежней системы.
type OldSegment struct {
	Text         string   `json:"text"`
	CriterionIDs []string `json:"criterionIds"`

	// CriterionID — прежнее поле «один критерий на фрагмент». Оно жило и
	// в прежней системе к моменту ввоза, и задачи с ним в базе есть.
	// Потерять его значит потерять разметку у самых старых задач — то
	// есть у тех, которые писались дольше всех.
	CriterionID string `json:"criterionId"`
}

// OldCriterion — положение, на которое ссылается разметка.
type OldCriterion struct {
	ID string `json:"id"`

	// Code — обозначение в первоисточнике («G1», «A»). Пусто, если
	// положение в указаниях не подписано вовсе.
	Code string `json:"code"`

	Label string `json:"label"`
}

// Translated — задача, переведённая целиком.
type Translated struct {
	Body casestore.Body

	// UnitLabel — метка единицы, по которой написана задача. В прежней
	// модели это TargetCode, и она же — верный ответ у задачи-узнавания.
	// В Кураторе это разные вещи, и разводятся они здесь.
	UnitLabel string
}

// Translate переводит тело задачи.
//
// Отказ, а не «сделаем что сможем»: задача, у которой перевод не сошёлся,
// не ввозится вовсе.
func Translate(raw []byte) (Translated, error) {
	var old OldCase
	if err := json.Unmarshal(raw, &old); err != nil {
		return Translated{}, fmt.Errorf("тело задачи не разобрано: %w", err)
	}
	if old.TargetCode == "" {
		// Задача без метки единицы — задача ни о чём: ввезти её значит
		// завести в новой базе запись, которую нечем найти и незачем
		// показывать.
		return Translated{}, fmt.Errorf("у задачи нет метки единицы")
	}

	// Вид задачи вычисляется из данных, а не берётся полем: поля не было.
	// Непустой эталон-действие означал в прежней модели второй вид.
	kind := "recognise"
	answer := old.TargetCode
	if old.Answer != "" {
		kind = "action"
		answer = old.Answer
	}

	// Обозначения положений — по номеру критерия. Ссылка переводится в
	// ОБОЗНАЧЕНИЕ первоисточника, а не в наш номер: номер осмыслен только
	// внутри той базы, которой скоро не будет, а обозначение врач найдёт
	// в самих указаниях.
	byID := make(map[string]string, len(old.Criteria))
	for _, one := range old.Criteria {
		mark := one.Code
		if mark == "" {
			// Не подписано в первоисточнике — берётся метка, под которой
			// его видел составитель. Выдумывать номер нельзя: выдуманная
			// ссылка на место хуже отсутствующей.
			mark = one.Label
		}
		if mark != "" {
			byID[one.ID] = mark
		}
	}

	segments := make([]casestore.Segment, 0, len(old.Segments))
	for _, one := range old.Segments {
		if one.Text == "" {
			continue
		}
		ids := one.CriterionIDs
		if len(ids) == 0 && one.CriterionID != "" {
			ids = []string{one.CriterionID}
		}
		marks := make([]string, 0, len(ids))
		for _, id := range ids {
			if mark, ok := byID[id]; ok {
				marks = append(marks, mark)
			}
			// Ссылка на несуществующий критерий молча отбрасывается, а не
			// роняет задачу: разметка — это подсказка, чем фрагмент
			// подтверждается, и потеря одной ссылки не делает задачу
			// нерешаемой. Потеря верного ответа — делает, и она ниже.
		}
		segments = append(segments, casestore.Segment{Text: one.Text, Statements: marks})
	}
	if len(segments) == 0 {
		return Translated{}, fmt.Errorf("у задачи нет условия")
	}

	// Варианты. В прежней модели это строки: у задачи-узнавания — метки
	// кодов, у задачи-действия — тексты действий. Поэтому метка
	// заполняется только там, где она метка.
	options := make([]casestore.Option, 0, len(old.Options))
	for _, text := range old.Options {
		if text == "" {
			continue
		}
		if kind == "recognise" {
			options = append(options, casestore.Option{Label: text, Text: text})
			continue
		}
		options = append(options, casestore.Option{Text: text})
	}
	if len(options) < 2 {
		// Один вариант — не выбор.
		return Translated{}, fmt.Errorf("у задачи меньше двух вариантов")
	}

	body := casestore.Body{
		Title:       old.Title,
		Kind:        kind,
		Segments:    segments,
		Options:     options,
		Answer:      answer,
		Explanation: old.ExplanationMd,
		Difficulty:  old.Difficulty,
	}

	// Верный ответ обязан быть среди вариантов. Задача, у которой его нет,
	// не решается никем и никогда — и это не сложность, а поломка, которую
	// ввоз обязан заметить здесь, а не оставить врачу.
	//
	// Спрашивается это у самой задачи (casestore), а не считается здесь:
	// правило «чем называется выбор варианта» уже написано там, и вторая
	// его запись разошлась бы с первой молча — ввоз пропустил бы задачу,
	// которую раздача считает поломанной, или наоборот.
	if _, ok := body.CorrectOption(); !ok {
		return Translated{}, fmt.Errorf("верного ответа %q нет среди вариантов", answer)
	}

	return Translated{Body: body, UnitLabel: old.TargetCode}, nil
}
