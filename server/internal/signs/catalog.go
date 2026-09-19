// Знаки отличия.
//
// # Знак выдаёт сервер, а не устройство
//
// Номер, дата выдачи, тираж и доля обладателей — сведения о том, каким врач
// пришёл СРЕДИ ВСЕХ, и вывести их из его собственной истории нельзя.
// Устройство считает только долю пути к знаку: её видно и без сети, и в ней
// нет ничего о других.
//
// # Опыт начисляется по выданному, а не по заслуженному
//
// Знак с тиражом можно заслужить и не получить. Расчёт «по заслугам»
// показал бы врачу опыт за знак, которого у него нет, — и объяснить это
// было бы нечем.
//
// # Выданный знак не отбирают
//
// Строка о выдаче не удаляется никогда. Единственное исключение —
// переходящий знак: он получает отметку об отзыве, а не удаление, потому
// что собирательные знаки смотрят на КОГДА-ЛИБО выданное. Считай они
// владение сейчас, кавалерский знак срывался бы вслед за переходящим, и
// собрать орден было бы нельзя в принципе.
package signs

import "curator/server/internal/progress"

// Kind — вид знака. Словарь закрыт: вид, заведённый по месту, приложение
// не нарисует, а сервер выдаст — и знак повиснет невидимым.
type Kind string

const (
	// KindMetric — за величину, тираж не ограничен.
	KindMetric Kind = "metric"
	// KindEdition — за величину, но тиражом: заслужить можно и не получить.
	KindEdition Kind = "edition"
	// KindCollective — за уже выданные знаки.
	KindCollective Kind = "collective"
	// KindRotating — переходящий: держит его один, лучший по величине.
	KindRotating Kind = "rotating"
)

// Sign — знак, как он объявлен в каталоге.
type Sign struct {
	Slug  string
	Title string
	Kind  Kind

	// Metric и Threshold — за какую величину и с какого значения.
	// У собирательного знака их нет.
	Metric    progress.MetricKey
	Threshold int64

	// Requires — знаки, которые должны быть когда-либо выданы.
	// Только у собирательного.
	Requires []string

	// EditionSize — тираж. Ноль значит «не ограничен».
	EditionSize int

	// XP — опыт за ВЫДАННЫЙ знак.
	XP int64
}

// Catalog — каталог целиком, в порядке эталона.
//
// Порядок значим: знаки показываются врачу списком, и порядок в нём — часть
// того, что человек видит.
func Catalog() []Sign {
	return []Sign{
		{Slug: "first-steps", Title: "Первые шаги", Kind: KindMetric,
			Metric: progress.CasesSolved, Threshold: 10, XP: 50},
		{Slug: "hundred", Title: "Сотня", Kind: KindMetric,
			Metric: progress.CasesSolved, Threshold: 100, XP: 200},
		{Slug: "streak-25", Title: "Двадцать пять подряд", Kind: KindMetric,
			Metric: progress.CorrectStreak, Threshold: 25, XP: 150},
		{Slug: "month", Title: "Месяц занятий", Kind: KindMetric,
			Metric: progress.DaysActive, Threshold: 30, XP: 200},
		{Slug: "reviewer", Title: "Возвращается", Kind: KindMetric,
			Metric: progress.ReviewsDone, Threshold: 100, XP: 150},
		{Slug: "breadth", Title: "Вширь", Kind: KindMetric,
			Metric: progress.SourcesTouched, Threshold: 5, XP: 100},
		{Slug: "pioneer", Title: "Первопроходец", Kind: KindEdition,
			Metric: progress.CasesSolved, Threshold: 1, EditionSize: 100, XP: 300},
		{Slug: "order-of-accuracy", Title: "Орден Точности", Kind: KindCollective,
			Requires: []string{"streak-25", "hundred"}, XP: 400},
		{Slug: "primus", Title: "Primus inter pares", Kind: KindRotating,
			Metric: progress.CorrectStreak, Threshold: 25, XP: 100},
	}
}

// Known отвечает, есть ли такой знак в каталоге.
func Known(slug string) bool {
	_, ok := find(slug)
	return ok
}

func find(slug string) (Sign, bool) {
	for _, s := range Catalog() {
		if s.Slug == slug {
			return s, true
		}
	}
	return Sign{}, false
}

// Earned отвечает, заслужен ли знак по величине.
//
// Заслужен — не то же, что выдан: у знака с тиражом между этими словами
// стоит очередь, и опыт начисляется за второе.
func (s Sign) Earned(m map[progress.MetricKey]int64) bool {
	switch s.Kind {
	case KindMetric, KindEdition, KindRotating:
		return m[s.Metric] >= s.Threshold
	default:
		// Собирательный смотрит не на величины, а на выданные знаки, и
		// отвечать на этот вопрос ему нечем. Отдельный ответ «нет» здесь
		// честнее, чем «да» по пустому порогу.
		return false
	}
}

// Progress — доля пути к знаку, от 0 до 1.
//
// Ровно это и считает устройство: доля видна без сети, и в ней нет ничего
// о других врачах. У собирательного знака доля считается по выданным
// частям, и считает её сервер: сколько частей выдано, знает только он.
func (s Sign) Progress(m map[progress.MetricKey]int64) float64 {
	if s.Threshold <= 0 {
		return 0
	}
	have := m[s.Metric]
	if have >= s.Threshold {
		return 1
	}
	if have <= 0 {
		return 0
	}
	return float64(have) / float64(s.Threshold)
}
