package gen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Очередь заданий генерации.
//
// Задание живёт отдельно от задачи, которую оно породит: задание — это
// сведения о том, КАК задачу получили, а текст и замечания — сведения о
// ней самой. Смешать их значит потерять и то и другое при первой же
// перегенерации.

// Состояния задания. Словарь закрыт: состояние, появившееся строкой по
// месту, не попадёт ни в один отбор — то есть задание зависнет, и заметят
// это не сразу.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Роды работы. Очередь одна, исполнители разные, и отбор идёт по роду:
// исполнитель, взявший чужое задание, прочёл бы чужой заказ и выполнил не
// то. Словарь закрыт по той же причине, что и словарь состояний.
const (
	// KindCase — написать задачу по единице источника.
	KindCase = "case"

	// KindParse — разобрать документ на единицы и положения.
	KindParse = "parse"
)

// Job — задание в очереди.
type Job struct {
	ID        int64
	Kind      string
	SourceID  int64
	UnitLabel string
	Status    string
	Step      string
	Attempts  int
	Error     string

	// Notes — замечания о сделанной работе: что отброшено, какая часть
	// документа не далась. Отдельно от Error, который говорит, почему
	// работа не сделана вовсе.
	Notes []string

	// Plan — заказ задачи. Пуст у разбора: у него свой заказ (ParsePlan),
	// и читать один JSON двумя разборами значило бы завести два места, где
	// известен формат заказа.
	Plan      Plan
	ParsePlan ParsePlan

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Jobs — хранилище заданий.
type Jobs struct {
	gate *dbgate.Gate
}

func NewJobs(gate *dbgate.Gate) *Jobs { return &Jobs{gate: gate} }

// jobColumns — колонки задания в одном месте на все чтения.
//
// Одно место потому, что порядок колонок обязан сойтись с порядком доводов
// scanJob: разъедься они — и род задания приехал бы в поле шага, а
// компилятор промолчал бы, потому что обе колонки текстовые.
const jobColumns = `id, kind, source_id, unit_label, status, step, attempts, error,
	        notes, params, created_at, updated_at`

// scanRow — строка, откуда читается задание. Интерфейсом, а не pgx.Row,
// чтобы одинаково читались и QueryRow, и строки Query.
type scanRow interface {
	Scan(dest ...any) error
}

// scanJob читает задание вместе с его заказом.
//
// Заказ разбирается ПО РОДУ: у задачи и у разбора это разные записи, и
// разбирать их одним типом значило бы молча принять одну за другую. Род,
// которого мы не знаем, — отказ, а не «пусть будет задачей»: непонятое не
// применяется.
func scanJob(row scanRow) (Job, error) {
	var job Job
	var notes, params []byte
	err := row.Scan(&job.ID, &job.Kind, &job.SourceID, &job.UnitLabel, &job.Status,
		&job.Step, &job.Attempts, &job.Error, &notes, &params,
		&job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return Job{}, err
	}
	if err := json.Unmarshal(notes, &job.Notes); err != nil {
		return Job{}, fmt.Errorf("замечания задания %d не разобраны: %w", job.ID, err)
	}
	// Пустой список — [], а не nil: он уедет в ответ ручки, и null вместо
	// списка роняет студию на исправном случае — на задании без замечаний.
	if job.Notes == nil {
		job.Notes = []string{}
	}
	switch job.Kind {
	case KindCase:
		if err := json.Unmarshal(params, &job.Plan); err != nil {
			return Job{}, fmt.Errorf("план задания %d не разобран: %w", job.ID, err)
		}
	case KindParse:
		if err := json.Unmarshal(params, &job.ParsePlan); err != nil {
			return Job{}, fmt.Errorf("заказ разбора %d не разобран: %w", job.ID, err)
		}
	default:
		return Job{}, fmt.Errorf("задание %d неизвестного рода %q", job.ID, job.Kind)
	}
	return job, nil
}

// taker — чтение строки, запоминающее номер задания по дороге.
//
// Номер нужен ровно в одном случае: колонки прочлись, а заказ при задании
// не разобрался. Тогда задание надо отбить с причиной, а отбивать нечем —
// номер приехал бы только вместе с разобранным заказом. Задание осталось
// бы висеть идущим навсегда, а со стороны это неотличимо от «долго
// думает», то есть хуже отказа.
//
// Подглядывание безопасно потому, что порядок колонок задан одним местом
// (jobColumns) и первая из них — номер.
type taker struct {
	row scanRow
	id  *int64
}

func (t taker) Scan(dest ...any) error {
	if err := t.row.Scan(dest...); err != nil {
		return err
	}
	if len(dest) > 0 {
		if got, ok := dest[0].(*int64); ok {
			*t.id = *got
		}
	}
	return nil
}

// Place ставит заказ в очередь и отдаёт задание.
//
// Повтор того же заказа возвращает прежнее задание, а не заводит второе:
// составитель, нажавший «заказать» дважды, хотел одну задачу. Держит это
// указатель базы, а не проверка перед вставкой: проверка и вставка — два
// шага, и соперники сталкиваются между ними.
func (j *Jobs) Place(ctx context.Context, order Order, plan Plan) (Job, error) {
	params, err := json.Marshal(plan)
	if err != nil {
		return Job{}, fmt.Errorf("план заказа не записан: %w", err)
	}

	job, err := scanJob(j.gate.QueryRow(ctx,
		`INSERT INTO gen_jobs (kind, source_id, unit_label, idem_key, params)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (idem_key) WHERE idem_key <> '' AND status IN ('queued', 'running')
		 DO UPDATE SET updated_at = gen_jobs.updated_at
		 RETURNING `+jobColumns,
		KindCase, plan.SourceID, order.UnitLabel, order.IdemKey(), params))
	if err != nil {
		return Job{}, fmt.Errorf("задание не поставлено в очередь: %w", err)
	}
	return job, nil
}

// PlaceParse ставит в очередь разбор документа.
//
// Ключа повторности у разбора нет намеренно, и это не забывчивость.
// Повторный разбор — обычная работа составителя: документ разобрался
// плохо, задание модели поправили, разбирают заново. Ключ, склеивающий
// такие заказы в один, отдал бы на второй заказ прежнее — уже закрытое —
// задание, то есть молча отказал бы в переделке.
//
// Повтор при этом не бесплатен, и заслон от него всё-таки есть: идущий
// разбор того же документа не заводит второго (см. RunningParse). Разница
// существенна — здесь запрещено ПАРАЛЛЕЛЬНОЕ, а не повторное.
func (j *Jobs) PlaceParse(ctx context.Context, plan ParsePlan) (Job, error) {
	params, err := json.Marshal(plan)
	if err != nil {
		return Job{}, fmt.Errorf("заказ разбора не записан: %w", err)
	}
	job, err := scanJob(j.gate.QueryRow(ctx,
		`INSERT INTO gen_jobs (kind, source_id, params)
		 VALUES ($1, $2, $3)
		 RETURNING `+jobColumns,
		KindParse, plan.SourceID, params))
	if err != nil {
		return Job{}, fmt.Errorf("разбор не поставлен в очередь: %w", err)
	}
	return job, nil
}

// RunningParse — идёт ли уже разбор этого документа.
//
// Отдаёт номер идущего задания. Заслон нужен потому, что два разбора
// одного документа пишут в один черновик: второй затирает части первого, и
// на выходе получается разбор, которого не делал никто. Заметить это
// нельзя ничем — обе работы отчитываются успехом.
func (j *Jobs) RunningParse(ctx context.Context, documentID int64) (int64, bool, error) {
	var id int64
	err := j.gate.QueryRow(ctx,
		`SELECT id FROM gen_jobs
		  WHERE kind = $1
		    AND status IN ('queued', 'running')
		    AND (params ->> 'documentId')::bigint = $2
		  ORDER BY id
		  LIMIT 1`, KindParse, documentID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("идущий разбор документа %d не проверен: %w", documentID, err)
	}
	return id, true, nil
}

// Take берёт следующее задание своего рода в работу.
//
// Отбор и пометка — одним запросом с FOR UPDATE SKIP LOCKED: два
// исполнителя, читающие очередь одновременно, иначе взяли бы одно и то же
// задание и написали бы две задачи по одному заказу — за деньги.
//
// Род стоит в отборе, а не проверяется после: исполнитель, взявший чужое
// задание и положивший его обратно, всё равно уже пометил его идущим — и
// оно досталось бы своему исполнителю с лишней попыткой в счёте. Хуже
// того, «положить обратно» пришлось бы писать, а не написанное оставило бы
// чужое задание висеть идущим навсегда.
//
// Вторым ответом идёт «нашлось ли»: пустая очередь — это не отказ, и
// исполнитель, принявший её за отказ, начал бы её чинить.
func (j *Jobs) Take(ctx context.Context, kind string) (Job, bool, error) {
	// Номер читается отдельно от разбора заказа: разбор может не
	// состояться, и тогда задание надо отбить — а отбить его нечем, если
	// номер приехал только вместе с заказом.
	var id int64
	row := j.gate.QueryRow(ctx,
		`UPDATE gen_jobs
		    SET status = 'running', attempts = attempts + 1, updated_at = NOW()
		  WHERE id = (SELECT id FROM gen_jobs
		               WHERE status = 'queued' AND kind = $1
		               ORDER BY id
		               FOR UPDATE SKIP LOCKED
		               LIMIT 1)
		 RETURNING `+jobColumns, kind)
	job, err := scanJob(taker{row: row, id: &id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		if id == 0 {
			return Job{}, false, fmt.Errorf("задание не взято в работу: %w", err)
		}
		// Непонятое не применяется: план, собранный прежней выкаткой и не
		// разобравшийся нынешней, — это не задание с пробелом, а задание,
		// которого мы не понимаем. Выполнять его по умолчаниям значит
		// написать задачу не про то.
		_ = j.Fail(ctx, id, "заказ задания не разобран: "+err.Error())
		return Job{}, false, fmt.Errorf("задание %d отбито: заказ не разобран: %w", id, err)
	}
	return job, true, nil
}

// Step отмечает, на каком узле конвейера задание сейчас.
//
// Это единственное, по чему видно живое задание: узлы идут десятками
// секунд, и без отметки «идёт» неотличимо от «зависло».
func (j *Jobs) Step(ctx context.Context, id int64, step string) error {
	_, err := j.gate.Exec(ctx,
		`UPDATE gen_jobs SET step = $2, updated_at = NOW() WHERE id = $1`, id, step)
	if err != nil {
		return fmt.Errorf("шаг задания %d не записан: %w", id, err)
	}
	return nil
}

// Done закрывает задание успехом.
func (j *Jobs) Done(ctx context.Context, id int64) error {
	return j.finish(ctx, id, StatusDone, "")
}

// Fail закрывает задание отказом.
func (j *Jobs) Fail(ctx context.Context, id int64, reason string) error {
	if reason == "" {
		// Отказ без причины нечем разбирать, а разбирать его будут: это
		// единственный след того, почему задача не написалась.
		reason = "причина не названа"
	}
	return j.finish(ctx, id, StatusFailed, reason)
}

// Cancel снимает задание.
func (j *Jobs) Cancel(ctx context.Context, id int64) error {
	// Снять можно только то, что ещё не доделано: снятое доделанное
	// означало бы, что задача есть, а задание говорит, что её нет.
	tag, err := j.gate.Exec(ctx,
		`UPDATE gen_jobs
		    SET status = 'cancelled', updated_at = NOW()
		  WHERE id = $1 AND status IN ('queued', 'running')`, id)
	if err != nil {
		return fmt.Errorf("задание %d не снято: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("задание %d снять нельзя: оно уже закрыто", id)
	}
	return nil
}

// Retry заводит новое задание по плану прежнего.
//
// # Почему новое задание, а не оживление прежнего
//
// Отказ — единственный след того, почему задача не написалась, и
// переписав его статусом «в очереди», мы стёрли бы разбор вместе с
// причиной. Составитель, повторивший заказ трижды, должен видеть три
// отказа, а не один загадочно исправившийся.
//
// # Почему без ключа повторности
//
// Ключ считается от заказанного и защищает от ДВОЙНОГО НАЖАТИЯ: тот, кто
// щёлкнул «заказать» дважды, хотел одну задачу. Но он же запирал единицу
// НАВСЕГДА: задание отказало — ключ остался, и повторный заказ той же
// единицы молча возвращал прежнее, закрытое задание. Выглядело это как
// «студия меня не слышит», а единица выбывала из работы насовсем.
//
// Повтор — осознанное действие составителя, как и повторный разбор
// документа (см. PlaceParse), и ключа у него поэтому нет. От двойного
// нажатия здесь защищает не ключ, а отказ на идущую работу по той же
// единице: RunningFor.
func (j *Jobs) Retry(ctx context.Context, prev Job) (Job, error) {
	params, err := json.Marshal(prev.Plan)
	if err != nil {
		return Job{}, fmt.Errorf("план прежнего задания не записан: %w", err)
	}
	job, err := scanJob(j.gate.QueryRow(ctx,
		`INSERT INTO gen_jobs (kind, source_id, unit_label, params)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+jobColumns,
		KindCase, prev.SourceID, prev.UnitLabel, params))
	if err != nil {
		return Job{}, fmt.Errorf("повтор не поставлен в очередь: %w", err)
	}
	return job, nil
}

// RunningFor — идёт ли уже работа по этой единице источника.
//
// Отдаёт номер идущего задания. Нужен там, где ключа повторности нет:
// два задания на одну единицу пишут две задачи, и заплачено будет за обе,
// а хотел составитель одну.
func (j *Jobs) RunningFor(ctx context.Context, sourceID int64, unitLabel string) (int64, bool, error) {
	var id int64
	err := j.gate.QueryRow(ctx,
		`SELECT id FROM gen_jobs
		  WHERE kind = $1
		    AND status IN ('queued', 'running')
		    AND source_id = $2
		    AND unit_label = $3
		  ORDER BY id
		  LIMIT 1`, KindCase, sourceID, unitLabel).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("идущие задания по единице не прочитаны: %w", err)
	}
	return id, true, nil
}

func (j *Jobs) finish(ctx context.Context, id int64, status, reason string) error {
	_, err := j.gate.Exec(ctx,
		`UPDATE gen_jobs SET status = $2, error = $3, updated_at = NOW() WHERE id = $1`,
		id, status, reason)
	if err != nil {
		return fmt.Errorf("задание %d не закрыто: %w", id, err)
	}
	return nil
}

// Note дописывает замечание о сделанной работе.
//
// Дописывает на стороне базы, а не «прочитать, добавить, записать»: между
// чтением и записью вклинивается соседняя часть того же разбора, и её
// замечание пропало бы молча — то есть отброшенное перестало бы считаться
// ровно там, где его больше всего.
func (j *Jobs) Note(ctx context.Context, id int64, note string) error {
	if note == "" {
		return nil
	}
	_, err := j.gate.Exec(ctx,
		`UPDATE gen_jobs
		    SET notes = notes || to_jsonb($2::text), updated_at = NOW()
		  WHERE id = $1`, id, note)
	if err != nil {
		return fmt.Errorf("замечание к заданию %d не записано: %w", id, err)
	}
	return nil
}

// Job читает одно задание.
func (j *Jobs) Job(ctx context.Context, id int64) (Job, error) {
	job, err := scanJob(j.gate.QueryRow(ctx,
		`SELECT `+jobColumns+` FROM gen_jobs WHERE id = $1`, id))
	if err != nil {
		return Job{}, fmt.Errorf("задание %d не найдено: %w", id, err)
	}
	return job, nil
}

// Recent — последние задания источника.
func (j *Jobs) Recent(ctx context.Context, sourceID int64, limit int) ([]Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := j.gate.Query(ctx,
		`SELECT id, kind, source_id, unit_label, status, step, attempts, error, notes,
		        COALESCE(params ->> 'filename', ''),
		        created_at, updated_at
		   FROM gen_jobs
		  WHERE source_id = $1
		  ORDER BY id DESC
		  LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("список заданий не прочитан: %w", err)
	}
	defer rows.Close()

	// Заказ в список не читается намеренно: он весит килобайты (положения
	// единицы и соседей), а списку нужны состояние и шаг. Читать его на
	// каждую строку значит таскать весь документ ради одной колонки.
	//
	// Замечания при этом читаются: их читает человек, глядя в список, и
	// «одна часть из восьмидесяти не далась» — это ровно то, ради чего в
	// список и смотрят.
	//
	// Имя файла вынимается из заказа ОДНИМ полем, а не разбором заказа
	// целиком: разбор называется в списке именем документа, и без него
	// строка разбора стоит безымянной — номер документа человеку не
	// говорит ничего. Одно поле стоит копейки, весь заказ — килобайты.
	out := []Job{}
	for rows.Next() {
		var job Job
		var notes []byte
		if err := rows.Scan(&job.ID, &job.Kind, &job.SourceID, &job.UnitLabel, &job.Status,
			&job.Step, &job.Attempts, &job.Error, &notes, &job.ParsePlan.Filename,
			&job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, fmt.Errorf("строка задания не разобрана: %w", err)
		}
		if err := json.Unmarshal(notes, &job.Notes); err != nil {
			return nil, fmt.Errorf("замечания задания %d не разобраны: %w", job.ID, err)
		}
		if job.Notes == nil {
			job.Notes = []string{}
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("задания дочитаны не до конца: %w", err)
	}
	return out, nil
}

// SaveDraft кладёт черновик задачи, написанный по заданию.
//
// Черновик — это ответ модели, принятый разбором: он хранится целиком и
// отдельно от задачи. Задачей он станет, когда его примет составитель, и
// до тех пор ни один запрос, читающий задачи, его не увидит — по той же
// причине, по какой черновик разбора источника живёт в своих таблицах:
// флаг забывают в условии запроса, отдельную таблицу забыть нельзя.
func (j *Jobs) SaveDraft(ctx context.Context, job Job, draft Draft) (int64, error) {
	body, err := json.Marshal(draft)
	if err != nil {
		return 0, fmt.Errorf("черновик задачи не записан: %w", err)
	}
	var id int64
	err = j.gate.QueryRow(ctx,
		`INSERT INTO case_drafts (job_id, source_id, unit_label, body)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		job.ID, job.SourceID, job.UnitLabel, body).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("черновик задачи не сохранён: %w", err)
	}
	return id, nil
}

// SaveCheck записывает итог слепой сверки в черновик.
//
// Отдельной записью после черновика, а не полем при вставке: сверка идёт
// ПОСЛЕ написания и стоит отдельных денег, и черновик обязан существовать
// раньше неё. Копи мы то и другое до одной вставки, обрыв между узлами
// терял бы уже написанное — то есть то, за что заплачено.
func (j *Jobs) SaveCheck(ctx context.Context, draftID int64, check Check) error {
	body, err := json.Marshal(check)
	if err != nil {
		return fmt.Errorf("итог сверки не записан: %w", err)
	}
	res, err := j.gate.Exec(ctx,
		`UPDATE case_drafts SET blind_check = $2 WHERE id = $1`, draftID, body)
	if err != nil {
		return fmt.Errorf("итог сверки не сохранён: %w", err)
	}
	if res.RowsAffected() == 0 {
		// Черновика нет — значит записали не туда, и молчать нельзя:
		// успех при нуле строк выглядит как записанная сверка, которой
		// не существует.
		return fmt.Errorf("черновик %d не найден: итог сверки не сохранён", draftID)
	}
	return nil
}

// Drafts — черновики, написанные по заданию.
//
// Списком, а не одним: перегенерация пишет второй черновик по тому же
// заданию, и прежний не затирается. Составитель сравнивает их и выбирает,
// а затёртый черновик сравнить не с чем.
func (j *Jobs) Drafts(ctx context.Context, jobID int64) ([]Stored, error) {
	rows, err := j.gate.Query(ctx,
		`SELECT id, body, blind_check FROM case_drafts WHERE job_id = $1 ORDER BY id`, jobID)
	if err != nil {
		return nil, fmt.Errorf("черновики задания %d не прочитаны: %w", jobID, err)
	}
	defer rows.Close()

	out := []Stored{}
	for rows.Next() {
		var id int64
		var raw, rawCheck []byte
		if err := rows.Scan(&id, &raw, &rawCheck); err != nil {
			return nil, fmt.Errorf("строка черновика не разобрана: %w", err)
		}
		var draft Draft
		if err := json.Unmarshal(raw, &draft); err != nil {
			// Непонятое не применяется: черновик, записанный прежней
			// выкаткой и не разобравшийся нынешней, — это не черновик с
			// пробелом. Отдать его наполовину значит показать составителю
			// задачу, которой никто не писал.
			return nil, fmt.Errorf("черновик задания %d не разобран: %w", jobID, err)
		}
		stored := Stored{ID: id, Draft: draft}
		if len(rawCheck) > 0 {
			var check Check
			if err := json.Unmarshal(rawCheck, &check); err != nil {
				// И здесь непонятое не применяется, но роняется только
				// сверка, а не черновик: задача написана и цела, а
				// неразобранный итог сверки — это ровно «сверки нет».
				// Уронив весь черновик, мы спрятали бы от составителя
				// исправную задачу из-за испорченной приписки к ней.
				log.Printf("черновик %d: итог сверки не разобран: %v", id, err)
			} else {
				stored.Check = &check
			}
		}
		out = append(out, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("черновики дочитаны не до конца: %w", err)
	}
	return out, nil
}
