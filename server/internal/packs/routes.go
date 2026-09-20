package packs

import (
	"errors"
	"net/http"
	"time"

	"curator/server/internal/studio"
)

// Ручки наборов в студии.
//
// Право стоит первым доводом у каждой: собрать набор и выпустить его —
// разные действия. Выпустить значит подписать нашим ключом то, что встанет
// на устройства и переживёт любую правку, потому что выпуск неизменяем.
func Routes(desk *studio.Desk, store *Store) {
	r := &routes{store: store}

	desk.Handle(studio.PermPacks, "POST /admin/api/packs", r.create)
	desk.Handle(studio.PermPacks, "PUT /admin/api/packs/{slug}/items", r.setItems)
	desk.Handle(studio.PermPacks, "POST /admin/api/packs/{slug}/releases", r.release)
	desk.Handle(studio.PermPacks, "GET /admin/api/packs", r.list)
	desk.Handle(studio.PermPacks, "GET /admin/api/packs/{slug}", r.show)
	desk.Handle(studio.PermPacks, "PUT /admin/api/packs/{slug}", r.update)
}

type routes struct {
	store *Store
}

type createRequest struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	SummaryMd string `json:"summaryMd"`
	Line      string `json:"line"`
}

func (r *routes) create(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body createRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	if err := r.store.Create(req.Context(), body.Slug, body.Title, body.SummaryMd, body.Line); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusCreated, map[string]any{"slug": body.Slug})
}

type itemsRequest struct {
	Cases    []string `json:"cases"`
	Revision int      `json:"revision"`
}

// setItems задаёт состав набора целиком.
//
// Целиком, а не по одной задаче: состав — это порядок, и правка по одной
// превращает его в череду мелких решений, из которых порядок не виден
// никому. Заодно это снимает вопрос, что делать с задачей, которую убрали:
// её просто нет в присланном списке.
func (r *routes) setItems(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body itemsRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	count, revision, err := r.store.SetItems(
		req.Context(), req.PathValue("slug"), body.Cases, body.Revision)
	if err != nil {
		studio.WriteError(w, stale(err), studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"cases": count, "revision": revision})
}

func (r *routes) release(w http.ResponseWriter, req *http.Request, _ studio.User) {
	out, err := r.store.Publish(req.Context(), req.PathValue("slug"), time.Now())
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusCreated, map[string]any{
		"slug": out.Slug, "version": out.Version,
		"cases": len(out.Manifest.Cases), "keyId": out.KeyID,
	})
}

func (r *routes) list(w http.ResponseWriter, req *http.Request, _ studio.User) {
	list, err := r.store.All(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Наборы не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"slug": one.Slug, "title": one.Title, "status": one.Status,
			"cases": one.Cases, "version": one.Version, "line": one.Line,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"packs": out})
}

func (r *routes) show(w http.ResponseWriter, req *http.Request, _ studio.User) {
	one, err := r.store.One(req.Context(), req.PathValue("slug"))
	if err != nil {
		studio.WriteError(w, http.StatusNotFound, studio.Sentence(err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(one.Items))
	for _, item := range one.Items {
		items = append(items, map[string]any{
			"id": item.ID, "ord": item.Ord, "title": item.Title,
			"unitLabel": item.UnitLabel, "status": item.Status,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"slug": one.Slug, "title": one.Title, "summaryMd": one.SummaryMd,
		"status": one.Status, "version": one.Version, "revision": one.Revision,
		"line": one.Line, "cases": items,
	})
}

type updateRequest struct {
	Title     string `json:"title"`
	SummaryMd string `json:"summaryMd"`
	Status    string `json:"status"`
	Line      string `json:"line"`
	Revision  int    `json:"revision"`
}

// stale разводит «так нельзя» и «вы опоздали».
//
// Разными кодами, а не одним: студия по 409 не просто показывает слова
// сервера, а предлагает перечитать набор — единственное, что тут можно
// сделать. По 400 такого предложения быть не должно, иначе оно появится
// и на «у набора должно быть название».
func stale(err error) int {
	if errors.Is(err, ErrStale) {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

func (r *routes) update(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body updateRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	revision, err := r.store.Update(req.Context(), req.PathValue("slug"),
		body.Title, body.SummaryMd, body.Status, body.Line, body.Revision)
	if err != nil {
		studio.WriteError(w, stale(err), studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"status": body.Status, "revision": revision,
	})
}
