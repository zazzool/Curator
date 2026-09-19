// Опубликованная задача: то, что доезжает до обучающегося.
//
// Третий этап сквозного пути начинается здесь. Черновик, написанный
// конвейером, задачей ещё не является: у него нет ни номера, под которым
// его узнает устройство, ни разрешённых ссылок на положения, ни места в
// источнике, закреплённого на момент публикации. Публикация — это
// превращение написанного в раздаваемое, и она однонаправленна.
//
// Пакет устроен теми же тремя слоями, что и source: модель и правила
// (model.go, publish.go) не знают ни про базу, ни про HTTP; хранилище
// (store.go) знает про базу; ручки (routes.go) знают про HTTP.
package casestore

import "time"

// Status — состояние задачи.
//
// Словарь закрыт и повторяет CHECK в схеме: состояние, появившееся строкой
// по месту, прошло бы мимо всех проверок и осело бы в колонке, которую
// читает раздача.
type Status string

const (
	// StatusDraft — написана, но не показывалась никому.
	StatusDraft Status = "draft"

	// StatusReview — отдана составителю на выверку.
	StatusReview Status = "review"

	// StatusPublished — раздаётся.
	StatusPublished Status = "published"

	// StatusArchived — снята с раздачи. Не удалена: на устройствах она
	// уже стоит, и попытки по ней уже записаны — задача, исчезнувшая из
	// базы, оставила бы попытки, ссылающиеся в никуда.
	StatusArchived Status = "archived"
)

// StatusWord — состояние по-русски: читает это составитель.
func StatusWord(s Status) string {
	switch s {
	case StatusDraft:
		return "черновик"
	case StatusReview:
		return "на выверке"
	case StatusPublished:
		return "раздаётся"
	case StatusArchived:
		return "снята с раздачи"
	default:
		return string(s)
	}
}

// Case — задача целиком.
//
// UnitPath скопирован из источника на момент публикации и с тех пор не
// меняется. Довод записан и в схеме, и повторён здесь, потому что
// нарушить его проще всего именно в коде: подбор по срезу пути обязан
// работать одним запросом, без соединения с деревом источника, а
// переписанный путь у старой задачи означал бы молчаливую смену её места
// в подборе — задача уехала бы в другой раздел, и никто бы этого не
// заметил.
type Case struct {
	ID        string
	SourceID  int64
	UnitLabel string
	UnitPath  string
	Status    Status
	Revision  int
	Origin    string
	Body      Body

	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt *time.Time
}

// Body — содержание задачи, каким его увидит устройство.
//
// Отдельным типом, а не картой: проводной формат меняется только
// добавлением необязательных полей, и уследить за этим можно лишь тогда,
// когда поля названы поимённо в одном месте.
type Body struct {
	Title       string    `json:"title"`
	Kind        string    `json:"kind"`
	Segments    []Segment `json:"segments"`
	Options     []Option  `json:"options"`
	Answer      string    `json:"answer"`
	Explanation string    `json:"explanationMd"`
	Difficulty  int       `json:"difficulty"`
}

// Segment — фрагмент условия.
//
// Statements здесь — обозначения положений («абз. 1»), а не их номера в
// базе: номер осмыслен только внутри нашей базы, а обозначение — то, что
// человек найдёт в первоисточнике. Номера живут отдельно, в case_chunks,
// и заполняются при публикации.
type Segment struct {
	Text       string   `json:"text"`
	Statements []string `json:"statements,omitempty"`
}

// Option — вариант ответа.
type Option struct {
	Label string `json:"label,omitempty"`
	Text  string `json:"text"`
}

// Condition — условие целиком: склейка фрагментов.
//
// Отдельного поля с условием нет намеренно: два места для одного текста
// расходятся молча, и разошлись бы они здесь тем хуже, что видно это
// только читающему задачу целиком.
func (b Body) Condition() string {
	out := ""
	for _, s := range b.Segments {
		if s.Text == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += s.Text
	}
	return out
}

// CorrectOption — верный вариант.
//
// Ищется по метке, а при её отсутствии по тексту: у задачи-узнавания
// вариант это единица источника и у него есть метка, у задачи-действия
// вариант это текст действия и метки у него нет.
func (b Body) CorrectOption() (Option, bool) {
	for _, o := range b.Options {
		if o.Label != "" && o.Label == b.Answer {
			return o, true
		}
		if o.Label == "" && o.Text == b.Answer {
			return o, true
		}
	}
	return Option{}, false
}
