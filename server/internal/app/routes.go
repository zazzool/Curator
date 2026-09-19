package app

import (
	"net/http"
	"strconv"
	"time"
)

// Маршруты /v1.
//
// Каждый объявлен именем, называющим, кого он пускает: Open — всякого,
// Keyed — сборку с ключом программы, Device — устройство с токеном.
func Routes(door *Door, feed *Feed) {
	r := &routes{accounts: door.accounts, feed: feed}

	// Справка о службе открыта всякому: её спрашивает и приложение до
	// заведения устройства, и наблюдение снаружи. Ничего о враче она не
	// рассказывает.
	door.Open("GET /v1/health", r.health)

	// Заведение устройства — единственное, что закрыто ключом программы и
	// не закрыто токеном: токена в этот момент ещё нет.
	door.Keyed("POST /v1/devices", r.enroll)

	door.Device("GET /v1/me", r.me)
	door.Device("PUT /v1/me", r.rename)
	door.Device("DELETE /v1/devices/current", r.forget)

	door.Device("GET /v1/content/version", r.version)
	door.Device("GET /v1/cases", r.cases)
	door.Device("GET /v1/cases/{id}", r.oneCase)
}

type routes struct {
	accounts *Accounts
	feed     *Feed
}

func (r *routes) health(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type enrollRequest struct {
	About
}

// enroll заводит устройство, а с ним и учётную запись.
func (r *routes) enroll(w http.ResponseWriter, req *http.Request, _ string) {
	var body enrollRequest
	// Тело необязательно: сборка, не рассказавшая о себе ничего, обязана
	// завестись. Эти сведения нужны отчётам, а не работе.
	_ = DecodeBody(req, &body)

	device, err := r.accounts.Enroll(req.Context(), body.About)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло начать. Попробуйте ещё раз")
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{
		"token":     device.Token,
		"accountId": device.AccountID,
		"deviceId":  device.DeviceID,
	})
}

func (r *routes) me(w http.ResponseWriter, req *http.Request, caller Caller) {
	profile, err := r.accounts.Profile(req.Context(), caller.AccountID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло прочитать вашу запись")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"accountId":   profile.AccountID,
		"email":       profile.Email,
		"displayName": profile.DisplayName,
		"createdAt":   profile.CreatedAt.Format(time.RFC3339),
	})
}

type renameRequest struct {
	DisplayName string `json:"displayName"`
}

func (r *routes) rename(w http.ResponseWriter, req *http.Request, caller Caller) {
	var body renameRequest
	if err := DecodeBody(req, &body); err != nil {
		WriteError(w, http.StatusBadRequest, "Запрос не разобран")
		return
	}
	if err := r.accounts.Rename(req.Context(), caller.AccountID, body.DisplayName); err != nil {
		WriteError(w, http.StatusInternalServerError, "Имя не сохранилось. Попробуйте ещё раз")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"displayName": body.DisplayName})
}

func (r *routes) forget(w http.ResponseWriter, req *http.Request, caller Caller) {
	if err := r.accounts.Forget(req.Context(), caller.DeviceID); err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло выйти. Попробуйте ещё раз")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (r *routes) version(w http.ResponseWriter, req *http.Request, _ Caller) {
	version, err := r.feed.Version(req.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло свериться с сервером")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"version": version})
}

// cases отдаёт страницу ленты.
//
// Страницы курсором, а не смещением: между двумя страницами задачу могут
// выпустить, и при смещении она сдвинет все следующие — врач получит одну
// задачу дважды, а соседнюю не получит вовсе.
func (r *routes) cases(w http.ResponseWriter, req *http.Request, _ Caller) {
	q := req.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	page, err := r.feed.Page(req.Context(), Cursor{
		After: q.Get("after"),
		Path:  q.Get("path"),
		Limit: limit,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло получить задачи")
		return
	}
	WriteJSON(w, http.StatusOK, pageJSON(page))
}

func (r *routes) oneCase(w http.ResponseWriter, req *http.Request, _ Caller) {
	one, err := r.feed.Case(req.Context(), req.PathValue("id"))
	if err != nil {
		// Одинаковый ответ на «нет такой» и на «снята с раздачи»:
		// снятая задача для приложения не существует, а разные ответы
		// рассказали бы, что она была.
		WriteError(w, http.StatusNotFound, "Такой задачи нет")
		return
	}
	WriteJSON(w, http.StatusOK, caseJSON(one))
}
