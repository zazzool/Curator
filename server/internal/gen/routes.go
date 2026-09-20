package gen

import (
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
func Routes(desk *studio.Desk, jobs *Jobs, resolver *Resolver, prompts *Prompts) {
	r := &routes{jobs: jobs, resolver: resolver, prompts: prompts}

	desk.Handle(studio.PermGenerate, "POST /admin/api/sources/{id}/orders", r.place)
	desk.Handle(studio.PermGenerate, "GET /admin/api/sources/{id}/jobs", r.list)
	desk.Handle(studio.PermGenerate, "GET /admin/api/jobs/{id}", r.show)
	desk.Handle(studio.PermGenerate, "POST /admin/api/jobs/{id}/cancel", r.cancel)

	// Разбор документа закрыт правом генерации, а не правом принимать
	// разбор, и это не описка. Право принимать отвечает на «что теперь
	// истина источника», а разбор истины не меняет: он кладёт черновик и
	// тратит на это деньги у поставщика моделей — то есть ровно то, чем
	// ведает право генерации. Принимать разобранное по-прежнему может
	// только тот, кому выдано право принимать.
	desk.Handle(studio.PermGenerate, "POST /admin/api/documents/{id}/parse", r.parse)

	desk.Handle(studio.PermPrompts, "GET /admin/api/prompts", r.listPrompts)
	desk.Handle(studio.PermPrompts, "PUT /admin/api/prompts/{id}", r.savePrompt)
}

type routes struct {
	jobs     *Jobs
	resolver *Resolver
	prompts  *Prompts
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
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"prompts": out})
}

type promptRequest struct {
	Name     string `json:"name"`
	SystemMd string `json:"systemMd"`
	UserMd   string `json:"userMd"`
	Revision int    `json:"revision"`
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
		SystemMd: body.SystemMd, UserMd: body.UserMd, Revision: body.Revision,
	}, user.Login)
	if err != nil {
		studio.WriteError(w, http.StatusConflict, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"id": saved.ID, "revision": saved.Revision,
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
