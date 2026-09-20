package gen

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"curator/server/internal/studio"
)

// Ручки генерации.
//
// Право стоит первым доводом у каждой: заказать задачу и читать промпты —
// разные права, потому что править задание модели значит решать, какими
// будут все будущие задачи, а заказать одну задачу — нет.
func Routes(desk *studio.Desk, jobs *Jobs, resolver *Resolver, prompts *Prompts, models ModelLister) {
	r := &routes{jobs: jobs, resolver: resolver, prompts: prompts, models: models}

	desk.Handle(studio.PermGenerate, "POST /admin/api/sources/{id}/orders", r.place)
	desk.Handle(studio.PermGenerate, "GET /admin/api/sources/{id}/jobs", r.list)
	desk.Handle(studio.PermGenerate, "GET /admin/api/jobs/{id}", r.show)
	desk.Handle(studio.PermGenerate, "POST /admin/api/jobs/{id}/cancel", r.cancel)
	desk.Handle(studio.PermGenerate, "POST /admin/api/jobs/{id}/retry", r.retry)

	// Разбор документа закрыт правом генерации, а не правом принимать
	// разбор, и это не описка. Право принимать отвечает на «что теперь
	// истина источника», а разбор истины не меняет: он кладёт черновик и
	// тратит на это деньги у поставщика моделей — то есть ровно то, чем
	// ведает право генерации. Принимать разобранное по-прежнему может
	// только тот, кому выдано право принимать.
	desk.Handle(studio.PermGenerate, "POST /admin/api/documents/{id}/parse", r.parse)

	desk.Handle(studio.PermPrompts, "GET /admin/api/prompts", r.listPrompts)
	desk.Handle(studio.PermPrompts, "PUT /admin/api/prompts/{id}", r.savePrompt)
	// Список моделей закрыт тем же правом, что и задания: он существует
	// ради одного поля в них. Права без раздела не бывает — раздел
	// мастерской закрыт правом на сервере, а не спрятан в студии.
	desk.Handle(studio.PermPrompts, "GET /admin/api/models", r.listModels)
}

type routes struct {
	jobs     *Jobs
	resolver *Resolver
	prompts  *Prompts
	models   ModelLister
}

type orderRequest struct {
	UnitLabel       string `json:"unitLabel"`
	Kind            string `json:"kind"`
	TargetStatement string `json:"targetStatement"`
	Model           string `json:"model"`
}

type parseRequest struct {
	Model string `json:"model"`
}

// parse ставит в очередь разбор документа.
//
// Разрешение заказа идёт здесь, при заказе, а не у исполнителя: составитель
// должен узнать «документ не привязан к источнику» сразу, а не через час
// отказом задания.
//
// Идущий разбор того же документа второго не заводит, и отказ говорит об
// этом словами. Два разбора одного документа пишут в один черновик: второй
// затирает части первого, и на выходе получается разбор, которого не делал
// никто, — притом обе работы отчитываются успехом.
func (r *routes) parse(w http.ResponseWriter, req *http.Request, _ studio.User) {
	documentID, ok := pathID(w, req)
	if !ok {
		return
	}
	var body parseRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	plan, err := r.resolver.ResolveParse(req.Context(), documentID, body.Model)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	if _, running, err := r.jobs.RunningParse(req.Context(), documentID); err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Идущий разбор не проверен")
		return
	} else if running {
		studio.WriteError(w, http.StatusConflict,
			"Этот документ уже разбирается: дождитесь конца или снимите задание")
		return
	}
	job, err := r.jobs.PlaceParse(req.Context(), plan)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Разбор не поставлен в очередь")
		return
	}
	studio.WriteJSON(w, http.StatusCreated, jobJSON(job, nil))
}

// retry заводит новое задание по плану отказавшего.
//
// План берётся у прежнего задания, а не разрешается заново, и это
// существенно: повторяют обычно то, что отказало по дороге — кончились
// деньги, модель вернула не тот JSON, оборвалась сеть, — а не то, что
// стало невыполнимым. Разреши мы заказ заново, повтор отказал бы всякий
// раз, когда соседнюю единицу успели поправить, и выглядело бы это как
// «повторить нельзя», хотя повторять было можно.
//
// Повторяется только ЗАКРЫТОЕ задание. Идущее повторять нечего: оно ещё
// не кончилось, и второе задание по той же единице написало бы вторую
// задачу — заплатить пришлось бы за обе, а хотел составитель одну.
func (r *routes) retry(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	prev, err := r.jobs.Job(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusNotFound, "Такого задания нет")
		return
	}
	if prev.Kind != KindCase {
		// Разбор документа повторяется своей ручкой: у него другой заказ,
		// другой исполнитель и свой заслон на параллельный разбор.
		studio.WriteError(w, http.StatusBadRequest,
			"Это задание не про написание задачи: разбор документа повторяется со страницы источника")
		return
	}
	if prev.Status == "queued" || prev.Status == "running" {
		studio.WriteError(w, http.StatusConflict,
			"Это задание ещё идёт: дождитесь конца или снимите его")
		return
	}
	if running, busy, err := r.jobs.RunningFor(req.Context(), prev.SourceID, prev.UnitLabel); err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Идущие задания не проверены")
		return
	} else if busy {
		studio.WriteError(w, http.StatusConflict, studio.Sentence(fmt.Sprintf(
			"по этой единице уже идёт задание № %d: дождитесь конца или снимите его", running)))
		return
	}
	job, err := r.jobs.Retry(req.Context(), prev)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Повтор не поставлен в очередь")
		return
	}
	studio.WriteJSON(w, http.StatusCreated, jobJSON(job, nil))
}

// place разрешает заказ и ставит его в очередь.
//
// Разрешение идёт здесь, при заказе, а не у исполнителя: составитель
// должен узнать «по этой единице писать нельзя» сразу, а не через час
// отказом задания. Заодно это и есть то самое разрешение контекста при
// заказе, на котором стоит весь конвейер.
func (r *routes) place(w http.ResponseWriter, req *http.Request, _ studio.User) {
	sourceID, ok := pathID(w, req)
	if !ok {
		return
	}
	var body orderRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	order := Order{
		SourceID: sourceID, UnitLabel: body.UnitLabel, Kind: body.Kind,
		TargetStatement: body.TargetStatement, Model: body.Model,
	}
	plan, err := r.resolver.Resolve(req.Context(), order)
	if err != nil {
		// Текст отказа уезжает как есть: он написан словарём источника и
		// говорит, что не так. «Проверьте поля» отправило бы составителя
		// перебирать их вслепую.
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	job, err := r.jobs.Place(req.Context(), order, plan)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Заказ не поставлен в очередь")
		return
	}
	studio.WriteJSON(w, http.StatusCreated, jobJSON(job, nil))
}

func (r *routes) list(w http.ResponseWriter, req *http.Request, _ studio.User) {
	sourceID, ok := pathID(w, req)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	jobs, err := r.jobs.Recent(req.Context(), sourceID, limit)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Список заданий не прочитан")
		return
	}
	out := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, jobJSON(job, nil))
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (r *routes) show(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	job, err := r.jobs.Job(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusNotFound, "Такого задания нет")
		return
	}
	drafts, err := r.jobs.Drafts(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Черновики задания не прочитаны")
		return
	}
	studio.WriteJSON(w, http.StatusOK, jobJSON(job, drafts))
}

func (r *routes) cancel(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	if err := r.jobs.Cancel(req.Context(), id); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}

func (r *routes) listPrompts(w http.ResponseWriter, req *http.Request, _ studio.User) {
	list, err := r.prompts.All(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Задания моделей не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]any{
			"id": p.ID, "name": p.Name, "node": p.Node, "nodeWord": NodeWord(p.Node),
			"systemMd": p.SystemMd, "userMd": p.UserMd, "revision": p.Revision,
			// Модель уезжает как есть, пустой в том числе: пустая означает
			// «моделью поставщика», и подставь мы сюда её имя, студия
			// показала бы выбор там, где выбора не делали, — а значит
			// перестала бы показывать, где его сделали.
			"model": p.Model,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"prompts": out})
}

type promptRequest struct {
	Name     string `json:"name"`
	SystemMd string `json:"systemMd"`
	UserMd   string `json:"userMd"`

	// Model — модель узла; пустая строка означает «моделью поставщика».
	//
	// Список моделей здесь НЕ сверяется, и это решение, а не недосмотр:
	// словарь моделей ведём не мы. Новая модель появляется у поставщика
	// раньше, чем в нашем прайсе, и сверка по прайсу отказывала бы ровно
	// в тот день, когда составителю понадобилось её попробовать.
	Model string `json:"model"`

	Revision int `json:"revision"`
}

// savePrompt правит задание модели.
//
// Редакция сверяется, а не игнорируется: два составителя, открывшие одно
// задание, иначе затирают друг друга молча — и узнает об этом тот, чья
// правка пропала, по качеству задач через неделю.
func (r *routes) savePrompt(w http.ResponseWriter, req *http.Request, user studio.User) {
	var body promptRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	saved, err := r.prompts.Save(req.Context(), Prompt{
		ID: req.PathValue("id"), Name: body.Name,
		SystemMd: body.SystemMd, UserMd: body.UserMd,
		Model: body.Model, Revision: body.Revision,
	}, user.Login)
	if err != nil {
		studio.WriteError(w, http.StatusConflict, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"id": saved.ID, "revision": saved.Revision, "model": saved.Model,
	})
}

// jobJSON — задание, каким его видит студия.
//
// План в ответ не уезжает: он весит килобайты (положения единицы и всех
// соседей), а студии нужны состояние, шаг и то, что написано. Нужен план —
// это другой разговор и другая ручка.
func jobJSON(job Job, drafts []Stored) map[string]any {
	// Замечания — пустым списком, а не null: студия ходит по ним циклом, и
	// null роняет её на исправном случае — на задании без замечаний.
	notes := job.Notes
	if notes == nil {
		notes = []string{}
	}
	out := map[string]any{
		"id":        job.ID,
		"kind":      job.Kind,
		"sourceId":  job.SourceID,
		"unitLabel": job.UnitLabel,
		"status":    job.Status,
		"step":      job.Step,
		"attempts":  job.Attempts,
		"error":     job.Error,
		"notes":     notes,
		"createdAt": job.CreatedAt.Format(time.RFC3339),
		"updatedAt": job.UpdatedAt.Format(time.RFC3339),
		"taskKind":  job.Plan.TaskKind,
		"unitTitle": job.Plan.Unit.Title,
		"unitWord":  job.Plan.UnitWord,
	}
	// Шаг словами зависит от рода. У задачи шаг — это узел конвейера, и
	// «compose» составителю ничего не говорит; у разбора шаг — это «часть
	// 12 из 80», то есть уже русская строка, и прогонять её через словарь
	// узлов значит либо вернуть её как есть (случайно), либо однажды
	// подменить чужим словом.
	if job.Kind == KindParse {
		out["stepWord"] = job.Step
		out["documentName"] = job.ParsePlan.Filename
	} else {
		out["stepWord"] = NodeWord(job.Step)
	}
	if drafts != nil {
		// Пустой список — [], а не null: студия ходит по нему циклом, и
		// null роняет её на исправном случае — на задании, которое ещё в
		// очереди.
		list := make([]Stored, 0, len(drafts))
		list = append(list, drafts...)
		out["drafts"] = list
	}
	return out
}

func pathID(w http.ResponseWriter, req *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		studio.WriteError(w, http.StatusBadRequest, "Номер в адресе не разобран")
		return 0, false
	}
	return id, true
}
