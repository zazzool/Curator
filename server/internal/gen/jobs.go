package gen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// Job — задание в очереди.
type Job struct {
	ID        int64
	SourceID  int64
	UnitLabel string
	Status    string
	Step      string
	Attempts  int
	Error     string
	Plan      Plan
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Jobs — хранилище заданий.
type Jobs struct {
	gate *dbgate.Gate
}

func NewJobs(gate *dbgate.Gate) *Jobs { return &Jobs{gate: gate} }

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

	var job Job
	var raw []byte
	err = j.gate.QueryRow(ctx,
		`INSERT INTO gen_jobs (source_id, unit_label, idem_key, params)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (idem_key) WHERE idem_key <> ''
		 DO UPDATE SET updated_at = gen_jobs.updated_at
		 RETURNING id, source_id, unit_label, status, step, attempts, error, params,
		           created_at, updated_at`,
		plan.SourceID, order.UnitLabel, order.IdemKey(), params).
		Scan(&job.ID, &job.SourceID, &job.UnitLabel, &job.Status, &job.Step,
			&job.Attempts, &job.Error, &raw, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return Job{}, fmt.Errorf("задание не поставлено в очередь: %w", err)
	}
	if err := json.Unmarshal(raw, &job.Plan); err != nil {
		return Job{}, fmt.Errorf("план задания не разобран: %w", err)
	}
	return job, nil
}

// Take берёт следующее задание в работу.
//
// Отбор и пометка — одним запросом с FOR UPDATE SKIP LOCKED: два
// исполнителя, читающие очередь одновременно, иначе взяли бы одно и то же
// задание и написали бы две задачи по одному заказу — за деньги.
//
// Вторым ответом идёт «нашлось ли»: пустая очередь — это не отказ, и
// исполнитель, принявший её за отказ, начал бы её чинить.
func (j *Jobs) Take(ctx context.Context) (Job, bool, error) {
	var job Job
	var raw []byte
	err := j.gate.QueryRow(ctx,
		`UPDATE gen_jobs
		    SET status = 'running', attempts = attempts + 1, updated_at = NOW()
		  WHERE id = (SELECT id FROM gen_jobs
		               WHERE status = 'queued'
		               ORDER BY id
		               FOR UPDATE SKIP LOCKED
		               LIMIT 1)
		 RETURNING id, source_id, unit_label, status, step, attempts, error, params,
		           created_at, updated_at`).
		Scan(&job.ID, &job.SourceID, &job.UnitLabel, &job.Status, &job.Step,
			&job.Attempts, &job.Error, &raw, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("задание не взято в работу: %w", err)
	}
	if err := json.Unmarshal(raw, &job.Plan); err != nil {
		// Непонятое не применяется: план, собранный прежней выкаткой и не
		// разобравшийся нынешней, — это не задание с пробелом, а задание,
		// которого мы не понимаем. Выполнять его по умолчаниям значит
		// написать задачу не про то.
		_ = j.Fail(ctx, job.ID, "план задания не разобран: "+err.Error())
		return Job{}, false, fmt.Errorf("задание %d отбито: план не разобран: %w", job.ID, err)
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

func (j *Jobs) finish(ctx context.Context, id int64, status, reason string) error {
	_, err := j.gate.Exec(ctx,
		`UPDATE gen_jobs SET status = $2, error = $3, updated_at = NOW() WHERE id = $1`,
		id, status, reason)
	if err != nil {
		return fmt.Errorf("задание %d не закрыто: %w", id, err)
	}
	return nil
}

// Job читает одно задание.
func (j *Jobs) Job(ctx context.Context, id int64) (Job, error) {
	var job Job
	var raw []byte
	err := j.gate.QueryRow(ctx,
		`SELECT id, source_id, unit_label, status, step, attempts, error, params,
		        created_at, updated_at
		   FROM gen_jobs WHERE id = $1`, id).
		Scan(&job.ID, &job.SourceID, &job.UnitLabel, &job.Status, &job.Step,
			&job.Attempts, &job.Error, &raw, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return Job{}, fmt.Errorf("задание %d не найдено: %w", id, err)
	}
	if err := json.Unmarshal(raw, &job.Plan); err != nil {
		return Job{}, fmt.Errorf("план задания %d не разобран: %w", id, err)
	}
	return job, nil
}

// Recent — последние задания источника.
func (j *Jobs) Recent(ctx context.Context, sourceID int64, limit int) ([]Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := j.gate.Query(ctx,
		`SELECT id, source_id, unit_label, status, step, attempts, error,
		        created_at, updated_at
		   FROM gen_jobs
		  WHERE source_id = $1
		  ORDER BY id DESC
		  LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("список заданий не прочитан: %w", err)
	}
	defer rows.Close()

	// План в список не читается намеренно: он весит килобайты (положения
	// единицы и соседей), а списку нужны состояние и шаг. Читать его на
	// каждую строку значит таскать весь документ ради одной колонки.
	out := []Job{}
	for rows.Next() {
		var job Job
		if err := rows.Scan(&job.ID, &job.SourceID, &job.UnitLabel, &job.Status,
			&job.Step, &job.Attempts, &job.Error, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, fmt.Errorf("строка задания не разобрана: %w", err)
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

// Drafts — черновики, написанные по заданию.
//
// Списком, а не одним: перегенерация пишет второй черновик по тому же
// заданию, и прежний не затирается. Составитель сравнивает их и выбирает,
// а затёртый черновик сравнить не с чем.
func (j *Jobs) Drafts(ctx context.Context, jobID int64) ([]Draft, error) {
	rows, err := j.gate.Query(ctx,
		`SELECT body FROM case_drafts WHERE job_id = $1 ORDER BY id`, jobID)
	if err != nil {
		return nil, fmt.Errorf("черновики задания %d не прочитаны: %w", jobID, err)
	}
	defer rows.Close()

	out := []Draft{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
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
		out = append(out, draft)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("черновики дочитаны не до конца: %w", err)
	}
	return out, nil
}
