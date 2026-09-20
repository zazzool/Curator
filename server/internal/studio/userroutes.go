package studio

import (
	"errors"
	"net/http"
	"time"
)

// Пользователи студии в самой студии.
//
// До этих ручек нового составителя заводили на контуре руками, а право
// мастерской уже тогда называло пользователей своим делом. Ручка, которой
// нет, не безопаснее ручки под правом: она просто выносит ту же власть из
// студии в ssh, где ни журнала, ни отказа за последнего мастера.
func UserRoutes(d *Desk) {
	r := &userRoutes{users: d.users}

	d.Handle(PermWorkshop, "GET /admin/api/users", r.list)
	d.Handle(PermWorkshop, "POST /admin/api/users", r.create)
	d.Handle(PermWorkshop, "PUT /admin/api/users/{login}/permissions", r.setPermissions)
	d.Handle(PermWorkshop, "PUT /admin/api/users/{login}/disabled", r.setDisabled)
}

type userRoutes struct {
	users *Users
}

func (r *userRoutes) list(w http.ResponseWriter, req *http.Request, _ User) {
	list, err := r.users.All(req.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Пользователи не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"login":       one.Login,
			"displayName": one.DisplayName,
			"permissions": permStrings(one.Permissions),
			"disabled":    one.Disabled,
			"createdAt":   one.CreatedAt.Format(time.RFC3339),
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"users": out})
}

type createUserRequest struct {
	Login       string   `json:"login"`
	DisplayName string   `json:"displayName"`
	Permissions []string `json:"permissions"`
}

// create заводит пользователя и показывает секрет аутентификатора — тот
// единственный раз, когда его вообще можно увидеть.
func (r *userRoutes) create(w http.ResponseWriter, req *http.Request, _ User) {
	var body createUserRequest
	if err := DecodeBody(req, &body); err != nil {
		WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	perms, err := ParsePermissions(body.Permissions)
	if err != nil {
		WriteError(w, http.StatusBadRequest, Sentence(err.Error()))
		return
	}
	user, secret, err := r.users.Create(req.Context(), body.Login, body.DisplayName, perms)
	if err != nil {
		WriteError(w, http.StatusBadRequest, Sentence(err.Error()))
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{
		"login":  user.Login,
		"secret": secret,
		"note": "Передайте секрет человеку сейчас и сотрите: второй раз его " +
			"не покажет никто. Забывшему заводят вход заново.",
	})
}

type permissionsRequest struct {
	Permissions []string `json:"permissions"`
}

func (r *userRoutes) setPermissions(w http.ResponseWriter, req *http.Request, _ User) {
	var body permissionsRequest
	if err := DecodeBody(req, &body); err != nil {
		WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	perms, err := ParsePermissions(body.Permissions)
	if err != nil {
		WriteError(w, http.StatusBadRequest, Sentence(err.Error()))
		return
	}
	if err := r.users.SetPermissions(req.Context(), req.PathValue("login"), perms); err != nil {
		writeChangeError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"permissions": body.Permissions})
}

type disabledRequest struct {
	Disabled bool `json:"disabled"`
}

func (r *userRoutes) setDisabled(w http.ResponseWriter, req *http.Request, _ User) {
	var body disabledRequest
	if err := DecodeBody(req, &body); err != nil {
		WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	if err := r.users.SetDisabled(req.Context(), req.PathValue("login"), body.Disabled); err != nil {
		writeChangeError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"disabled": body.Disabled})
}

// writeChangeError отвечает на отказ правки.
//
// Последний мастер — отдельный ответ (409), а не обычные 400: это не
// негодный запрос, а верный запрос в негодный момент, и студии надо
// отличать одно от другого, чтобы сказать человеку не «исправьте поле», а
// «сначала дайте право второму».
func writeChangeError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrLastWorkshop) {
		WriteError(w, http.StatusConflict, Sentence(err.Error()))
		return
	}
	WriteError(w, http.StatusBadRequest, Sentence(err.Error()))
}
