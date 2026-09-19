package app

import (
	"net/http"
	"strconv"
)

// Маршруты справочника.
//
// Отдельным файлом и отдельной парой ручек, а не полем в ленте задач:
// лента отдаёт то, что решают, справочник — то, что читают, и качаются
// они по разным поводам. Смешай их — и устройство, которому нужна одна
// страница ленты, тянуло бы за ней весь справочник.
func ReferenceRoutes(door *Door, ref *Reference) {
	r := &refRoutes{ref: ref}

	door.Device("GET /v1/reference", r.sources)
	door.Device("GET /v1/reference/{slug}/units", r.units)
	door.Device("GET /v1/reference/{slug}/statements", r.statements)
}

type refRoutes struct {
	ref *Reference
}

func (r *refRoutes) sources(w http.ResponseWriter, req *http.Request, _ Caller) {
	list, err := r.ref.Sources(req.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло получить список справочников")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, map[string]any{
			"slug":          s.Slug,
			"title":         s.Title,
			"unitWord":      s.UnitWord,
			"statementWord": s.StatementWord,
			"edition":       s.Edition,
			"units":         s.Units,
			"statements":    s.Statements,
			"version":       s.Version,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"sources": out})
}

func (r *refRoutes) units(w http.ResponseWriter, req *http.Request, _ Caller) {
	q := req.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	page, err := r.ref.Units(req.Context(), req.PathValue("slug"), q.Get("after"), limit)
	if err != nil {
		WriteError(w, http.StatusNotFound, "Такого справочника нет")
		return
	}

	units := make([]map[string]any, 0, len(page.Units))
	for _, u := range page.Units {
		units = append(units, map[string]any{
			"label":       u.Label,
			"parentLabel": u.ParentLabel,
			"title":       u.Title,
			"path":        u.Path,
			"depth":       u.Depth,
			"kind":        u.Kind,
			"answerable":  u.Answerable,
			"statements":  u.Statements,
			"ord":         u.Ord,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"units": units, "next": page.Next, "version": page.Version,
	})
}

func (r *refRoutes) statements(w http.ResponseWriter, req *http.Request, _ Caller) {
	q := req.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)

	page, err := r.ref.Statements(req.Context(), req.PathValue("slug"), after, limit)
	if err != nil {
		WriteError(w, http.StatusNotFound, "Такого справочника нет")
		return
	}

	statements := make([]map[string]any, 0, len(page.Statements))
	for _, s := range page.Statements {
		statements = append(statements, map[string]any{
			"id":          s.ID,
			"unitLabel":   s.UnitLabel,
			"kind":        s.Kind,
			"designation": s.Designation,
			"placeRef":    s.PlaceRef,
			"bodyMd":      s.Body,
			"ord":         s.Ord,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"statements": statements, "next": page.Next, "version": page.Version,
	})
}
