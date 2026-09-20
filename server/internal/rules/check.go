package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Детерминированные проверки свода.
//
// # Зачем они, если правило и так уходит в задание
//
// Правило, живущее только текстом в задании, необязательно к исполнению.
// Это не мнение: у донора три дуэли заданий на одном частном запрете
// кончились протечками три к трём, после чего запрет переехал в код и
// перестал зависеть от сговорчивости модели. Наш детектор подсказок
// (`internal/gen/lint.go`) — тот же случай, доведённый до конца. Здесь
// мысль доведена до общего вида: всякое правило, которое можно проверить
// машинно, проверяется машинно.
//
// # Модель не пишет проверок
//
// Каталог предикатов ЗАКРЫТ и живёт в коде. Модель и составитель могут
// только выбрать предикат из списка и заполнить его параметры, и
// выбранное видит человек. Причина не в недоверии к моделям, а в
// устройстве круга: правило выводится из работы, которую сделала модель,
// и система, которой позволено писать себе проверки, рано или поздно
// напишет проверку, ослабляющую проверку. Это измеренный сбой
// самообучающихся петель, а не предположение.
//
// # Объявление здесь, исполнение в gen
//
// В этом пакете лежит ТОЛЬКО объявление предиката: что он такое, какие у
// него параметры и как он читается по-русски. Исполняется он в
// `internal/gen` (`rulecheck.go`), и разведено это не для красоты:
// сличение русских слов уже написано там — основы режет детектор
// подсказок, род считает вычитка, — а вторая копия стеммера расходилась
// бы с первой молча. Обратной зависимости при этом нет: gen знает про
// rules, rules про gen не знает.

// Род проверки. Список закрыт: новый предикат появляется вместе с кодом,
// который его исполняет, и никак иначе.
type CheckType string

const (
	// CheckForbidWords — слов быть не должно.
	CheckForbidWords CheckType = "forbid-words"
	// CheckRequireWords — хотя бы одно из слов должно быть.
	CheckRequireWords CheckType = "require-words"
	// CheckForbidWhen — если встретилось одно, второго быть не должно.
	CheckForbidWhen CheckType = "forbid-when"
	// CheckSpouseGender — супруг назван вопреки полу того, о ком условие.
	CheckSpouseGender CheckType = "spouse-gender"
	// CheckLength — длина текста в знаках.
	CheckLength CheckType = "length"
	// CheckCount — сколько чего в задаче.
	CheckCount CheckType = "count"
	// CheckPattern — регулярное выражение. Только для составителя: модель
	// выражений не пишет.
	CheckPattern CheckType = "pattern"
)

// Части задачи, в которые проверка смотрит.
const (
	FieldSegments    = "segments"
	FieldExplanation = "explanation"
	FieldTitle       = "title"
)

// Что считает CheckCount.
const (
	// CountSegments — фрагментов условия.
	CountSegments = "segments"
	// CountOptions — вариантов ответа.
	CountOptions = "options"
	// CountMarked — сколько РАЗНЫХ положений источника размечено
	// фрагментами. Счёт этот наш, донорского подобия у него нет:
	// разметка — половина ценности задачи, и «размечено хотя бы два
	// положения» единственное, чем требование к ней записывается правилом.
	CountMarked = "marked"
)

// Вариантов ответа среди мест, куда проверка смотрит, нет намеренно.
//
// У задачи-узнавания вариант — это метка единицы («3.2»), и словарному
// предикату искать в ней нечего: проверка не находила бы ничего никогда,
// а правило выглядело бы работающим. Молчаливая проверка хуже
// отсутствующей — на неё полагаются. Посчитать варианты при этом можно:
// счёт метку читать не обязан (CountOptions).

// Check — проверка правила в разобранном виде.
//
// Одна структура на все предикаты, а не по структуре на каждый. Причина
// приземлённая: проверка ездит в JSONB базы и в форму студии, и семь
// разных записей означали бы семь ветвей разбора в каждом из этих мест.
// Незанятые поля предиката просто пусты.
type Check struct {
	Type CheckType `json:"type"`

	// Where — в каких частях задачи смотреть. Пусто — в условии:
	// подавляющее большинство правил про него.
	Where []string `json:"where,omitempty"`

	// Words — слова предиката: запрещённые у forbid-words, обязательные у
	// require-words, запрещённые при выполнении When у forbid-when.
	Words []string `json:"words,omitempty"`

	// When — слова-условие для forbid-when.
	When []string `json:"when,omitempty"`

	// Gender — правило действует, только когда условие о человеке этого
	// пола. Пол берётся по САМОМУ ТЕКСТУ, а не из отдельного поля: поля
	// такого у задачи нет, и завести его ради проверки значило бы
	// проверять поле, а не текст.
	Gender string `json:"gender,omitempty"`

	// AllowNegated — отрицательная находка нарушением не считается.
	//
	// «Навязчивые мысли отрицает» — не подсказка, а обязательная запись:
	// отсутствие признака нельзя показать, его можно только назвать.
	// Умолчание — считать (false): для большинства запретов отрицание
	// ничего не меняет.
	AllowNegated bool `json:"allowNegated,omitempty"`

	// What — что считает CheckCount.
	What string `json:"what,omitempty"`

	// Min и Max — пределы для length и count. Ноль означает «предела
	// нет»: длина и количество у задачи всегда положительны, и отличать
	// «ноль» от «не задано» здесь не нужно.
	Min int `json:"min,omitempty"`
	Max int `json:"max,omitempty"`

	// Pattern — регулярное выражение для CheckPattern.
	Pattern string `json:"pattern,omitempty"`
}

// Normalize приводит проверку к записанному виду.
func (c *Check) Normalize() {
	if c == nil {
		return
	}
	c.Where = normalizeFields(c.Where)
	c.Words = normalizeCheckWords(c.Words)
	c.When = normalizeCheckWords(c.When)
	c.Pattern = strings.TrimSpace(c.Pattern)
	c.What = strings.TrimSpace(c.What)
	switch c.Gender {
	case GenderMale, GenderFemale:
	default:
		c.Gender = ""
	}
	if c.Min < 0 {
		c.Min = 0
	}
	if c.Max < 0 {
		c.Max = 0
	}
}

// Пол в записи проверки. Значения те же, что у вычитки
// (`internal/gen/gender.go`), и записаны они здесь строками, а не
// ссылкой на её тип: зависимость шла бы в обратную сторону. Сходство
// стережёт зеркальная проверка.
const (
	GenderMale   = "m"
	GenderFemale = "f"
)

// Known — исполним ли предикат.
//
// Проверяется и род предиката, и достаточность параметров. Второе не
// придирчивость: forbid-words без слов запрещает пустоту, то есть не
// срабатывает никогда, и правило с такой проверкой выглядит работающим,
// не будучи им.
func (c *Check) Known() bool {
	if c == nil {
		return false
	}
	switch c.Type {
	case CheckForbidWords, CheckRequireWords:
		return len(c.Words) > 0
	case CheckForbidWhen:
		return len(c.Words) > 0 && len(c.When) > 0
	case CheckSpouseGender:
		return true
	case CheckLength, CheckCount:
		return c.Min > 0 || c.Max > 0
	case CheckPattern:
		if c.Pattern == "" {
			return false
		}
		_, err := regexp.Compile(c.Pattern)
		return err == nil
	}
	return false
}

// Fields — где смотреть на самом деле.
func (c *Check) Fields() []string {
	if c == nil || len(c.Where) == 0 {
		return []string{FieldSegments}
	}
	return c.Where
}

func normalizeFields(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		f := strings.ToLower(strings.TrimSpace(raw))
		switch f {
		case FieldSegments, FieldExplanation, FieldTitle:
		default:
			continue
		}
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeCheckWords(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		// «ё» сводится к «е» здесь же, а не при сличении: слово,
		// записанное составителем через «ё», и слово, написанное моделью
		// через «е», — одно слово, и решать это дважды в разных местах
		// значит развести их молча.
		w := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), "ё", "е")
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CheckSpec — описание предиката: как он зовётся по-русски, какие у него
// параметры и кому доступен.
//
// Отдаётся и форме студии, и — впредь — компилятору правил в задании
// модели. Один источник на обоих: разъедься описание с тем, что исполняет
// код, и выбирающий начал бы уверенно ставить проверки, которые не
// работают.
type CheckSpec struct {
	Type   CheckType `json:"type"`
	Title  string    `json:"title"`
	About  string    `json:"about"`
	Params []string  `json:"params"`

	// DoctorOnly — предикат недоступен модели: слишком остёр.
	DoctorOnly bool `json:"doctorOnly,omitempty"`
}

// Catalog — закрытый каталог предикатов.
func Catalog() []CheckSpec {
	return []CheckSpec{
		{Type: CheckForbidWords, Title: "Запрещённые слова",
			About:  "слов быть не должно; можно ограничить полом того, о ком условие, и разрешить отрицательные находки",
			Params: []string{"words", "where", "gender", "allowNegated"}},
		{Type: CheckRequireWords, Title: "Обязательные слова",
			About:  "хотя бы одно из слов должно стоять в тексте",
			Params: []string{"words", "where", "gender"}},
		{Type: CheckForbidWhen, Title: "Несовместимое",
			About:  "если в задаче есть первое, второго быть не должно",
			Params: []string{"when", "words", "where", "allowNegated"}},
		{Type: CheckSpouseGender, Title: "Супруг по полу",
			About:  "у мужчины жена, у женщины муж; пол определяется по самому условию",
			Params: []string{"where"}},
		{Type: CheckLength, Title: "Длина текста",
			About:  "сколько знаков должно быть в условии, заголовке или разборе",
			Params: []string{"where", "min", "max"}},
		{Type: CheckCount, Title: "Количество",
			About:  "сколько фрагментов условия, вариантов ответа или размеченных положений",
			Params: []string{"what", "min", "max"}},
		{Type: CheckPattern, Title: "Выражение",
			About:      "регулярное выражение; пишет только составитель",
			Params:     []string{"pattern", "where"},
			DoctorOnly: true},
	}
}

// Describe — проверка словами, для списка правил и для журнала.
//
// Фраза называет предмет условия, а не пациента: источник у нас любой, и
// «только у пациентов-мужчин» обещало бы врачебное там, где его нет.
// Механику это не переименовывает — отбор по полу и вправду про человека,
// и у задачи, условие которой не о человеке, он не сработает ни разу.
func Describe(c *Check) string {
	if c == nil {
		return ""
	}
	where := strings.Join(fieldTitles(c.Fields()), " и ")
	gender := ""
	switch c.Gender {
	case GenderMale:
		gender = ", только когда условие о мужчине"
	case GenderFemale:
		gender = ", только когда условие о женщине"
	}

	switch c.Type {
	case CheckForbidWords:
		return fmt.Sprintf("в %s не должно быть: %s%s", where, strings.Join(c.Words, ", "), gender)
	case CheckRequireWords:
		return fmt.Sprintf("в %s должно быть хотя бы одно из: %s%s", where, strings.Join(c.Words, ", "), gender)
	case CheckForbidWhen:
		return fmt.Sprintf("если есть %s, то в %s не должно быть: %s",
			strings.Join(c.When, ", "), where, strings.Join(c.Words, ", "))
	case CheckSpouseGender:
		return "супруг должен быть противоположного пола тому, о ком условие"
	case CheckLength:
		return fmt.Sprintf("длина %s: %s", where, rangeWords(c.Min, c.Max, "знаков"))
	case CheckCount:
		return fmt.Sprintf("%s: %s", CountTitle(c.What), rangeWords(c.Min, c.Max, "штук"))
	case CheckPattern:
		return fmt.Sprintf("в %s не должно встречаться выражение %s", where, c.Pattern)
	}
	return string(c.Type)
}

func fieldTitles(fields []string) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		switch f {
		case FieldExplanation:
			out = append(out, "разборе")
		case FieldTitle:
			out = append(out, "заголовке")
		default:
			out = append(out, "условии")
		}
	}
	return out
}

// CountTitle — что считает предикат, словами.
func CountTitle(what string) string {
	switch what {
	case CountOptions:
		return "вариантов ответа"
	case CountMarked:
		return "размеченных положений"
	default:
		return "фрагментов условия"
	}
}

func rangeWords(min, max int, unit string) string {
	switch {
	case min > 0 && max > 0:
		return fmt.Sprintf("от %d до %d %s", min, max, unit)
	case min > 0:
		return fmt.Sprintf("не меньше %d %s", min, unit)
	case max > 0:
		return fmt.Sprintf("не больше %d %s", max, unit)
	}
	return "без предела"
}
