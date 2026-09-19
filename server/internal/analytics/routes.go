package analytics

import (
	"net/http"
	"strconv"
	"time"

	"curator/server/internal/studio"
)

// Ручки отчётов. Все под правом analytics: читать, как ведут себя врачи, —
// отдельное занятие, и давать его всякому, кто правит задачи, незачем.
func Routes(desk *studio.Desk, rollup *Rollup) {
	r := &routes{rollup: rollup}
	desk.Handle(studio.PermAnalytics, "GET /admin/api/reports/cases", r.cases)
	desk.Handle(studio.PermAnalytics, "GET /admin/api/reports/events", r.events)
	desk.Handle(studio.PermAnalytics, "GET /admin/api/reports/cases/{id}", r.one)
}

type routes struct {
	rollup *Rollup
}

// cases отдаёт крайние задачи: слишком лёгкие и неразрешимые.
func (r *routes) cases(w http.ResponseWriter, req *http.Request, _ studio.User) {
	q := req.URL.Query()
	easy := float64byQuery(q.Get("easyAbove"), 0.95)
	hard := float64byQuery(q.Get("hardBelow"), 0.25)
	limit, _ := strconv.Atoi(q.Get("limit"))

	out, err := r.rollup.Rank(req.Context(), easy, hard, limit)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Отчёт не построен")
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"floor": Floor,
		"easy":  statsJSON(out.Easy),
		"hard":  statsJSON(out.Hard),
	})
}

// one отдаёт сводку по одной задаче — вместе с путаницей вариантов.
func (r *routes) one(w http.ResponseWriter, req *http.Request, _ studio.User) {
	stats, found, err := r.rollup.Get(req.Context(), req.PathValue("id"))
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Сводка не прочитана")
		return
	}
	if !found {
		// Не отказ «нет такой задачи»: задача есть, попыток по ней нет.
		// Пустая сводка честнее отказа — по отказу составитель пойдёт
		// искать задачу, которая на месте.
		studio.WriteJSON(w, http.StatusOK, map[string]any{
			"caseId": req.PathValue("id"), "attempts": 0, "correct": 0,
			"solveRate": 0, "medianMs": 0, "confusion": map[string]int64{},
			"origin": "", "enough": false,
		})
		return
	}
	studio.WriteJSON(w, http.StatusOK, statsJSON([]Stats{stats})[0])
}

func (r *routes) events(w http.ResponseWriter, req *http.Request, _ studio.User) {
	q := req.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	if days <= 0 || days > 180 {
		days = 7
	}
	until := time.Now()
	since := until.AddDate(0, 0, -days)

	list, err := r.rollup.Funnel(req.Context(), since, until)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Отчёт не построен")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"name": one.Name, "title": one.Title,
			"count": one.Count, "accounts": one.Accounts,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"days": days, "events": out,
	})
}

func statsJSON(list []Stats) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"caseId": one.CaseID, "attempts": one.Attempts, "correct": one.Correct,
			"solveRate": one.SolveRate, "medianMs": one.MedianMs,
			"confusion": one.Confusion, "origin": one.Origin,
			// enough говорит, хватает ли попыток, чтобы решаемости верить.
			// Без него составитель прочтёт долю у задачи с тремя попытками
			// как приговор ей.
			"enough": one.Attempts >= Floor,
		})
	}
	return out
}

func float64byQuery(raw string, fallback float64) float64 {
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 || v > 1 {
		return fallback
	}
	return v
}
