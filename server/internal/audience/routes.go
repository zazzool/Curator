package audience

import (
	"net/http"
	"strconv"
	"time"

	"curator/server/internal/studio"
)

// Ручки групп в студии.
//
// # Почему право — «клиенты», а не своё
//
// Группа — это то же, что карточка врача, только про многих сразу: кому
// что открыто и почему. Своё право здесь выглядело бы честнее, но в день
// наката оно не было бы выдано никому, и раздел, ради которого всё
// делалось, оказался бы закрыт для всех — включая того, кто должен был бы
// его раздать.
//
// Привязка набора к группам закрыта правом наборов, а не клиентов: это
// решение о товаре, и принимают его там же, где линейку.
func Routes(desk *studio.Desk, store *Store) {
	r := &routes{store: store}

	desk.Handle(studio.PermClients, "GET /admin/api/audiences", r.list)
	desk.Handle(studio.PermClients, "POST /admin/api/audiences", r.create)
	desk.Handle(studio.PermClients, "GET /admin/api/audiences/{slug}", r.show)
	desk.Handle(studio.PermClients, "PUT /admin/api/audiences/{slug}", r.update)
	desk.Handle(studio.PermClients, "DELETE /admin/api/audiences/{slug}", r.drop)
	desk.Handle(studio.PermClients, "GET /admin/api/audiences/{slug}/size", r.size)
	desk.Handle(studio.PermClients, "GET /admin/api/audiences/{slug}/members", r.members)
	desk.Handle(studio.PermClients, "POST /admin/api/audiences/{slug}/members", r.addMember)
	desk.Handle(studio.PermClients, "DELETE /admin/api/audiences/{slug}/members/{account}", r.dropMember)
	desk.Handle(studio.PermClients, "GET /admin/api/clients/{account}/audiences", r.ofClient)

	desk.Handle(studio.PermPacks, "GET /admin/api/packs/{slug}/audiences", r.ofPack)
	desk.Handle(studio.PermPacks, "PUT /admin/api/packs/{slug}/audiences", r.setPack)
}

type routes struct {
	store *Store
}

// shown приводит группу к тому, что уезжает в студию.
//
// Одна работа на список и на карточку: разойдись они — карточка показала
// бы правило иначе, чем строка списка, и составитель поверил бы той,
// которую открыл последней.
func shown(one Group) map[string]any {
	rule := make([]map[string]any, 0, len(one.Rule))
	for _, c := range one.Rule {
		item := map[string]any{"trait": c.Trait}
		if c.N != 0 {
			item["n"] = c.N
		}
		if c.Over != "" {
			item["over"] = c.Over
		}
		if c.Not {
			item["not"] = true
		}
		rule = append(rule, item)
	}
	return map[string]any{
		"slug": one.Slug, "title": one.Title, "note": one.Note,
		"rule": rule, "members": one.Members, "broken": one.Broken,
	}
}

func (r *routes) list(w http.ResponseWriter, req *http.Request, _ studio.User) {
	all, err := r.store.All(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Группы не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(all))
	for _, one := range all {
		out = append(out, shown(one))
	}
	// Словарь уезжает вместе со списком: студия рисует по нему выбор
	// признаков и окон, и второй его редакции у неё быть не должно.
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"audiences": out, "traits": Traits, "windows": Windows,
	})
}

type groupRequest struct {
	Slug  string      `json:"slug"`
	Title string      `json:"title"`
	Note  string      `json:"note"`
	Rule  []Condition `json:"rule"`
}

func (r *routes) create(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body groupRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	if err := r.store.Create(req.Context(), body.Slug, body.Title, body.Note, body.Rule); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusCreated, map[string]any{"slug": body.Slug})
}

func (r *routes) show(w http.ResponseWriter, req *http.Request, _ studio.User) {
	one, err := r.store.One(req.Context(), req.PathValue("slug"))
	if err != nil {
		studio.WriteError(w, http.StatusNotFound, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, shown(one))
}

func (r *routes) update(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body groupRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	if err := r.store.Update(req.Context(), req.PathValue("slug"),
		body.Title, body.Note, body.Rule); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"slug": req.PathValue("slug")})
}

func (r *routes) drop(w http.ResponseWriter, req *http.Request, _ studio.User) {
	if err := r.store.Drop(req.Context(), req.PathValue("slug")); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"dropped": req.PathValue("slug")})
}

func (r *routes) size(w http.ResponseWriter, req *http.Request, _ studio.User) {
	count, err := r.store.Size(req.Context(), req.PathValue("slug"))
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"size": count})
}

func (r *routes) members(w http.ResponseWriter, req *http.Request, _ studio.User) {
	list, err := r.store.Members(req.Context(), req.PathValue("slug"))
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Состав группы не прочитан")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"account": one.AccountID, "email": one.Email,
			"addedBy": one.AddedBy, "addedAt": one.AddedAt,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"members": out})
}

type memberRequest struct {
	Account int64 `json:"account"`
}

func (r *routes) addMember(w http.ResponseWriter, req *http.Request, who studio.User) {
	var body memberRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	// Имя добавившего пишется в строку: спросят об этом ровно тогда,
	// когда врач скажет, что доступ у него откуда-то взялся.
	if err := r.store.AddMember(req.Context(), req.PathValue("slug"),
		body.Account, who.Login); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"account": body.Account})
}

func (r *routes) dropMember(w http.ResponseWriter, req *http.Request, _ studio.User) {
	account, err := strconv.ParseInt(req.PathValue("account"), 10, 64)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Номер врача — это число")
		return
	}
	if err := r.store.DropMember(req.Context(), req.PathValue("slug"), account); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"account": account})
}

func (r *routes) ofClient(w http.ResponseWriter, req *http.Request, _ studio.User) {
	account, err := strconv.ParseInt(req.PathValue("account"), 10, 64)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Номер врача — это число")
		return
	}
	list, err := r.store.Of(req.Context(), account, time.Now())
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{"slug": one.Slug, "title": one.Title})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"audiences": out})
}

func (r *routes) ofPack(w http.ResponseWriter, req *http.Request, _ studio.User) {
	list, err := r.store.OfPack(req.Context(), req.PathValue("slug"))
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Группы набора не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"slug": one.Slug, "title": one.Title, "mode": one.Mode,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"audiences": out})
}

type packRequest struct {
	Audiences []struct {
		Slug string `json:"slug"`
		Mode string `json:"mode"`
	} `json:"audiences"`
}

func (r *routes) setPack(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body packRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	bounds := make([]Bound, 0, len(body.Audiences))
	for _, one := range body.Audiences {
		bounds = append(bounds, Bound{Slug: one.Slug, Mode: one.Mode})
	}
	if err := r.store.SetPack(req.Context(), req.PathValue("slug"), bounds); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"audiences": len(bounds)})
}
