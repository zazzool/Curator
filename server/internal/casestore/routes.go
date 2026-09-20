package casestore

import (
	"net/http"
	"strconv"

	"curator/server/internal/studio"
)

// Ручки задач.
//
// Право стоит первым доводом у каждой, и прав здесь два: читать задачи и
// править их — разные вещи. Публикация закрыта правом правки, а не своим
// собственным: снять с раздачи и выпустить — это одно решение, принятое в
// две стороны, и разводить их по разным правам значило бы разрешить
// одному выпускать, а другому снимать.
func Routes(desk *studio.Desk, store *Store) {
	r := &routes{store: store}

	desk.Handle(studio.PermCaseRead, "GET /admin/api/cases", r.list)
	desk.Handle(studio.PermCaseRead, "GET /admin/api/cases/{id}", r.show)
	desk.Handle(studio.PermCaseRead, "GET /admin/api/content-version", r.version)

	desk.Handle(studio.PermCaseWrite, "POST /admin/api/cases", r.fromDraft)
	desk.Handle(studio.PermCaseWrite, "PUT /admin/api/cases/{id}", r.save)
	desk.Handle(studio.PermCaseWrite, "POST /admin/api/cases/{id}/publish", r.publish)
	desk.Handle(studio.PermCaseWrite, "POST /admin/api/cases/{id}/withdraw", r.withdraw)
}

type routes struct {
	store *Store
}

func (r *routes) list(w http.ResponseWriter, req *http.Request, _ studio.User) {
	q := req.URL.Query()
	sourceID, _ := strconv.ParseInt(q.Get("source"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))

	cases, err := r.store.Cases(req.Context(), Filter{
		SourceID: sourceID,
		Path:     q.Get("path"),
		Status:   Status(q.Get("status")),
		Limit:    limit,
	})
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Задачи не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(cases))
	for _, one := range cases {
		out = append(out, caseJSON(one))
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"cases": out})
}

func (r *routes) show(w http.ResponseWriter, req *http.Request, _ studio.User) {
	one, err := r.store.Case(req.Context(), req.PathValue("id"))
	if err != nil {
		studio.WriteError(w, http.StatusNotFound, "Такой задачи нет")
		return
	}
	studio.WriteJSON(w, http.StatusOK, caseJSON(one))
}

func (r *routes) version(w http.ResponseWriter, req *http.Request, _ studio.User) {
	version, err := r.store.Version(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Версия содержания не прочитана")
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"version": version})
}

type fromDraftRequest struct {
	DraftID int64 `json:"draftId"`
}

func (r *routes) fromDraft(w http.ResponseWriter, req *http.Request, user studio.User) {
	var body fromDraftRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	one, repeated, err := r.store.FromDraft(req.Context(), body.DraftID, "generated:"+user.Login)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	// Повтор отдаётся прежней задачей и кодом 200, а не 201: вторая
	// задача не заведена, и сказать «создано» значит соврать студии,
	// которая по этому коду решает, что показать составителю.
	out := caseJSON(one)
	out["repeated"] = repeated
	if repeated {
		studio.WriteJSON(w, http.StatusOK, out)
		return
	}
	studio.WriteJSON(w, http.StatusCreated, out)
}

type saveRequest struct {
	Body     Body `json:"body"`
	Revision int  `json:"revision"`
}

func (r *routes) save(w http.ResponseWriter, req *http.Request, user studio.User) {
	var body saveRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	one, err := r.store.Save(req.Context(), req.PathValue("id"), body.Body, body.Revision, user.Login)
	if err != nil {
		studio.WriteError(w, http.StatusConflict, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, caseJSON(one))
}

// publish выпускает задачу в раздачу.
//
// Отказ уезжает списком, а не строкой: составитель правит задачу в один
// заход, и отказ, называющий одну беду из четырёх, заставляет его ходить
// по кругу ровно четыре раза.
func (r *routes) publish(w http.ResponseWriter, req *http.Request, user studio.User) {
	one, err := r.store.Publish(req.Context(), req.PathValue("id"), user.Login)
	if err != nil {
		if faults, ok := err.(Faults); ok {
			list := make([]map[string]any, 0, len(faults))
			for _, f := range faults {
				list = append(list, map[string]any{"where": f.Where, "what": f.What})
			}
			studio.WriteJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "Задачу пока нельзя раздавать — вот что мешает",
				"faults": list,
			})
			return
		}
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, caseJSON(one))
}

func (r *routes) withdraw(w http.ResponseWriter, req *http.Request, user studio.User) {
	one, err := r.store.Withdraw(req.Context(), req.PathValue("id"), user.Login)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, caseJSON(one))
}

// caseJSON — задача, какой её видит студия.
func caseJSON(c Case) map[string]any {
	out := map[string]any{
		"id":         c.ID,
		"sourceId":   c.SourceID,
		"unitLabel":  c.UnitLabel,
		"unitPath":   c.UnitPath,
		"status":     string(c.Status),
		"statusWord": StatusWord(c.Status),
		"revision":   c.Revision,
		"origin":     c.Origin,
		"body":       c.Body,
		"createdAt":  c.CreatedAt.Format(timeLayout),
		"updatedAt":  c.UpdatedAt.Format(timeLayout),
	}
	if c.PublishedAt != nil {
		out["publishedAt"] = c.PublishedAt.Format(timeLayout)
	}
	// Пустые списки — [], а не null: студия ходит по ним циклом, и null
	// роняет её на исправном случае — на задаче без разметки.
	if c.Body.Segments == nil {
		out["body"] = withEmptyLists(c.Body)
	}
	return out
}

func withEmptyLists(b Body) Body {
	if b.Segments == nil {
		b.Segments = []Segment{}
	}
	if b.Options == nil {
		b.Options = []Option{}
	}
	return b
}

const timeLayout = "2006-01-02T15:04:05Z07:00"
