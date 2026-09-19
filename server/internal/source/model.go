// Разбор текстового источника: первый этап сквозного пути.
//
// Источником может быть что угодно, где есть проверяемые положения:
// классификация, приказ, клинические рекомендации, стандарт, руководство.
// Пакет — весь этап целиком, тремя слоями снизу вверх: устройство
// источника и разбор текста (model.go, path.go, text.go, docx.go, upload.go)
// не знают ни про базу, ни про HTTP и проверяются без того и другого;
// хранилище (store.go) знает про базу; ручки (routes.go) знают про HTTP и
// зовут первые два. Слои не перемешиваются: правило, написанное в разборе,
// в хранилище не повторяется — вторая реализация одного правила расходится
// с первой молча.
//
// Проверять разбор надо целиком: его ошибка не падает, она молча обедняет
// всё, что ниже по пути.
package source

// Kind — вид источника. Словарь закрыт: новый вид — это работа, а не
// значение, незаметно появившееся в колонке.
type Kind string

const (
	KindClassification Kind = "classification"
	KindDecree         Kind = "decree"
	KindGuidelines     Kind = "guidelines"
	KindStandard       Kind = "standard"
	KindHandbook       Kind = "handbook"
	KindOther          Kind = "other"
)

// Completeness — полнота источника.
//
// Обязательна и честна: документ, разобранный из DOCX, — это КУСОК, и
// объявлять его полным нельзя. На полноту опирается расчёт охвата, и ложная
// полнота даёт ложные доли — то есть врёт ровно там, где на неё смотрят.
type Completeness string

// Имена с приставкой вида: Fragment без неё столкнулось бы с куском
// документа, а кусок и полнота — разные вещи, и путать их в коде нельзя.
const (
	CompletenessComplete Completeness = "complete"
	CompletenessFragment Completeness = "fragment"
)

// Purpose — по какой оси источник классифицирует.
//
// Отсюда подбор задач понимает, складываются ли два источника в одну ось
// или стоят поперёк: тема и система органов — разные оси, и смешивать их в
// одном подборе значит выдавать врачу случайную смесь. Словарь взят из LOM
// 9.1 purpose и закрыт.
type Purpose string

const (
	PurposeTopic      Purpose = "topic"
	PurposeSystem     Purpose = "system"
	PurposeDiscipline Purpose = "discipline"
	PurposeTask       Purpose = "task"
	PurposeLevel      Purpose = "level"
	PurposeLegal      Purpose = "legal"
	PurposeOther      Purpose = "other"
)

// Hierarchy — смысл вложенности.
//
// Объявляется, а не подразумевается: раздел классификации и пункт приказа
// вложены по-разному. «F32.1 — это разновидность F32» и «пункт 3.2 — часть
// пункта 3» выглядят в базе одинаково, а значат разное, и подбор, считающий
// их одним, молча беднеет. Словарь взят из FHIR hierarchyMeaning.
type Hierarchy string

const (
	HierarchyIsA     Hierarchy = "is-a"
	HierarchyPartOf  Hierarchy = "part-of"
	HierarchyGrouped Hierarchy = "grouped"
)

// Source — паспорт источника.
//
// UnitWord и StatementWord — словарь интерфейса: как звать единицу и
// положение у этого источника. Пустыми не бывают: интерфейс без слова
// показал бы «единица» врачу, который ждёт слова «диагноз» или «пункт».
type Source struct {
	Slug          string
	Kind          Kind
	Title         string
	UnitWord      string
	StatementWord string
	Purpose       Purpose
	Hierarchy     Hierarchy
	Completeness  Completeness
	Edition       string

	// ID и Status заполняются при чтении из базы; при заведении не
	// спрашиваются: номер выдаёт база, а состояние нового источника —
	// всегда черновик.
	ID     int64
	Status string
}

// Unit — единица источника: диагноз, пункт приказа, раздел рекомендаций.
//
// Path и Depth не задаются снаружи — их считает BuildPaths по ParentLabel.
// В этом весь смысл устройства: «раздел» и «рубрика» становятся срезом
// пути, а не отрезанием знаков от метки. Для МКБ результат тот же, но
// получен данными, а не догадкой о формате кода.
type Unit struct {
	Label       string
	ParentLabel string
	Title       string

	// Kind — род записи: 'group' (вход в навигацию) или 'entry' (то, по
	// чему спрашивают). Пусто означает 'entry': источник, ничего не
	// сказавший о роде, состоит из записей.
	Kind string

	Answerable bool
	Ord        int

	Path  string
	Depth int
}

// Statement — положение единицы: текст, на который ссылается разметка
// задачи.
//
// Designation — обозначение в первоисточнике («G1», «абз. 2»), PlaceRef —
// ссылка на место (страница, пункт). У документа, где их нет, остаются
// пустыми, а не выдуманными: выдуманная ссылка на место хуже отсутствующей,
// потому что по ней пойдут проверять.
type Statement struct {
	UnitLabel   string
	Kind        string
	Designation string
	Body        string
	PlaceRef    string
	Ord         int
}

// Fragment — кусок документа, каким его вынул разбор.
//
// Level и Title взяты из заголовка, под которым кусок стоит; у документа
// без заголовков весь текст — один кусок нулевого уровня. CharFrom и
// CharTo — место в исходном тексте: по ним человек находит кусок в
// оригинале, когда разбор ошибся.
type Fragment struct {
	Level    int
	Title    string
	Body     string
	CharFrom int
	CharTo   int
}
