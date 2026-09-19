package app

import (
	"encoding/json"
	"net/http"
	"time"

	"curator/server/internal/packs"
	"curator/server/internal/sales"
)

// Ручки пакетов: витрина и скачивание.
//
// # Открытого ключа здесь не отдаётся
//
// Выпуск несёт подпись и имя ключа, которым он подписан, — но не сам
// ключ. Отдай мы ключ той же дверью, что и пакет, подпись перестала бы
// значить что-либо: тот, кто подменил пакет по дороге, подменил бы и ключ.
// Ключи, которым верит приложение, приезжают в его сборке; имя ключа нужно
// лишь затем, чтобы выбрать верный из них при смене.
func PackRoutes(door *Door, store *packs.Store, access *sales.Access, prices *sales.Prices) {
	r := &packRoutes{store: store, access: access, prices: prices}
	door.Device("GET /v1/packs", r.shelf)
	door.Device("GET /v1/packs/{slug}", r.release)
	door.Device("GET /v1/packs/{slug}/cases", r.bodies)
}

type packRoutes struct {
	store  *packs.Store
	access *sales.Access
	prices *sales.Prices
}

// shelf отдаёт витрину.
//
// Цена и право приезжают тем же ответом: витрина, не говорящая, что уже
// куплено, заставляет врача выяснять это покупкой. Цена в КОПЕЙКАХ —
// рубли дело показа, и переводить их здесь значит завести дробное число
// там, где его быть не должно.
func (r *packRoutes) shelf(w http.ResponseWriter, req *http.Request, caller Caller) {
	list, err := r.store.Shelf(req.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло получить наборы задач")
		return
	}
	live, err := r.prices.Live(req.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло получить цены")
		return
	}

	now := time.Now()
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		kopecks := live["pack:"+one.Slug]
		owned, err := r.access.Allowed(req.Context(), caller.AccountID, one.Slug, now)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "Не вышло проверить доступ")
			return
		}
		out = append(out, map[string]any{
			"slug": one.Slug, "title": one.Title, "summaryMd": one.SummaryMd,
			"version": one.Version, "cases": one.Cases,
			"kopecks": kopecks, "owned": owned,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"packs": out})
}

func (r *packRoutes) release(w http.ResponseWriter, req *http.Request, _ Caller) {
	one, err := r.store.Latest(req.Context(), req.PathValue("slug"))
	if err != nil {
		WriteError(w, http.StatusNotFound, "Такого набора нет")
		return
	}
	cases := make([]map[string]any, 0, len(one.Manifest.Cases))
	for _, item := range one.Manifest.Cases {
		cases = append(cases, map[string]any{
			"id": item.ID, "ord": item.Ord, "hash": item.Hash,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"slug":       one.Slug,
		"version":    one.Version,
		"title":      one.Manifest.Title,
		"releasedAt": one.Manifest.ReleasedAt.UTC().Format(time.RFC3339),
		"signature":  one.Signature,
		"keyId":      one.KeyID,
		"cases":      cases,
	})
}

// bodies отдаёт содержание задач набора страницей.
//
// Страницей, а не целиком: набор на тысячу задач весит мегабайты, и
// телефон в метро не дотянет одну большую закачку. Оборвавшаяся закачка
// продолжается с курсора, а не начинается заново.
func (r *packRoutes) bodies(w http.ResponseWriter, req *http.Request, caller Caller) {
	q := req.URL.Query()
	limit := 0
	if raw := q.Get("limit"); raw != "" {
		limit = atoi(raw)
	}
	// Право проверяется на содержании, а не на описи. Опись — это
	// номера и отпечатки, по ней не позанимаешься; закрыв её, мы лишили бы
	// витрину возможности показать, что внутри, и ничего не защитили.
	slug := req.PathValue("slug")
	allowed, err := r.access.Allowed(req.Context(), caller.AccountID, slug, time.Now())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло проверить доступ")
		return
	}
	if !allowed {
		// Отдельный код и разговорный текст: врач должен понять, что
		// набор надо купить, а не что приложение сломалось.
		WriteError(w, http.StatusPaymentRequired,
			"Этот набор пока не открыт. Купите его или подписку — и он появится")
		return
	}

	list, next, err := r.store.Bodies(req.Context(), slug, q.Get("after"), limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло получить задачи набора")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"id":   one.ID,
			"body": json.RawMessage(one.Body),
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"cases": out, "next": next})
}

func atoi(raw string) int {
	var out int
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0
		}
		out = out*10 + int(r-'0')
		if out > 1000 {
			return 1000
		}
	}
	return out
}
