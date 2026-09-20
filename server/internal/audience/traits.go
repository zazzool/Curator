// Группы врачей: кого считать одной аудиторией и чем это открывает наборы.
//
// # Почему группа описывается признаками, а не ярлыком
//
// Ярлык, навешенный руками, работает на десяти врачах и перестаёт на
// тысяче: раздать его некому, а через месяц никто не вспомнит, кому и за
// что он достался. Признак же пересчитывается сам, и врач, переставший
// платить, выходит из группы плательщиков в тот же час без чьего-либо
// участия. Поимённый список при этом остаётся — для кафедры, которой
// выдали доступ списком, он единственный разумный способ, — но он частный
// случай, а не основа.
//
// # Почему «и», а не дерево условий
//
// Правило — это список признаков, соединённых «и», и больше ничего.
// «Или» делается тем, что набор открывается сразу нескольким группам: то
// же самое «или», только видно его на карточке набора, а не внутри
// правила. Дерево со скобками — это язык запросов, вписанный в студию, и
// правильность такого правила потом не проверит никто, включая того, кто
// его написал.
package audience

import (
	"encoding/json"
	"fmt"
)

// Окна, за которые считается поведение. Словарь закрытый, и закрытость
// эта не украшение: портрет врача считается одним запросом, и окно,
// названное правилом произвольно, превратило бы этот запрос в сборку
// текста SQL по числу из базы.
//
// Четырёх хватает по смыслу: «вообще», «на этой неделе», «в этом месяце»
// и «в этом квартале». Пятое окно добавляется строкой здесь и колонкой в
// портрете — дорого ровно настолько, чтобы не добавлять его случайно.
const (
	// WindowAll — за всё время.
	WindowAll = "all"
	// Window7, Window30, Window90 — за последние столько суток.
	Window7  = "7d"
	Window30 = "30d"
	Window90 = "90d"
)

// Windows — все окна словаря.
var Windows = []string{WindowAll, Window7, Window30, Window90}

// Признаки. Каждый считается из того, что сервер уже пишет: учётной
// записи, прав и попыток решения. Ночного пересчёта ради них не заводится
// — признак, требующий отдельной сводки, здесь не появится, пока эта
// сводка не понадобится сама по себе.
const (
	// TraitEmail — почта привязана. «Авторизован» у нас значит именно
	// это: запись заводится молча при первом запуске, и отличает
	// назвавшегося от промолчавшего только почта.
	TraitEmail = "email-bound"

	// TraitSubscribed — живая подписка прямо сейчас.
	TraitSubscribed = "subscribed"

	// TraitPaid — когда-либо платил: покупка или подписка в прошлом.
	// Отличается от TraitSubscribed тем, что не истекает: ушедший
	// плательщик — это отдельная и самая интересная аудитория.
	TraitPaid = "ever-paid"

	// TraitAge — учётной записи не меньше N суток. Стаж, а не дата:
	// правило с датой внутри протухает молча, и через год «заведён до
	// марта» означает «все».
	TraitAge = "age-days"

	// TraitIdle — не заходил не меньше N суток. У ни разу не заходившего
	// считается от заведения записи.
	TraitIdle = "idle-days"

	// TraitSolved — решил не меньше N задач за окно.
	TraitSolved = "solved-min"

	// TraitAccuracy — доля верных не ниже N процентов за окно.
	//
	// Порога попыток у признака нет намеренно: он ставится вторым
	// признаком того же правила («решил не меньше 50» и «доля верных не
	// ниже 80»). Для того «и» и заведено, и второе место для того же
	// порога разошлось бы с первым.
	TraitAccuracy = "accuracy-min"

	// TraitActiveDays — занимался не меньше N дней за окно. Именно дней,
	// а не попыток: решивший триста задач за один вечер и решающий по
	// десять каждый день — разные врачи, и путать их незачем.
	TraitActiveDays = "active-days-min"
)

// Traits — все признаки словаря, в порядке показа.
var Traits = []string{
	TraitEmail, TraitSubscribed, TraitPaid,
	TraitAge, TraitIdle,
	TraitSolved, TraitAccuracy, TraitActiveDays,
}

// Condition — один признак правила.
//
// Not держится здесь, а не отдельным признаком «почта НЕ привязана»:
// словарь иначе удваивается, и половина его существует ради отрицания
// другой половины.
type Condition struct {
	Trait string `json:"trait"`

	// N — порог. Смысл зависит от признака: сутки, задачи, проценты, дни.
	// У признаков-флагов не спрашивается.
	N int `json:"n,omitempty"`

	// Over — окно, за которое считается поведение. Пусто — за всё время.
	Over string `json:"over,omitempty"`

	// Not — признак обязан НЕ выполняться.
	Not bool `json:"not,omitempty"`
}

// Rule — правило группы: признаки, соединённые «и».
//
// Пустое правило не означает «все». Оно означает, что группа держится
// только поимённым списком, и это ровно то, что нужно кафедре. Правило
// «все» задаётся признаком, который выполняется у всякого, и пишется
// вслух — а не получается по недосмотру у того, кто забыл дописать
// условие.
type Rule []Condition

// flag говорит, что признак не берёт порога.
func flag(trait string) bool {
	switch trait {
	case TraitEmail, TraitSubscribed, TraitPaid:
		return true
	}
	return false
}

// windowed говорит, что признак считается за окно.
func windowed(trait string) bool {
	switch trait {
	case TraitSolved, TraitAccuracy, TraitActiveDays:
		return true
	}
	return false
}

// knownWindow — есть ли такое окно.
func knownWindow(over string) bool {
	for _, one := range Windows {
		if one == over {
			return true
		}
	}
	return false
}

// ParseRule разбирает правило и отказывает на непонятном.
//
// Отказ, а не пропуск негодного признака: правило, из которого молча
// выбросили условие, продолжает работать — и работает шире, чем написано.
// Группу «платившие и решившие сотню» без второго условия составитель
// увидит как «платившие» только по числу попавших, если вообще заметит.
func ParseRule(raw []byte) (Rule, error) {
	if len(raw) == 0 {
		return Rule{}, nil
	}
	var rule Rule
	if err := json.Unmarshal(raw, &rule); err != nil {
		return nil, fmt.Errorf("правило не разобрано: %w", err)
	}
	if err := rule.Check(); err != nil {
		return nil, err
	}
	return rule, nil
}

// Check проверяет правило целиком.
func (r Rule) Check() error {
	for _, one := range r {
		if err := one.check(); err != nil {
			return err
		}
	}
	return nil
}

func (c Condition) check() error {
	known := false
	for _, one := range Traits {
		if one == c.Trait {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("признака %q не бывает", c.Trait)
	}
	if flag(c.Trait) {
		if c.N != 0 || c.Over != "" {
			return fmt.Errorf("признак %q — это да или нет: порога и окна у него нет", c.Trait)
		}
		return nil
	}
	if c.N < 0 {
		return fmt.Errorf("порог признака %q отрицателен", c.Trait)
	}
	if c.Trait == TraitAccuracy && c.N > 100 {
		return fmt.Errorf("доля верных выше ста процентов не бывает")
	}
	if !windowed(c.Trait) {
		if c.Over != "" {
			return fmt.Errorf("признак %q считается не за окно: окна у него нет", c.Trait)
		}
		return nil
	}
	if c.Over != "" && !knownWindow(c.Over) {
		return fmt.Errorf("окна %q не бывает", c.Over)
	}
	return nil
}

// window — окно условия, с умолчанием.
func (c Condition) window() string {
	if c.Over == "" {
		return WindowAll
	}
	return c.Over
}

// Portrait — то, что известно о враче. Считается в базе одним запросом,
// а применяется здесь: второй реализации правила на SQL нет и не будет,
// и расходиться поэтому нечему.
type Portrait struct {
	EmailBound bool
	Subscribed bool
	EverPaid   bool

	// AgeDays — сколько суток учётной записи.
	AgeDays int

	// IdleDays — сколько суток не заходил. У ни разу не заходившего
	// равен возрасту записи.
	IdleDays int

	// Solved, Correct, ActiveDays — по окнам словаря. Карта, а не
	// поля: окно добавляется строкой в словарь, и переписывать ради
	// него структуру не надо.
	Solved     map[string]int
	Correct    map[string]int
	ActiveDays map[string]int
}

// NeedsBehaviour — нужна ли правилу поведенческая половина портрета.
//
// Спрашивается не ради красоты: поведенческая половина читает попытки
// врача, а их у давнего пользователя десятки тысяч. Пока ни одна группа
// про поведение не спрашивает — этот запрос не идёт вовсе, и на свежей
// установке группы не стоят ничего.
func (r Rule) NeedsBehaviour() bool {
	for _, one := range r {
		if windowed(one.Trait) {
			return true
		}
	}
	return false
}

// Matches — попадает ли врач в группу по этому правилу.
//
// Пустое правило подходит всякому, и это верно: группа с пустым правилом
// держится поимённым списком, а список проверяется не здесь.
func (r Rule) Matches(p Portrait) bool {
	for _, one := range r {
		if !one.holds(p) {
			return false
		}
	}
	return true
}

func (c Condition) holds(p Portrait) bool {
	yes := false
	switch c.Trait {
	case TraitEmail:
		yes = p.EmailBound
	case TraitSubscribed:
		yes = p.Subscribed
	case TraitPaid:
		yes = p.EverPaid
	case TraitAge:
		yes = p.AgeDays >= c.N
	case TraitIdle:
		yes = p.IdleDays >= c.N
	case TraitSolved:
		yes = p.Solved[c.window()] >= c.N
	case TraitActiveDays:
		yes = p.ActiveDays[c.window()] >= c.N
	case TraitAccuracy:
		// Доля при нуле попыток — не сто процентов и не ноль, а
		// «считать не из чего»: признак не выполняется. Иначе врач,
		// не решивший ни одной задачи, попал бы в отличников.
		solved := p.Solved[c.window()]
		yes = solved > 0 && p.Correct[c.window()]*100 >= c.N*solved
	default:
		// Сюда не попасть через ParseRule, и это единственная защита от
		// правила, добравшегося мимо него: непонятый признак не
		// выполняется, а не выполняется молча.
		yes = false
	}
	if c.Not {
		return !yes
	}
	return yes
}

// WindowDays — сколько суток в окне. Ноль у WindowAll: «за всё время»
// сроком не задаётся.
//
// Живёт рядом со словарём намеренно: запрос портрета берёт эти числа
// доводами, а не вписывает их в текст SQL. Окно, добавленное в словарь,
// требует поэтому ещё и колонки в запросе — и это та цена, которая не
// даёт добавить пятое окно по случайности.
var WindowDays = map[string]int{
	WindowAll: 0,
	Window7:   7,
	Window30:  30,
	Window90:  90,
}
