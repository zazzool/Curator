package app

import (
	"net/http"
	"time"

	"curator/server/internal/telemetry"
)

// Дверь телеметрии.
//
// Отдельная от разборов намеренно: разбор — это то, что врач решил, и он
// обязан лечь. Событие — это то, как он при этом себя вёл, и потеря пачки
// событий стоит строки в отчёте, а не вечера работы. Смешай мы их в одной
// посылке — и отказ по событию уронил бы разборы.
func TelemetryRoutes(door *Door, store *telemetry.Store) {
	r := &telemetryRoutes{store: store, now: time.Now}
	door.Device("POST /v1/telemetry", r.record)
}

type telemetryRoutes struct {
	store *telemetry.Store
	now   func() time.Time
}

type telemetryRequest struct {
	Events []telemetry.Incoming `json:"events"`
}

func (r *telemetryRoutes) record(w http.ResponseWriter, req *http.Request, caller Caller) {
	var body telemetryRequest
	if err := DecodeBody(req, &body); err != nil {
		WriteError(w, http.StatusBadRequest, "Запрос не разобран")
		return
	}
	if len(body.Events) > 1000 {
		WriteError(w, http.StatusBadRequest,
			"Слишком много событий в одной посылке. Пошлите их частями")
		return
	}

	out, err := r.store.Record(req.Context(), caller.AccountID, body.Events, r.now())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "События не сохранились")
		return
	}
	// dropped возвращается намеренно: молча отброшенная половина пачки
	// выглядит в отчёте как «врач ничего не делал». Приложение пишет это
	// число в свой журнал, и по нему видно, что словарь сервера отстал от
	// сборки.
	WriteJSON(w, http.StatusOK, map[string]any{
		"accepted": out.Accepted,
		"repeated": out.Repeated,
		"dropped":  out.Dropped,
	})
}
