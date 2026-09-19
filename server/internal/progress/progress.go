// Прогресс: опыт, уровни и повторение по интервалам.
//
// # Расчёт живёт в двух местах, и это требование, а не небрежность
//
// Клиент без сети должен видеть свой опыт, уровень и то, что ему сегодня
// повторять, а не пустой экран. Значит, считать умеют и сервер, и
// приложение. Две реализации одного расчёта расходятся МОЛЧА: врач увидит
// на устройстве один уровень, в отчёте другой, и кто прав, выяснять будет
// некому.
//
// Сходятся они только благодаря общему эталону — shared/progress-fixtures.json.
// Он не набор примеров для удобства, а определение расчёта: где файл и код
// спорят, прав файл. Отсутствие файла роняет проверку, а не пропускает её.
//
// Пакет ничего не знает ни про базу, ни про HTTP: ему дают числа и
// получают числа. Это граница, а не аккуратность — расчёт, знающий про
// базу, нельзя проверить эталоном, не подняв базу.
package progress

import (
	"math"
	"time"
)

// Rules — правила расчёта, какими их задаёт эталон.
//
// Не константами в коде: пороги и награды правят, глядя на поведение
// клиентов, и сборка ради порога не выпускается. Приложение читает их с
// сервера и считает по ним само — по тому же эталону.
type Rules struct {
	// Thresholds — пороги уровней: сколько опыта нужно, чтобы уровень стал
	// таким. Список закрыт сверху: набравший больше последнего порога
	// остаётся на последнем уровне, потому что бесконечная лестница
	// обесценивает верх.
	Thresholds []int64

	Correct       int64
	Wrong         int64
	RepeatCorrect int64
	RepeatWrong   int64
	ReviewCorrect int64
	ReviewWrong   int64

	EaseStart      float64
	EaseFloor      float64
	FirstInterval  int
	SecondInterval int
}

// Default — правила по умолчанию.
//
// Повторяют эталон, но эталоном не являются: проверка сверяет их с файлом
// и отказывает при расхождении. Числа здесь затем, чтобы свежая установка
// работала до первой настройки, а не затем, чтобы быть вторым местом, где
// живут пороги.
func Default() Rules {
	return Rules{
		Thresholds:    []int64{0, 100, 250, 500, 900, 1500, 2400, 3600, 5200, 7200},
		Correct:       10,
		Wrong:         3,
		RepeatCorrect: 0,
		RepeatWrong:   0,
		ReviewCorrect: 6,
		ReviewWrong:   2,

		EaseStart:      2.5,
		EaseFloor:      1.3,
		FirstInterval:  1,
		SecondInterval: 6,
	}
}

// Level — уровень при таком опыте.
//
// Новый врач уже первого уровня, а не нулевого: нулевой читается как «ты
// никто», и это первое, что человек видит.
func (r Rules) Level(xp int64) int {
	level := 1
	for i, threshold := range r.Thresholds {
		if xp >= threshold {
			level = i + 1
		}
	}
	return level
}

// Award — сколько опыта даёт попытка.
//
// Повторный разбор той же задачи награды не даёт: иначе опыт набивается
// одной задачей, и уровень перестаёт что-либо означать. Повторение по
// интервалам — другое дело: это работа, а не набивание, и у него своя
// награда.
func (r Rules) Award(correct, repeat, review bool) int64 {
	switch {
	case review && correct:
		return r.ReviewCorrect
	case review:
		return r.ReviewWrong
	case repeat && correct:
		return r.RepeatCorrect
	case repeat:
		return r.RepeatWrong
	case correct:
		return r.Correct
	default:
		// Не ноль. Ноль за ошибку учит не отвечать, когда не уверен, а это
		// ровно то поведение, от которого задачи и должны отучать.
		return r.Wrong
	}
}

// State — состояние повторения по одной задаче.
type State struct {
	Ease         float64
	IntervalDays int
	Repetitions  int
	DueAt        time.Time
}

// Next считает состояние после ответа.
//
// SM-2, с оценкой, сведённой к «верно / неверно»: спрашивать у врача,
// насколько легко далась задача, значит спрашивать о том, чего он не
// знает, и получать шум. Верный ответ идёт как оценка 4, неверный — как 2.
//
// Неверный ответ сбрасывает счёт повторений и ставит интервал в сутки, но
// НЕ сбрасывает лёгкость к началу: лёгкость — свойство задачи для этого
// врача, накопленное за месяцы, и стирать его из-за одной ошибки значит
// терять накопленное на пустом месте.
func (r Rules) Next(before State, correct bool, now time.Time) State {
	grade := 2.0
	if correct {
		grade = 4.0
	}

	ease := before.Ease
	if ease == 0 {
		ease = r.EaseStart
	}
	ease = round2(ease + (0.1 - (5-grade)*(0.08+(5-grade)*0.02)))
	if ease < r.EaseFloor {
		// Ниже пола задача возвращается каждый день и превращает
		// повторение в наказание.
		ease = r.EaseFloor
	}

	out := State{Ease: ease}
	if !correct {
		out.Repetitions = 0
		out.IntervalDays = r.FirstInterval
	} else {
		out.Repetitions = before.Repetitions + 1
		switch out.Repetitions {
		case 1:
			out.IntervalDays = r.FirstInterval
		case 2:
			out.IntervalDays = r.SecondInterval
		default:
			// Умножается на ПРЕЖНЮЮ лёгкость, а не на новую: новая учитывает
			// сегодняшний ответ, а интервал назначается за то, как задача
			// шла до него. Взять новую — значит дважды применить один и тот
			// же ответ, и разойтись с эталоном на третьем повторении.
			out.IntervalDays = int(math.Ceil(float64(before.IntervalDays) * before.Ease))
		}
	}
	out.DueAt = now.AddDate(0, 0, out.IntervalDays)
	return out
}

// Fresh — состояние задачи, которую ещё не разбирали.
func (r Rules) Fresh() State { return State{Ease: r.EaseStart} }

// round2 округляет лёгкость до сотых.
//
// Округление здесь не украшение: лёгкость складывается сотнями шагов, и
// без округления Go и Dart разойдутся на последнем знаке — а сравнивать их
// будет эталон, точным равенством.
func round2(v float64) float64 { return math.Round(v*100) / 100 }
