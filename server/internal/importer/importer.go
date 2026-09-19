// Ввоз из прежней системы.
//
// # Зачем он вообще
//
// В прежней базе лежит справочник МКБ-10 и задачи, написанные по нему
// врачами за годы. Написать их заново нельзя ни за какие деньги, и это
// единственная причина, по которой ввоз существует.
//
// # Что НЕ ввозится, и почему это решение
//
// Попытки обучающихся. Они принадлежат людям, которых в новой базе не
// будет: учётные записи не переносятся, и попытка без своего хозяина —
// это число, приписанное никому. Из попыток нужна ровно решаемость, и она
// ввозится СВОДКОЙ по задаче.
//
// Ввезённая решаемость помечается `origin = 'imported'` и не смешивается с
// нашей. Она мерила другую аудиторию на другом приложении, и сложить её с
// нашей значит получить среднее по двум разным вещам — число, которое
// выглядит точным и таковым не является.
//
// Организации, квоты, счета, теория. Их в Кураторе нет вовсе.
//
// # Ввоз идёт по одному источнику
//
// Не «всё сразу»: источник — это дерево, задачи ссылаются на его единицы,
// и половина дерева означает задачи, висящие в пустоте. За один заход
// ввозится один источник целиком, и МКБ-10 идёт первым — заодно это
// первая проверка универсальной модели: ложится ли справочник в неё без
// исключений.
//
// # Повторный ввоз безопасен
//
// Обрыв посреди ввоза — обычное дело: база чужая, сеть между ними тоже.
// Поэтому всё, что ввоз пишет, пишется с разрешением конфликта, и
// повторный заход досчитывает недостающее, а не задваивает сделанное.
package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Rows — то немногое, что ввозу нужно от базы.
//
// Свой узкий вид, а не *pgxpool.Pool: прежняя база читается отдельным
// соединением и только на чтение, и давать ввозу больше, чем «спросить»,
// незачем.
type Rows interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Profile — то, чего в прежней базе не было и что выдумать нельзя.
//
// Куратор спрашивает у источника три вещи, которых прежняя схема не
// знала: по какой оси он классифицирует, что означает вложенность и полон
// ли он. От них зависит подбор задач и расчёт охвата, и умолчание здесь
// означало бы тихо неверные доли. Поэтому ввоз их ТРЕБУЕТ: назвать их
// должен человек, знающий источник.
type Profile struct {
	Purpose      string
	Hierarchy    string
	Completeness string
}

// Valid проверяет, что свойства названы и названы допустимым.
func (p Profile) Valid() error {
	purposes := map[string]bool{"topic": true, "system": true,
		"discipline": true, "task": true, "level": true, "legal": true, "other": true}
	hierarchies := map[string]bool{"part-of": true, "is-a": true, "grouped": true}
	completeness := map[string]bool{"complete": true, "fragment": true}

	if !purposes[p.Purpose] {
		return fmt.Errorf("ось классификации источника не названа или неизвестна: %q", p.Purpose)
	}
	if !hierarchies[p.Hierarchy] {
		return fmt.Errorf("смысл вложенности не назван или неизвестен: %q", p.Hierarchy)
	}
	if !completeness[p.Completeness] {
		return fmt.Errorf("полнота источника не названа или неизвестна: %q", p.Completeness)
	}
	return nil
}

// Report — что сделал ввоз.
//
// Считается всё, включая пропущенное: молча пропущенная половина задач
// выглядит как «их столько и было», и заметить это можно только сверив
// числа руками через месяц.
type Report struct {
	SourceID   int64
	Units      int
	Statements int
	Cases      int
	Stats      int

	// Skipped — задачи, которые не перевелись, с причиной у каждой.
	// Причина нужна не для отчётности: по ней видно, чего модель не
	// знает, а по числу без причины не видно ничего.
	Skipped map[string]string
}

// Import ввозит один источник со всем, что к нему относится.
// adoptUnassigned объясняется у cases: это выбор человека, а не умолчание.
func Import(ctx context.Context, old, fresh Rows, slug string, profile Profile,
	adoptUnassigned bool, now time.Time) (Report, error) {

	out := Report{Skipped: map[string]string{}}
	if err := profile.Valid(); err != nil {
		return out, err
	}

	sourceID, oldSourceID, err := source(ctx, old, fresh, slug, profile)
	if err != nil {
		return out, err
	}
	out.SourceID = sourceID

	if out.Units, err = units(ctx, old, fresh, oldSourceID, sourceID); err != nil {
		return out, err
	}
	if out.Statements, err = statements(ctx, old, fresh, oldSourceID, sourceID); err != nil {
		return out, err
	}
	if err := cases(ctx, old, fresh, oldSourceID, sourceID, adoptUnassigned, now, &out); err != nil {
		return out, err
	}
	if out.Stats, err = stats(ctx, old, fresh, now); err != nil {
		return out, err
	}
	return out, nil
}

// source переносит сам источник и возвращает его номера: новый и прежний.
func source(ctx context.Context, old, fresh Rows, slug string, profile Profile) (int64, int64, error) {
	var (
		oldID                         int64
		kind, title, unitWord, stWord string
		edition, issued, url, legal   string
		status                        string
		rawProfile                    []byte
	)
	err := old.QueryRow(ctx, `
		SELECT id, kind, title, unit_word, statement_word,
		       edition, issued, url, legal_note, status, profile
		  FROM sources WHERE slug = $1`, slug).
		Scan(&oldID, &kind, &title, &unitWord, &stWord,
			&edition, &issued, &url, &legal, &status, &rawProfile)
	if err != nil {
		return 0, 0, fmt.Errorf("источник %q в прежней базе не найден: %w", slug, err)
	}
	if len(rawProfile) == 0 {
		rawProfile = []byte("{}")
	}

	var newID int64
	err = fresh.QueryRow(ctx, `
		INSERT INTO sources (slug, kind, title, unit_word, statement_word,
		                     purpose, hierarchy, completeness,
		                     edition, issued, url, legal_note, status, profile)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (slug) DO UPDATE SET title = EXCLUDED.title
		RETURNING id`,
		slug, kind, title, unitWord, stWord,
		profile.Purpose, profile.Hierarchy, profile.Completeness,
		edition, issued, url, legal, status, rawProfile).Scan(&newID)
	if err != nil {
		return 0, 0, fmt.Errorf("источник не записан: %w", err)
	}
	return newID, oldID, nil
}

// unit — единица источника, какой её читает ввоз.
type unit struct {
	kind       string
	label      string
	parent     string
	title      string
	answerable bool
	ord        int
	attrs      []byte
}

// units переносит единицы и СЧИТАЕТ путь.
//
// Путь считается из связи с родителем, а не из формата метки. Это и есть
// то место, ради которого вся затея с универсальным источником и
// нужна: соблазн отрезать знаки от кода МКБ («F32.1» → «F32» → «F3»)
// здесь особенно силён, потому что для МКБ он сработал бы. Он сработал бы
// ровно для одного источника на свете, и каждый следующий пришлось бы
// снабжать своей веткой кода.
func units(ctx context.Context, old, fresh Rows, oldSourceID, sourceID int64) (int, error) {
	rows, err := old.Query(ctx, `
		SELECT kind, label, parent_label, title, answerable, ord, attrs
		  FROM source_units WHERE source_id = $1 ORDER BY kind, ord, label`,
		oldSourceID)
	if err != nil {
		return 0, fmt.Errorf("единицы прежнего источника не прочитаны: %w", err)
	}
	defer rows.Close()

	list := []unit{}
	for rows.Next() {
		var one unit
		if err := rows.Scan(&one.kind, &one.label, &one.parent, &one.title,
			&one.answerable, &one.ord, &one.attrs); err != nil {
			return 0, err
		}
		list = append(list, one)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	paths := Paths(list)
	taken := 0
	for _, one := range list {
		path, ok := paths[one.label]
		if !ok {
			// Метка с разделителем внутри или оборванная цепочка родителей:
			// путь посчитать нечем. Единица не ввозится — путь, собранный
			// наугад, увёл бы задачу в чужой раздел, и заметить это можно
			// было бы только открыв раздел.
			continue
		}
		if len(one.attrs) == 0 {
			one.attrs = []byte("{}")
		}
		// Считается ЗАПИСАННОЕ, а не попытки записать. Разница видна
		// только на повторном заходе — и там отчёт о «пяти ввезённых
		// единицах» при пяти уже лежащих означал бы, что ввоз задвоил
		// дерево. Он не задвоил, но по отчёту этого не отличить.
		//
		// Ключ — метка без рода: в прежней базе он стоял вместе с родом, и
		// «F00» как группа уживалась с «F00» как записью. Чтение по метке
		// отдавало бы тогда то одну, то другую. Если в доноре такая пара
		// всё же есть, вторая единица не ляжет, и ввоз назовёт её числом —
		// прочитано столько, записано меньше.
		tag, err := fresh.Exec(ctx, `
			INSERT INTO source_units (source_id, kind, label, parent_label, title,
			                          path, depth, answerable, ord, attrs)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (source_id, label) DO NOTHING`,
			sourceID, one.kind, one.label, one.parent, one.title,
			path, depthOf(path), one.answerable, one.ord, one.attrs)
		if err != nil {
			return taken, fmt.Errorf("единица %q не записана: %w", one.label, err)
		}
		if tag.RowsAffected() > 0 {
			taken++
		}
	}
	return taken, nil
}

// Paths считает путь каждой единицы по цепочке родителей.
//
// Разделитель '/' не встречается в метках справочников; метка с ним
// внутри пути не получает вовсе — схема такую метку и не примет, и молча
// испорченный путь хуже отсутствующего.
//
// Цикл в родителях (метка, оказавшаяся собственным предком) обрывает
// расчёт для этой ветки. Данные чужие, и доверять их связности нельзя:
// без обрыва расчёт ушёл бы в бесконечность на одной битой строке.
func Paths(list []unit) map[string]string {
	parents := make(map[string]string, len(list))
	for _, one := range list {
		if strings.ContainsRune(one.label, '/') {
			continue
		}
		parents[one.label] = one.parent
	}

	out := make(map[string]string, len(list))
	for label := range parents {
		chain := []string{}
		seen := map[string]bool{}
		at := label
		broken := false
		for at != "" {
			if seen[at] {
				broken = true
				break
			}
			seen[at] = true
			parent, ok := parents[at]
			if !ok {
				// Родитель назван, но своей строки в перечне не имеет.
				// Он всё равно входит в путь, и цепочка обрывается на
				// нём: выкинуть его значило бы поднять единицу в корень и
				// соврать о дереве — срез «всё, что под F3» перестал бы
				// её находить. Выгрузка может не содержать корень, и это
				// не отказ.
				chain = append(chain, at)
				break
			}
			chain = append(chain, at)
			at = parent
		}
		if broken {
			continue
		}
		// Цепочка собрана снизу вверх — переворачиваем на месте.
		for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
			chain[i], chain[j] = chain[j], chain[i]
		}
		out[label] = strings.Join(chain, "/")
	}
	return out
}

// depthOf — длина пути в шагах. Пустой путь — нулевая глубина.
func depthOf(path string) int {
	if path == "" {
		return 0
	}
	return strings.Count(path, "/") + 1
}

// statements переносит положения единиц.
func statements(ctx context.Context, old, fresh Rows, oldSourceID, sourceID int64) (int, error) {
	rows, err := old.Query(ctx, `
		SELECT unit_label, kind, designation, body_md, place_ref, ord
		  FROM source_unit_statements WHERE source_id = $1
		 ORDER BY unit_label, ord`, oldSourceID)
	if err != nil {
		return 0, fmt.Errorf("положения прежнего источника не прочитаны: %w", err)
	}
	defer rows.Close()

	type row struct {
		label, kind, designation, body, place string
		ord                                   int
	}
	list := []row{}
	for rows.Next() {
		var one row
		if err := rows.Scan(&one.label, &one.kind, &one.designation,
			&one.body, &one.place, &one.ord); err != nil {
			return 0, err
		}
		list = append(list, one)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	taken := 0
	for _, one := range list {
		// Своего ключа у положения нет ни там, ни здесь, поэтому
		// повторность ловится самим набором полей: второй ввоз того же
		// положения не должен его задваивать.
		var exists bool
		if err := fresh.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM source_unit_statements
			                WHERE source_id = $1 AND unit_label = $2 AND ord = $3
			                  AND body_md = $4)`,
			sourceID, one.label, one.ord, one.body).Scan(&exists); err != nil {
			return taken, err
		}
		if exists {
			continue
		}
		if _, err := fresh.Exec(ctx, `
			INSERT INTO source_unit_statements
			       (source_id, unit_label, kind, designation, body_md, place_ref, ord)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			sourceID, one.label, one.kind, one.designation,
			one.body, one.place, one.ord); err != nil {
			return taken, fmt.Errorf("положение единицы %q не записано: %w", one.label, err)
		}
		taken++
	}
	return taken, nil
}

// cases переносит задачи, переводя тело каждой.
//
// Ввозятся только те, что были на раздаче или готовы к ней: черновик
// прежней системы написан под прежнюю модель, и ввозить его значит
// ввозить работу, которую всё равно придётся переделывать.
//
// # Удалённое не ввозится
//
// В прежней системе удаление задачи обратимо: она проставляет deleted_at
// и лежит в «Корзине». Ввезти такую значит вернуть к жизни то, что врач
// выбросил, — причём молча и без «Корзины», из которой её можно было бы
// выбросить снова.
//
// # Чужие источники не ввозятся
//
// Задача принадлежит источнику колонкой. Ввоз идёт по одному источнику, и
// брать чужие задачи значит вешать их на единицы, которых у них нет.
//
// # adoptUnassigned — про задачи БЕЗ источника, и это выбор человека
//
// В прежней базе есть задачи старше самого понятия источника: они
// писались, когда справочник был один и встроен в код. У них source_id
// пуст. Приписать их ввозимому источнику можно — но только назвав это
// вслух, потому что верно это ровно для одного источника и ровно один
// раз. Умолчание здесь — не приписывать: молчаливое присвоение чужих
// задач источнику заметить потом нечем.
func cases(ctx context.Context, old, fresh Rows, oldSourceID, sourceID int64,
	adoptUnassigned bool, now time.Time, out *Report) error {

	where := "source_id = $1"
	if adoptUnassigned {
		where = "(source_id = $1 OR source_id IS NULL)"
	}
	rows, err := old.Query(ctx, `
		SELECT id, status, origin, body, created_at, published_at
		  FROM cases
		 WHERE deleted_at IS NULL AND status IN ('published', 'review')
		   AND `+where+`
		 ORDER BY id`, oldSourceID)
	if err != nil {
		return fmt.Errorf("задачи прежней базы не прочитаны: %w", err)
	}
	defer rows.Close()

	type row struct {
		id, status, origin string
		body               []byte
		createdAt          time.Time
		publishedAt        *time.Time
	}
	list := []row{}
	for rows.Next() {
		var one row
		if err := rows.Scan(&one.id, &one.status, &one.origin, &one.body,
			&one.createdAt, &one.publishedAt); err != nil {
			return err
		}
		list = append(list, one)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, one := range list {
		translated, err := Translate(one.body)
		if err != nil {
			out.Skipped[one.id] = err.Error()
			continue
		}
		// Путь берётся из УЖЕ ввезённого дерева, а не считается снова:
		// два расчёта одного пути разошлись бы молча, и задача уехала бы
		// в раздел, которого у неё нет.
		var path string
		if err := fresh.QueryRow(ctx, `
			SELECT path FROM source_units
			 WHERE source_id = $1 AND label = $2 AND kind = 'entry'`,
			sourceID, translated.UnitLabel).Scan(&path); err != nil {
			out.Skipped[one.id] = fmt.Sprintf(
				"единицы %q нет во ввезённом источнике", translated.UnitLabel)
			continue
		}

		body, err := json.Marshal(translated.Body)
		if err != nil {
			out.Skipped[one.id] = err.Error()
			continue
		}
		tag, err := fresh.Exec(ctx, `
			INSERT INTO cases (id, source_id, unit_label, unit_path, status,
			                   revision, origin, body, created_at, updated_at, published_at)
			VALUES ($1,$2,$3,$4,$5,1,$6,$7,$8,$9,$10)
			ON CONFLICT (id) DO NOTHING`,
			one.id, sourceID, translated.UnitLabel, path, one.status,
			one.origin, body, one.createdAt, now, one.publishedAt)
		if err != nil {
			return fmt.Errorf("задача %q не записана: %w", one.id, err)
		}
		// Считается записанное: задача, уже лежащая от прошлого захода,
		// не ввезена этим — и отчёт обязан это различать.
		if tag.RowsAffected() > 0 {
			out.Cases++
		}
	}
	return nil
}

// stats переносит решаемость СВОДКОЙ.
//
// Не попытками: попытки принадлежат людям, которых в новой базе не будет.
// Из них нужна ровно доля решивших, и она ввозится помеченной чужой —
// `origin = 'imported'`. Смешать её с нашей значит получить среднее по
// двум разным аудиториям на двух разных приложениях.
//
// Медиана времени ввозится только та, что мерила решение: у прежней базы
// их три (решение, чтение, разбор), и складывать их незачем — сложенная
// медиана не медиана ничего.
func stats(ctx context.Context, old, fresh Rows, now time.Time) (int, error) {
	rows, err := old.Query(ctx, `
		SELECT case_id, attempts, correct, COALESCE(median_solve_ms, 0)
		  FROM rollup_case_stats ORDER BY case_id`)
	if err != nil {
		return 0, fmt.Errorf("решаемость прежней базы не прочитана: %w", err)
	}
	defer rows.Close()

	type row struct {
		id                        string
		attempts, correct, median int64
	}
	list := []row{}
	for rows.Next() {
		var one row
		if err := rows.Scan(&one.id, &one.attempts, &one.correct, &one.median); err != nil {
			return 0, err
		}
		list = append(list, one)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	taken := 0
	for _, one := range list {
		if one.attempts <= 0 {
			// Ноль попыток — это отсутствие сведений, а не решаемость
			// ноль. Доля, посчитанная из нуля, выглядела бы как
			// «задачу не решил никто».
			continue
		}
		rate := float64(one.correct) / float64(one.attempts)
		tag, err := fresh.Exec(ctx, `
			INSERT INTO case_stats (case_id, attempts, correct, solve_rate,
			                        median_ms, confusion, origin, updated_at)
			SELECT $1,$2,$3,$4,$5,'{}'::jsonb,'imported',$6
			 WHERE EXISTS (SELECT 1 FROM cases WHERE id = $1)
			ON CONFLICT (case_id) DO NOTHING`,
			one.id, one.attempts, one.correct, rate, one.median, now)
		if err != nil {
			return taken, fmt.Errorf("решаемость задачи %q не записана: %w", one.id, err)
		}
		if tag.RowsAffected() > 0 {
			taken++
		}
	}
	return taken, nil
}

// Узкие виды строк и результата: ввоз не тащит на себе весь pgx.
type pgxRows = pgx.Rows
type pgxRow = pgx.Row
type commandTag = pgconn.CommandTag
