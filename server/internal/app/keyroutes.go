package app

import (
	"net/http"

	"curator/server/internal/studio"
)

// Ключи программ в студии.
//
// Закрыты правом мастерской: ключ — это настройка службы, а не работа над
// задачами. Составитель, пишущий задачи, ключи не заводит и знать о них не
// должен.
func KeyRoutes(desk *studio.Desk, keys *Keys) {
	r := &keyRoutes{keys: keys}

	desk.Handle(studio.PermWorkshop, "GET /admin/api/app-keys", r.list)
	desk.Handle(studio.PermWorkshop, "POST /admin/api/app-keys", r.issue)
	desk.Handle(studio.PermWorkshop, "POST /admin/api/app-keys/{id}/disable", r.disable)
}

type keyRoutes struct {
	keys *Keys
}

func (r *keyRoutes) list(w http.ResponseWriter, req *http.Request, _ studio.User) {
	list, err := r.keys.All(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Ключи программ не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		// Секрет наружу не уезжает даже огрызком: он не хранится вовсе, а
		// поле «секрет» в ответе однажды кто-нибудь заполнит.
		out = append(out, map[string]any{
			"keyId":     one.KeyID,
			"title":     one.Title,
			"disabled":  one.Disabled,
			"createdAt": one.CreatedAt.Format(timeLayout),
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"keys": out})
}

type issueRequest struct {
	KeyID string `json:"keyId"`
	Title string `json:"title"`
}

// issue заводит ключ и отдаёт его целиком — единственный раз в его жизни.
func (r *keyRoutes) issue(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body issueRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	key, err := r.keys.Issue(req.Context(), body.KeyID, body.Title)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusCreated, map[string]any{
		"key": key,
		"note": "Сохраните ключ сейчас: второй раз его не покажет никто — " +
			"в базе лежит только отпечаток. Потерянный ключ заводится заново.",
	})
}

func (r *keyRoutes) disable(w http.ResponseWriter, req *http.Request, _ studio.User) {
	if err := r.keys.Disable(req.Context(), req.PathValue("id")); err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Ключ не выключен")
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

const timeLayout = "2006-01-02T15:04:05Z07:00"
