// Генерация задач: заказ, план и очередь заданий.
//
// Заказ называет источник и единицу, а всё остальное — словарь, положения,
// круг различения — конвейер берёт из ДАННЫХ источника. Условия «если это
// МКБ» здесь нет и быть не должно: источник любой, и то, чем он отличается
// от соседа, он рассказывает о себе сам.
//
// # Контекст разрешается ПРИ ЗАКАЗЕ
//
// План заказа собирается один раз, в момент заказа, и целиком уезжает в
// задание. Составитель заказал задачу по тому, что видел, и выполняется
// заказанное, а не то, чем источник стал к моменту, когда очередь дошла до
// задания. Заодно это делает задание возобновляемым после перезапуска без
// второго похода в хранилище — а второе чтение разошлось бы с первым
// молча: положение правят между заказом и сверкой, и сверка начала бы
// отвергать исправное.
package gen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"curator/server/internal/casestore"
	"curator/server/internal/dbgate"
)

// Вид задачи. Словарь закрыт, и закрытость эта не строгость ради строгости.
//
// У донора заказ по разделу клинических рекомендаций «Лечение» вернулся
// отказом модели: «эталон не является диагнозом, виньетку для постановки
// диагноза не составить». Отказ был верен — спрашивали «что это» у
// раздела, который отвечает «что делать». Третьего вопроса мы задавать не
// умеем, и вид «на всякий случай» означал бы задание, не умеющее ни того
// ни другого.
//
// Сами имена живут у модели (internal/casestore) и сюда приходят
// ссылкой: вид читают трое — заказ, публикация и устройство, — и
// написанный дважды словарь расходится молча. Здесь остаётся довод, там
// лежат имена.
const (
	// KindRecognise — узнай единицу по описанию: эталон — единица,
	// варианты — её соседи.
	KindRecognise = casestore.KindRecognise

	// KindAction — выбери верное действие: эталон — ПОЛОЖЕНИЕ единицы,
	// варианты — соседние положения этой единицы и положения соседних
	// единиц по пути.
	KindAction = casestore.KindAction
)

// KindWord — вид задачи словами отказа.
//
// Отказ читает составитель, и «action» ему ничего не говорит: чинить он
// будет выбор в форме, а не словарь схемы.
func KindWord(kind string) string {
	if kind == KindAction {
		return "выбор верного действия"
	}
	return "узнавание единицы по описанию"
}

// normalizeKind — вид из заказа.
//
// Незнакомое слово — отказ, а не отступление к умолчанию: непонятое не
// применяется, а молча понятый как узнавание заказ действия написал бы
// задачу не про то.
func normalizeKind(kind string) (string, error) {
	switch strings.TrimSpace(kind) {
	case "", KindRecognise:
		return KindRecognise, nil
	case KindAction:
		return KindAction, nil
	default:
		return "", fmt.Errorf("вид задачи %q неизвестен: бывает узнавание единицы по описанию или выбор верного действия", kind)
	}
}

// Order — заказ, каким его делает составитель.
type Order struct {
	SourceID  int64  `json:"sourceId"`
	UnitLabel string `json:"unitLabel"`
	Kind      string `json:"kind"`

	// TargetStatement — обозначение положения, заказанного эталоном у
	// задачи-действия. Пусто — эталоном идёт первое положение единицы: они
	// стоят в порядке документа, и первое есть головное указание раздела.
	//
	// Выбирает его ЗАКАЗ, а не модель: отдай выбор модели, и задача
	// ответит не на тот вопрос, который заказывали.
	TargetStatement string `json:"targetStatement,omitempty"`

	// Model — просьба написать этой моделью. Пусто — моделью узла.
	Model string `json:"model,omitempty"`
}

// UnitRef — единица источника, как её видит заказ.
type UnitRef struct {
	Label string `json:"label"`
	Title string `json:"title"`

	// Path — путь единицы от корня («3/3.1»). Нужен своду правил: правило
	// про раздел обязано доставаться его пунктам, иначе область пришлось
	// бы перечислять поимённо и переписывать при каждом пополнении
	// источника. У соседей не заполняется: круг различения берётся
	// метками.
	//
	// Поле необязательное, и у заданий, заказанных до его появления, оно
	// пусто. Сверка области это знает и падает обратно на метку — иначе
	// правило про источник перестало бы действовать на его старых
	// заданиях, и объяснить это было бы нечем.
	Path string `json:"path,omitempty"`

	// StatementsMd — положения соседа. Заполняются только у кандидатов
	// неверных вариантов и только ради различающей сверки: условие готовой
	// задачи прогоняется и против положений каждого неверного варианта,
	// чтобы поймать двойника — соседа, которого условие подтверждает не
	// хуже эталона.
	StatementsMd string `json:"statementsMd,omitempty"`
}

// StatementRef — положение единицы.
type StatementRef struct {
	Designation string `json:"designation,omitempty"`
	Body        string `json:"body"`
	PlaceRef    string `json:"placeRef,omitempty"`
}

// Plan — разрешённый контекст заказа: всё, что конвейеру нужно знать,
// чтобы собрать задание без второго чтения.
type Plan struct {
	SourceID int64  `json:"sourceId"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`

	// UnitWord и StatementWord — словарь источника: как он зовёт единицу и
	// положение. Идут в задание и в подписи, а не «диагноз» и «критерий»
	// по привычке первого источника.
	UnitWord      string `json:"unitWord"`
	StatementWord string `json:"statementWord"`

	// Hierarchy — смысл вложенности источника. От него зависит, кого
	// считать соседом: у «часть целого» сосед по родителю — другая часть
	// того же целого, у «разновидности» — другая разновидность того же
	// рода. Задание говорит об этом модели словами источника, а не
	// подразумевает одно устройство для всех.
	Hierarchy string `json:"hierarchy"`

	TaskKind string `json:"taskKind"`

	Unit UnitRef `json:"unit"`

	// Statements — положения единицы, как они прочитаны при заказе.
	// StatementsMd — те же положения, собранные в текст задания.
	//
	// Оба поля собираются ОДИН раз, при заказе, из одного чтения, и тем же
	// текстом уезжают и в задание модели, и в слепую сверку: два чтения
	// разошлись бы молча. Разбирать текст обратно в обозначения нельзя по
	// той же причине — это была бы вторая реализация сборки, и расходилась
	// бы она с первой тихо.
	Statements   []StatementRef `json:"statements"`
	StatementsMd string         `json:"statementsMd"`

	// Target — положение, заказанное эталоном у задачи-действия. У задачи
	// на узнавание пусто.
	Target *StatementRef `json:"target,omitempty"`

	// Siblings — соседние единицы источника: кандидаты неверных вариантов.
	// Круг различения у чужого источника не выводится ниоткуда, кроме его
	// собственных единиц.
	Siblings []UnitRef `json:"siblings"`

	// Model — модель, названная в заказе. Пусто — моделью узла.
	Model string `json:"model,omitempty"`
}

// IdemKey — ключ повторности заказа.
//
// Повтор того же заказа не заводит второго задания: составитель, нажавший
// «заказать» дважды, хотел одну задачу, а не две, и платить за вторую
// незачем. Ключ считается от заказанного, а не от плана: план зависит от
// состояния источника, и правка соседней единицы завела бы второе задание
// на тот же заказ.
func (o Order) IdemKey() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s",
		o.SourceID, o.UnitLabel, o.Kind, o.TargetStatement)))
	return hex.EncodeToString(sum[:16])
}

// Resolver собирает план заказа из источника.
type Resolver struct {
	gate *dbgate.Gate
}

func NewResolver(gate *dbgate.Gate) *Resolver { return &Resolver{gate: gate} }

// Resolve разрешает заказ в план.
//
// Отказывает, а не додумывает: заказ по группе, по единице в карантине или
// по единице без положений выполнить нечем, и молча собранное задание
// написало бы задачу ни о чём — заметили бы это не при заказе, а у врача.
func (r *Resolver) Resolve(ctx context.Context, order Order) (Plan, error) {
	kind, err := normalizeKind(order.Kind)
	if err != nil {
		return Plan{}, err
	}
	label := strings.TrimSpace(order.UnitLabel)
	if label == "" {
		return Plan{}, errors.New("в заказе не названа единица источника")
	}

	plan := Plan{SourceID: order.SourceID, TaskKind: kind, Model: strings.TrimSpace(order.Model)}
	err = r.gate.QueryRow(ctx,
		`SELECT slug, title, kind, unit_word, statement_word, hierarchy
		   FROM sources WHERE id = $1`, order.SourceID).
		Scan(&plan.Slug, &plan.Title, &plan.Kind, &plan.UnitWord, &plan.StatementWord, &plan.Hierarchy)
	if err != nil {
		return Plan{}, fmt.Errorf("источник %d не найден: %w", order.SourceID, err)
	}

	var parentLabel string
	var answerable bool
	// Путь читается здесь же, одним запросом: по нему свод правил решает,
	// достаётся ли задаче правило, написанное про вышестоящий раздел.
	// Собрать путь из метки обратно нельзя — это была бы вторая
	// реализация вложенности, и расходилась бы она с первой молча.
	err = r.gate.QueryRow(ctx,
		`SELECT title, parent_label, answerable, path
		   FROM source_units
		  WHERE source_id = $1 AND label = $2 AND kind = 'entry'`,
		order.SourceID, label).Scan(&plan.Unit.Title, &parentLabel, &answerable, &plan.Unit.Path)
	if err != nil {
		return Plan{}, fmt.Errorf("в источнике нет такой единицы: %s", label)
	}
	plan.Unit.Label = label
	if !answerable {
		// Группа — тоже единица, только к ответу не пригодная. Задача по
		// ней спрашивала бы то, на что ответа нет.
		return Plan{}, fmt.Errorf("%s %q к ответу не пригоден: это группа, а не запись",
			plan.UnitWord, label)
	}

	var quarantine string
	err = r.gate.QueryRow(ctx,
		`SELECT reason FROM source_unit_quarantine WHERE source_id = $1 AND unit_label = $2`,
		order.SourceID, label).Scan(&quarantine)
	if err == nil {
		// Карантин — редакционное решение «по этой единице писать нельзя».
		// Обойти его молча значит выпустить задачу, которую уже признали
		// негодной.
		return Plan{}, fmt.Errorf("по этой единице писать нельзя: %s", quarantine)
	}

	statements, err := r.statements(ctx, order.SourceID, label)
	if err != nil {
		return Plan{}, err
	}
	if len(statements) == 0 {
		return Plan{}, fmt.Errorf("у единицы %q нет ни одного положения: писать не по чему", label)
	}
	plan.Statements = statements
	plan.StatementsMd = statementsMarkdown(statements)

	if kind == KindAction {
		target, err := pickTarget(statements, order.TargetStatement, plan.StatementWord)
		if err != nil {
			return Plan{}, err
		}
		plan.Target = &target
	}

	plan.Siblings, err = r.siblings(ctx, order.SourceID, label, parentLabel)
	if err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// statements — положения единицы по порядку документа.
func (r *Resolver) statements(ctx context.Context, sourceID int64, label string) ([]StatementRef, error) {
	rows, err := r.gate.Query(ctx,
		`SELECT designation, body_md, place_ref
		   FROM source_unit_statements
		  WHERE source_id = $1 AND unit_label = $2
		  ORDER BY ord, id`, sourceID, label)
	if err != nil {
		return nil, fmt.Errorf("положения единицы %q не прочитаны: %w", label, err)
	}
	defer rows.Close()

	out := []StatementRef{}
	for rows.Next() {
		var st StatementRef
		if err := rows.Scan(&st.Designation, &st.Body, &st.PlaceRef); err != nil {
			return nil, fmt.Errorf("строка положения не разобрана: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("положения дочитаны не до конца: %w", err)
	}
	return out, nil
}

// pickTarget — какое положение идёт эталоном у задачи-действия.
func pickTarget(statements []StatementRef, designation, statementWord string) (StatementRef, error) {
	designation = strings.TrimSpace(designation)
	if designation == "" {
		// Положения стоят в порядке документа, и первое есть головное
		// указание раздела.
		return statements[0], nil
	}
	for _, st := range statements {
		if strings.EqualFold(strings.TrimSpace(st.Designation), designation) {
			return st, nil
		}
	}
	// Отказ, а не «возьмём первое»: заказали одно положение, а задача
	// вышла бы про другое, и увидел бы это только тот, кто заказывал.
	return StatementRef{}, fmt.Errorf("%s %q у этой единицы нет", statementWord, designation)
}

// siblings — соседние единицы: кандидаты неверных вариантов.
//
// Соседство берётся из данных источника, а не из формата метки: у единицы
// с родителем соседи — другие дети того же родителя, у корневой — другие
// корневые. К ним добавляются пары «путают с», если источник их назвал:
// это единственное место, где сходство двух единиц названо прямо, и
// пренебрегать им значит подбирать неверные варианты хуже, чем мог бы сам
// источник.
func (r *Resolver) siblings(ctx context.Context, sourceID int64, label, parentLabel string) ([]UnitRef, error) {
	rows, err := r.gate.Query(ctx,
		`SELECT u.label, u.title,
		        COALESCE(string_agg(s.body_md, E'\n' ORDER BY s.ord, s.id), '')
		   FROM source_units u
		   LEFT JOIN source_unit_statements s
		          ON s.source_id = u.source_id AND s.unit_label = u.label
		  WHERE u.source_id = $1
		    AND u.kind = 'entry'
		    AND u.answerable
		    AND u.label <> $2
		    AND (u.parent_label = $3
		         OR u.label IN (SELECT counterpart FROM source_unit_differentials
		                         WHERE source_id = $1 AND unit_label = $2))
		    AND u.label NOT IN (SELECT unit_label FROM source_unit_quarantine
		                         WHERE source_id = $1)
		  GROUP BY u.id, u.label, u.title
		  ORDER BY u.ord, u.label`,
		sourceID, label, parentLabel)
	if err != nil {
		return nil, fmt.Errorf("соседние единицы не прочитаны: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: он уедет в план задания, а оттуда в
	// ответ ручки, и null вместо списка роняет студию на исправном
	// случае — на единице без соседей.
	out := []UnitRef{}
	for rows.Next() {
		var u UnitRef
		if err := rows.Scan(&u.Label, &u.Title, &u.StatementsMd); err != nil {
			return nil, fmt.Errorf("строка соседа не разобрана: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("соседи дочитаны не до конца: %w", err)
	}
	return out, nil
}

func statementsMarkdown(statements []StatementRef) string {
	names := Plan{Statements: statements}.Designations()
	var b strings.Builder
	for i, st := range statements {
		if i > 0 {
			b.WriteString("\n")
		}
		// Обозначение и ссылка на место идут вместе с телом: по ним
		// составитель находит положение в первоисточнике, когда спорит с
		// задачей. Выдуманных здесь не появляется — у документа, где их
		// нет, стоит номер по списку.
		b.WriteString("- ")
		b.WriteString(names[i])
		b.WriteString(". ")
		b.WriteString(st.Body)
		if st.PlaceRef != "" {
			b.WriteString(" (")
			b.WriteString(st.PlaceRef)
			b.WriteString(")")
		}
	}
	return b.String()
}

// Designations — обозначения положений, какими на них ссылается разметка.
//
// Обозначение может быть пустым — у документа, где положения не подписаны.
// Тогда ссылка идёт номером по списку, и номер этот считается здесь, один
// раз: модель, выдумавшая нумерацию сама, разметила бы задачу по своему
// счёту, а не по документу.
func (p Plan) Designations() []string {
	out := make([]string, 0, len(p.Statements))
	for i, st := range p.Statements {
		if d := strings.TrimSpace(st.Designation); d != "" {
			out = append(out, d)
			continue
		}
		out = append(out, fmt.Sprintf("%d", i+1))
	}
	return out
}

// statementsMarkdown собирает положения в текст задания.
//
// Каждое положение названо тем же обозначением, каким на него будет
// ссылаться разметка (см. Designations): модель должна видеть в задании
// ровно те имена, которыми ей разрешено ссылаться.

// Blinded — план, из которого убрано всё, чего слепая сверка знать не
// должна: положения источника, заказанная единица с её названием,
// эталонное положение и положения соседей.
//
// Остаётся то, что сверке нужно и что подсказкой не является: название
// источника, его словарь и смысл вложенности — этим она пишет свой ответ
// теми же словами, какими задача названа на экране.
//
// Слепота держится ЗДЕСЬ, а не в тексте задания: задание правит
// составитель, и вписанное им {положения} превратило бы сверку в
// самоподтверждение, выглядящее работой.
func (p Plan) Blinded() Plan {
	blind := Plan{
		SourceID:      p.SourceID,
		Slug:          p.Slug,
		Title:         p.Title,
		Kind:          p.Kind,
		UnitWord:      p.UnitWord,
		StatementWord: p.StatementWord,
		Hierarchy:     p.Hierarchy,
		TaskKind:      p.TaskKind,
		Model:         p.Model,
		Statements:    []StatementRef{},
		Siblings:      []UnitRef{},
	}
	return blind
}
