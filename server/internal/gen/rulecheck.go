package gen

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"curator/server/internal/rules"
)

// Исполнение детерминированных проверок свода.
//
// Объявление предиката живёт в `internal/rules` (check.go); сюда вынесено
// только исполнение, и вынесено по существу: сличение русских слов уже
// написано здесь — основы режет детектор подсказок, род считает вычитка, —
// а вторая копия стеммера расходилась бы с первой молча. Зависимость при
// этом идёт в одну сторону: gen знает про rules, rules про gen не знает.
//
// # Про сличение слов
//
// Слова сравниваются не побуквенно и не по вхождению подстроки: «муж»
// входит в «мужчину», и проверка, ищущая подстроку, объявила бы
// нарушением каждое второе условие. Короткие слова сверяются по закрытому
// списку падежных окончаний, длинные — по общему началу основ, ровно как
// в детекторе подсказок.

// Finding — одно замечание проверки.
//
// Важности и рода у него нет: их проставляет свод, потому что зависят они
// от ПРАВИЛА, а не от предиката. Проверке положено отвечать на один
// вопрос — нарушено ли и где.
type Finding struct {
	// Where — где нашлось: «segments[2]», «title», «explanationMd».
	// Тем же видом, что у детектора подсказок: студия подсвечивает
	// фрагмент по этой строке, и две записи одного адреса разошлись бы
	// молча.
	Where   string `json:"where"`
	Message string `json:"message"`
}

// RunCheck исполняет проверку над черновиком.
//
// Пустой список — исправно. Неисполнимая проверка тоже даёт пустой
// список, и это намеренно: негодную отсеивает Known при сохранении, а
// падать на данных из базы проверке нечем.
func RunCheck(draft Draft, check *rules.Check) []Finding {
	if !check.Known() {
		return nil
	}
	fields := check.Fields()

	// Пол, если правило им ограничено. Считается по условию целиком:
	// перевес по всему тексту надёжнее одной фразы.
	if check.Gender != "" {
		if string(DominantGender(draft.Condition())) != check.Gender {
			return nil
		}
	}

	switch check.Type {
	case rules.CheckForbidWords:
		return checkForbidWords(draft, check, fields)
	case rules.CheckRequireWords:
		return checkRequireWords(draft, check, fields)
	case rules.CheckForbidWhen:
		return checkForbidWhen(draft, check, fields)
	case rules.CheckSpouseGender:
		return checkSpouseGender(draft, fields)
	case rules.CheckLength:
		return checkLength(draft, check, fields)
	case rules.CheckCount:
		return checkCount(draft, check)
	case rules.CheckPattern:
		return checkPattern(draft, check, fields)
	}
	return nil
}

// --- Куски задачи ---

// checkTarget — кусок черновика, по которому идёт проверка.
type checkTarget struct {
	where string // адрес для замечания: «segments[2]», «explanationMd»
	label string // как назвать место составителю: «условии», «разборе»
	text  string
}

// checkTargets раскладывает черновик на куски согласно Where.
//
// Условие раскладывается по фрагментам, а не сплошняком: замечание,
// указывающее на конкретный фрагмент, студия подсвечивает, а указывающее
// на «условие вообще» — нет. Длине и счёту это не нужно: им нужен текст
// целиком.
func checkTargets(draft Draft, fields []string) []checkTarget {
	var out []checkTarget
	for _, field := range fields {
		switch field {
		case rules.FieldExplanation:
			out = append(out, checkTarget{
				where: "explanationMd", label: "разборе", text: draft.ExplanationMd,
			})
		case rules.FieldTitle:
			out = append(out, checkTarget{where: "title", label: "заголовке", text: draft.Title})
		default:
			for i, s := range draft.Segments {
				out = append(out, checkTarget{
					where: fmt.Sprintf("segments[%d]", i),
					label: "условии",
					text:  s.Text,
				})
			}
		}
	}
	return out
}

// wholeText — текст всех проверяемых кусков сплошняком.
func wholeText(draft Draft, fields []string) string {
	parts := make([]string, 0, 8)
	for _, t := range checkTargets(draft, fields) {
		if t.text != "" {
			parts = append(parts, t.text)
		}
	}
	return strings.Join(parts, " ")
}

func primaryField(fields []string) string {
	if len(fields) == 0 {
		return "segments"
	}
	switch fields[0] {
	case rules.FieldExplanation:
		return "explanationMd"
	case rules.FieldTitle:
		return "title"
	}
	return "segments"
}

// --- Сличение слов ---

// падежные — окончания, которые проверка готова отбросить у КОРОТКОГО
// слова.
//
// Список закрыт и умышленно короток. Нужен он ровно для одной беды: «муж»
// — слово из трёх букв, и общее начало основ, которым сличаются длинные
// слова, свело бы его с «мужчиной» и «мужским». Здесь же «мужчина» даёт
// остаток «чина», которого в списке нет, а «мужем» — остаток «ем»,
// который есть.
var падежные = map[string]bool{
	"":  true,
	"а": true, "у": true, "е": true, "и": true, "ы": true, "о": true,
	"ю": true, "й": true, "ь": true,
	"ом": true, "ем": true, "ой": true, "ей": true, "ах": true, "ям": true,
	"ам": true, "ов": true, "ев": true, "ья": true, "ье": true, "ью": true,
	"ами": true, "ями": true, "иях": true, "ьям": true,
}

const (
	// короткоеСлово — до какой длины слово сличается по окончаниям, а не
	// по общему началу основ. Пять знаков включительно.
	//
	// У донора граница стояла строго ниже пяти, и на самой пятёрке была
	// щель: «среда» и «среду» делят четыре знака — на один меньше, чем
	// требует длинный путь, — и правило про среду не находило среды ни
	// в одном падеже, кроме именительного. Молча: проверка была, слова
	// стояли, находок не было.
	//
	// Опустить длинный порог до четырёх нельзя: ровно на четырёх сходятся
	// «супруг» с «супрастином» и «мания» с «манипуляцией», и обе пары
	// проверены. А пятибуквенное слово на коротком пути безопасно: там
	// сверяется ОСТАТОК по закрытому списку падежных окончаний, и
	// «манипулятивное» минус «мани» даёт «пулятивное», которого в списке
	// нет. Обе ловушки закрыты проверками рядом.
	короткоеСлово = 5

	// общееНачало — сколько знаков должны совпасть у начала длинных слов.
	//
	// Пять. Четырёх мало: именно на четырёх сходятся «супруг» с
	// «супрастином» и «мания» с «манипуляцией». Шесть уже отсекает
	// правильные пары вроде «ритуал»/«ритуалы».
	общееНачало = 5
)

// словоСовпало — то же ли это слово, что искомое.
//
// Два способа, и выбор между ними по длине искомого:
//
//   - короткое сличается точно, с отбрасыванием падежного окончания;
//   - длинное — по общему началу основ, как в детекторе подсказок:
//     стеммер режет два знака, и словоформы разной длины дают разные
//     основы («ритуалы» → «ритуа», «ритуал» → «риту»), которые сводит
//     только общее начало.
func словоСовпало(word, needle string) bool {
	word = strings.ReplaceAll(strings.ToLower(word), "ё", "е")
	needle = strings.ReplaceAll(strings.ToLower(needle), "ё", "е")
	if word == needle {
		return true
	}
	if len([]rune(needle)) <= короткоеСлово {
		base := короткаяОснова(needle)
		if !strings.HasPrefix(word, base) {
			return false
		}
		return падежные[strings.TrimPrefix(word, base)]
	}

	// Общего начала основ мало. Стеммер режет два знака, и «супруг» даёт
	// основу «супр», которая начинает «супрастин»; «мания» даёт «мани»,
	// которое начинает «манипулятивное». Оба совпадения проверены и оба
	// ложные. Поэтому вторым условием — общее начало самих СЛОВ.
	if общаяДлина(word, needle) < общееНачало {
		return false
	}
	ws, ns := stemRu(word), stemRu(needle)
	if len([]rune(ws)) < cueMinStem || len([]rune(ns)) < cueMinStem {
		return false
	}
	return strings.HasPrefix(ws, ns) || strings.HasPrefix(ns, ws)
}

func общаяДлина(a, b string) int {
	ar, br := []rune(a), []rune(b)
	n := 0
	for n < len(ar) && n < len(br) && ar[n] == br[n] {
		n++
	}
	return n
}

// короткаяОснова — основа короткого слова: словарная форма без своего
// окончания.
//
// Нужна ровно потому, что искомое слово составитель пишет в именительном
// падеже. «Жена» — это «жен» плюс «а», и без отбрасывания собственного
// окончания проверка не нашла бы ни «жены», ни «женой»: они начинаются не
// с «жена». «Муж» же кончается согласным, и трогать его нельзя — «му»
// совпало бы с «мужчиной», «музыкой» и «мучением» разом.
func короткаяОснова(needle string) string {
	runes := []rune(needle)
	if len(runes) < 2 {
		return needle
	}
	switch runes[len(runes)-1] {
	case 'а', 'я', 'о', 'е', 'ы', 'и', 'у', 'ю', 'ь', 'й':
		return string(runes[:len(runes)-1])
	}
	return needle
}

// словаТекста — значимые слова текста в порядке следования.
func словаТекста(text string) []string {
	return strings.FieldsFunc(strings.ReplaceAll(strings.ToLower(text), "ё", "е"),
		func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// найденные — какие из искомых слов стоят в тексте.
//
// Возвращает найденные СЛОВОФОРМЫ, а не искомые слова: составителю надо
// показать то, что стоит в задаче, а не то, что записано в правиле.
// allowNegated отсекает находки внутри отрицательных оборотов — тем же
// словарём, что и детектор подсказок.
func найденные(text string, needles []string, allowNegated bool) []string {
	var hits []string
	seen := map[string]bool{}

	fragments := []string{text}
	if allowNegated {
		// Границы — все, на которых кончается мысль, а не только точка с
		// запятой: разбор написан разметкой, переводов строк там больше,
		// чем точек, и отрицание из одной строки глушило бы находку в
		// следующей.
		fragments = strings.FieldsFunc(text, func(r rune) bool {
			return r == '.' || r == ';' || r == '\n' || r == '!' || r == '?'
		})
	}
	for _, fragment := range fragments {
		if allowNegated && отрицание.MatchString(strings.ToLower(fragment)) {
			continue
		}
		for _, word := range словаТекста(fragment) {
			if seen[word] {
				continue
			}
			for _, needle := range needles {
				if словоСовпало(word, needle) {
					seen[word] = true
					hits = append(hits, word)
					break
				}
			}
		}
	}
	return hits
}

func вКавычках(words []string) string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, fmt.Sprintf("«%s»", w))
	}
	return strings.Join(out, ", ")
}

// --- Словарные предикаты ---

func checkForbidWords(draft Draft, check *rules.Check, fields []string) []Finding {
	var out []Finding
	for _, t := range checkTargets(draft, fields) {
		hits := найденные(t.text, check.Words, check.AllowNegated)
		if len(hits) == 0 {
			continue
		}
		out = append(out, Finding{
			Where:   t.where,
			Message: fmt.Sprintf("в %s стоит %s", t.label, вКавычках(hits)),
		})
	}
	return out
}

// checkRequireWords ищет обязательное — и потому смотрит на все куски
// разом.
//
// Замечание на каждый фрагмент, где обязательного слова нет, — это
// замечание на каждый фрагмент: обязательное слово стоит в одном месте, а
// не во всех. Требование выполнено, если оно выполнено хоть где-то.
func checkRequireWords(draft Draft, check *rules.Check, fields []string) []Finding {
	if len(найденные(wholeText(draft, fields), check.Words, false)) > 0 {
		return nil
	}
	return []Finding{{
		Where:   primaryField(fields),
		Message: fmt.Sprintf("нет ничего из обязательного: %s", вКавычках(check.Words)),
	}}
}

// checkForbidWhen — несовместимость.
//
// Условие ищется по тексту целиком, а запрещённое — по кускам: раз в
// задаче сказано про одно, второго не должно быть нигде. Обратный порядок
// («если в этом же фрагменте») дал бы правило, которое обходится
// переносом слова в соседнее предложение.
func checkForbidWhen(draft Draft, check *rules.Check, fields []string) []Finding {
	whole := wholeText(draft, fields)
	trigger := найденные(whole, check.When, check.AllowNegated)
	if len(trigger) == 0 {
		return nil
	}

	var out []Finding
	for _, t := range checkTargets(draft, fields) {
		hits := найденные(t.text, check.Words, check.AllowNegated)
		if len(hits) == 0 {
			continue
		}
		out = append(out, Finding{
			Where: t.where,
			Message: fmt.Sprintf("в задаче есть %s — и вместе с ним в %s стоит %s",
				вКавычках(trigger), t.label, вКавычках(hits)),
		})
	}
	return out
}

// --- Длина, счёт, выражение ---

func checkLength(draft Draft, check *rules.Check, fields []string) []Finding {
	// Знаки, а не байты: предел задаёт человек, глядя на текст, и
	// «800 знаков» должно значить восемьсот букв, а не четыреста.
	n := len([]rune(wholeText(draft, fields)))
	switch {
	case check.Min > 0 && n < check.Min:
		return []Finding{{Where: primaryField(fields),
			Message: fmt.Sprintf("длина %d знаков — короче положенных %d", n, check.Min)}}
	case check.Max > 0 && n > check.Max:
		return []Finding{{Where: primaryField(fields),
			Message: fmt.Sprintf("длина %d знаков — длиннее положенных %d", n, check.Max)}}
	}
	return nil
}

func checkCount(draft Draft, check *rules.Check) []Finding {
	n, where := len(draft.Segments), "segments"
	switch check.What {
	case rules.CountOptions:
		n, where = len(draft.Options), "options"
	case rules.CountMarked:
		// РАЗНЫХ положений, а не ссылок: три фрагмента, размеченные одним
		// и тем же положением, обосновывают одно, а не три.
		seen := map[string]bool{}
		for _, s := range draft.Segments {
			for _, ref := range s.Statements {
				if key := strings.ToLower(strings.TrimSpace(ref)); key != "" {
					seen[key] = true
				}
			}
		}
		n = len(seen)
	}

	what := rules.CountTitle(check.What)
	switch {
	case check.Min > 0 && n < check.Min:
		return []Finding{{Where: where,
			Message: fmt.Sprintf("%s всего %d — меньше положенных %d", what, n, check.Min)}}
	case check.Max > 0 && n > check.Max:
		return []Finding{{Where: where,
			Message: fmt.Sprintf("%s уже %d — больше положенных %d", what, n, check.Max)}}
	}
	return nil
}

func checkPattern(draft Draft, check *rules.Check, fields []string) []Finding {
	re, err := regexp.Compile(check.Pattern)
	if err != nil {
		return nil
	}
	var out []Finding
	for _, t := range checkTargets(draft, fields) {
		found := re.FindString(t.text)
		if found == "" {
			continue
		}
		out = append(out, Finding{
			Where:   t.where,
			Message: fmt.Sprintf("в %s стоит «%s»", t.label, обрезать(found, 120)),
		})
	}
	return out
}

func обрезать(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// --- Супруг по полу ---

// формыСупруга — словоформы, по которым супруга узнают ОДНОЗНАЧНО.
//
// Однозначно — ключевое слово, и оно исключило половину напрашивавшихся
// форм. «Супруга» в русском тексте бывает и женой в именительном падеже,
// и мужем в родительном («со слов супруга»); «супругу» — и женой в
// винительном, и мужем в дательном. Такие формы сюда не входят вовсе:
// проверка, срабатывающая на неоднозначности, приучает не читать
// замечания.
//
// Оставлено то, что спутать нельзя: «муж» со своими падежами, «жена» со
// своими и творительный падеж обоих супругов.
var формыСупруга = map[string]Gender{
	"муж": GenderMale, "мужа": GenderMale, "мужу": GenderMale,
	"мужем": GenderMale, "муже": GenderMale, "мужья": GenderMale,
	"супругом": GenderMale,

	"жена": GenderFemale, "жены": GenderFemale, "жене": GenderFemale,
	"жену": GenderFemale, "женой": GenderFemale, "женою": GenderFemale,
	"супругой": GenderFemale, "супругою": GenderFemale,
}

// притяжательные — слова, после которых супруг принадлежит тому, о ком
// речь. Список закрытый и короткий: в условии о человеке «его» и «её»
// значат его самого.
var притяжательные = map[string]bool{
	"его": true, "ее": true, "их": true,
	"свой": true, "своя": true, "свою": true, "своего": true,
	"своей": true, "своим": true, "своею": true, "свое": true,
	// Оборот пересказа: «со слов мужа», «по словам жены».
	"слов": true, "словам": true, "слову": true,
}

// чужаяРодня — родня в родительном падеже, отбирающая супруга у того, о
// ком условие: «муж сестры», «жена брата».
//
// Нужна ровно для случая, когда чужой супруг стоит подлежащим в начале
// предложения и все прочие признаки за него. Список короток намеренно:
// это не морфология, а перечень оборотов, встречающихся в условиях.
var чужаяРодня = map[string]bool{
	"сестры": true, "брата": true, "дочери": true, "сына": true,
	"матери": true, "отца": true, "подруги": true, "друга": true,
	"соседки": true, "соседа": true, "коллеги": true, "племянницы": true,
	"племянника": true, "внучки": true, "внука": true,
}

// checkSpouseGender — назван ли супруг вопреки полу того, о ком условие.
//
// Ровно тот случай, ради которого свод и заводился: «пациент, мужчина 43
// лет… его муж рассказывает». Заданием это не лечится — модель сбивается
// на согласовании тем чаще, чем длиннее текст, — а проверяется в одну
// строку.
//
// Проверка МОЛЧИТ, когда пол не определён: половина условий написана
// так, что род виден по одному слову, и объявлять нарушением всё, в чём
// мы не уверены, значит завалить составителя пустыми замечаниями. Молчит
// она и на условии, чей род спорит сам с собой: об этом уже говорит
// вычитка, и второе замечание о том же ничего не добавит.
//
// И главное, чему научила донора живая проверка: **супруг в условии не
// всегда супруг того, о ком речь.** Первая редакция ловила любое
// упоминание и объявляла нарушением обычный текст:
//
//	«Пациентка Л., 31 год, жена военнослужащего»  — чужая роль в анамнезе
//	«конфликтует с мужем сестры»                   — чужой супруг
//	«считает себя плохой женой и матерью»          — роль самой больной
//
// Все три — нормальный социальный анамнез, и замечание на них убило бы
// доверие ко всей проверке быстрее, чем она поймала бы хоть одну
// настоящую ошибку.
//
// Поэтому спрашивается не «есть ли в тексте слово», а «сказано ли, что
// это супруг ТОГО, О КОМ УСЛОВИЕ». Признаков три, и все закрытые и
// проверяемые глазами: слово стоит в начале предложения (обычное
// подлежащее — «Муж отмечает…»), или перед ним притяжательное («его муж»,
// «своей жене»), или оно введено оборотом пересказа («со слов мужа»).
// Полной морфологии тут нет и быть не должно — по той же причине, что и в
// словаре родов: угадывание портит текст ровно так же, как его портило
// слепое согласование.
func checkSpouseGender(draft Draft, fields []string) []Finding {
	кто := DominantGender(draft.Condition())
	if кто != GenderMale && кто != GenderFemale {
		return nil
	}

	var out []Finding
	for _, t := range checkTargets(draft, fields) {
		var wrong []string
		seen := map[string]bool{}
		for _, spot := range упоминанияСупруга(t.text) {
			g, ok := формыСупруга[spot.word]
			// Супруг ТОГО ЖЕ пола — вот и нарушение: «муж» при мужчине.
			if !ok || g != кто || seen[spot.word] || !spot.свой {
				continue
			}
			seen[spot.word] = true
			wrong = append(wrong, spot.word)
		}
		if len(wrong) == 0 {
			continue
		}
		о, должен := "мужчине", "жена"
		if кто == GenderFemale {
			о, должен = "женщине", "муж"
		}
		out = append(out, Finding{
			Where: t.where,
			Message: fmt.Sprintf("условие о %s, а супруг назван того же пола (%s) — должна быть %s",
				о, вКавычках(wrong), должен),
		})
	}
	return out
}

// упоминаниеСупруга — упоминание и ответ на вопрос, чей это супруг.
type упоминаниеСупруга struct {
	word string
	свой bool
}

// упоминанияСупруга находит упоминания супруга и решает, о чьём речь.
//
// Возвращает ВСЕ упоминания, а не только свои: решать, нарушение это или
// нет, — дело проверки, а здесь только разбор текста.
func упоминанияСупруга(text string) []упоминаниеСупруга {
	words := словаТекста(text)

	// Какие слова начинают предложение. Считается по исходному тексту:
	// словаТекста знаков препинания не оставляет, а именно они и отмечают
	// границу. Номер слова ведётся тем же счётом — иначе отметка встанет
	// не на то слово.
	starts := map[int]bool{}
	idx, новое, вСлове := 0, true, false
	for _, r := range strings.ReplaceAll(strings.ToLower(text), "ё", "е") {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !вСлове {
				if новое {
					starts[idx] = true
					новое = false
				}
				вСлове = true
			}
			continue
		}
		if вСлове {
			idx++
			вСлове = false
		}
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			новое = true
		}
	}

	var out []упоминаниеСупруга
	for n, word := range words {
		if _, ok := формыСупруга[word]; !ok {
			continue
		}
		свой := starts[n]
		if n > 0 && притяжательные[words[n-1]] {
			свой = true
		}
		// Чужая родня отбирает супруга обратно, даже если он подлежащее.
		if n+1 < len(words) && чужаяРодня[words[n+1]] {
			свой = false
		}
		out = append(out, упоминаниеСупруга{word: word, свой: свой})
	}
	return out
}
