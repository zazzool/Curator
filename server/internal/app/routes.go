package app

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"curator/server/internal/limits"
	"curator/server/internal/progress"
	"curator/server/internal/sales"
)

// Маршруты /v1.
//
// Каждый объявлен именем, называющим, кого он пускает: Open — всякого,
// Keyed — сборку с ключом программы, Device — устройство с токеном.
func Routes(door *Door, feed *Feed, attempts *Attempts, access *sales.Access) {
	r := &routes{
		accounts: door.accounts, feed: feed, attempts: attempts,
		access: access, now: time.Now,
	}

	// Справка о службе открыта всякому: её спрашивает и приложение до
	// заведения устройства, и наблюдение снаружи. Ничего о враче она не
	// рассказывает.
	door.Open("GET /v1/health", r.health)

	// Заведение устройства — единственное, что закрыто ключом программы и
	// не закрыто токеном: токена в этот момент ещё нет.
	//
	// И оно же считается по адресу. Ключ программы лежит в сборке, то
	// есть у всякого, кто её разобрал, — пояснение к Keyed это признаёт
	// само. Без счёта первый же любопытный заводит нам столько учётных
	// записей, сколько выдержит база; запас в двадцать заведений покрывает
	// переустановку приложения и общий адрес за одним прокси, а пять в
	// минуту — это больше, чем врач заводит устройств за всю жизнь.
	door.Keyed("POST /v1/devices", Metered(
		limits.NewBucket(20, 5), time.Now,
		"Слишком много заведений с этого адреса. Попробуйте позже",
		r.enroll))

	door.Device("GET /v1/me", r.me)
	door.Device("PUT /v1/me", r.rename)
	door.Device("DELETE /v1/devices/current", r.forget)

	door.Device("GET /v1/content/version", r.version)
	door.Device("GET /v1/cases", r.cases)
	door.Device("GET /v1/cases/{id}", r.oneCase)

	door.Device("POST /v1/attempts", r.record)
	door.Device("GET /v1/progress", r.progress)
	door.Device("GET /v1/review", r.review)
	door.Device("GET /v1/signs", r.signs)
}

type routes struct {
	accounts *Accounts
	feed     *Feed
	attempts *Attempts

	// access — живые права врача. Нужны /v1/me: витрина говорит, куплен
	// ли отдельный набор, а «что у меня вообще есть и до когда» не
	// говорил никто. Врач, заплативший за подписку, видел ровно то же,
	// что и не заплативший, — и выяснять, дошли ли деньги, ему
	// приходилось у нас.
	access *sales.Access

	// now — источник времени. Полем, а не time.Now по месту: проверке
	// нужно подвинуть часы, чтобы увидеть подошедший срок повторения, а
	// ждать сутки она не может.
	now func() time.Time
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
	// Права читаются отдельно и их отказ не роняет ответ: кто я такой —
	// сведения о самом враче, а права о том, что он купил. Уроним ответ
	// целиком — и приложение не покажет даже имени.
	//
	// Но и молчать об отказе нельзя. Пустой список прав значит «ничего не
	// куплено», и оплативший врач при отказе чтения видел ровно то же,
	// что неоплативший, — а приложение видело то же самое и закрывало
	// ему платное. Поэтому рядом со списком едет признак, прочитались ли
	// права вообще: пусто и «неизвестно» — разные вещи, и врачу о них
	// говорят по-разному.
	rights := []map[string]any{}
	rightsKnown := r.access == nil
	if r.access != nil {
		live, err := r.access.Live(req.Context(), caller.AccountID, r.now())
		if err != nil {
			log.Printf("/v1/me: права врача %d не прочитаны: %v", caller.AccountID, err)
		} else {
			rightsKnown = true
			for _, one := range live {
				row := map[string]any{
					"kind": one.Kind, "pack": one.Pack, "origin": one.Origin,
					"startsAt": one.StartsAt.Format(time.RFC3339),
					// Пусто — бессрочно. Отсутствие поля приложение
					// прочитало бы как «старый сервер», а пустая строка
					// говорит то, что есть: срока нет.
					"expiresAt": "",
				}
				if one.ExpiresAt != nil {
					row["expiresAt"] = one.ExpiresAt.Format(time.RFC3339)
				}
				rights = append(rights, row)
			}
		}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"accountId":   profile.AccountID,
		"email":       profile.Email,
		"displayName": profile.DisplayName,
		"createdAt":   profile.CreatedAt.Format(time.RFC3339),
		"rights":      rights,
		// Поле добавлено, а не переосмыслено существующее: сборки на
		// руках его не знают и продолжат читать один «rights», как
		// читали. Истина у них при отказе чтения прав будет прежней —
		// неверной, — и это единственное, чего исправить нельзя.
		"rightsKnown": rightsKnown,
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

type recordRequest struct {
	Attempts []Attempt `json:"attempts"`
}

// record принимает пачку попыток.
//
// Пачкой, а не по одной: приложение работает офлайн и досылает вечером
// всё, что накопилось за день. Посылка по одной означала бы сотню
// обращений подряд с телефона в метро — то есть сотню поводов оборваться.
func (r *routes) record(w http.ResponseWriter, req *http.Request, caller Caller) {
	var body recordRequest
	if err := DecodeBody(req, &body); err != nil {
		WriteError(w, http.StatusBadRequest, "Запрос не разобран")
		return
	}
	if len(body.Attempts) > 500 {
		// Потолок назван вслух и отказывает, а не молча обрезает:
		// обрезанная пачка означает потерянный разбор, и врач об этом не
		// узнает. Пятисот хватает на любой разумный офлайн.
		WriteError(w, http.StatusBadRequest,
			"Слишком много разборов в одной посылке. Пошлите их частями")
		return
	}

	out, err := r.attempts.Record(req.Context(), caller.AccountID, body.Attempts, r.now())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Разборы не сохранились. Попробуйте ещё раз")
		return
	}
	// Знаки, выданные этой пачкой, — всегда список, пустой как []:
	// пачка, никого не наградившая, это обычный случай, а не особый.
	awarded := make([]map[string]any, 0, len(out.Signs))
	for _, one := range out.Signs {
		awarded = append(awarded, map[string]any{
			"slug": one.Slug, "title": one.Title, "serial": one.Serial, "xp": one.XP,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"accepted": out.Accepted,
		"repeated": out.Repeated,
		"xp":       out.XP,
		"level":    out.Level,
		"due":      out.Due,
		"metrics":  metricsJSON(out.Metrics),
		"signs":    awarded,
	})
}

func (r *routes) progress(w http.ResponseWriter, req *http.Request, caller Caller) {
	out, err := r.attempts.Progress(req.Context(), caller.AccountID, r.now())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло прочитать ваш прогресс")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"xp": out.XP, "level": out.Level, "due": out.Due,
		"metrics": metricsJSON(out.Metrics),
	})
}

// review отдаёт задачи, которым пришёл срок повторения.
func (r *routes) review(w http.ResponseWriter, req *http.Request, caller Caller) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	list, err := r.attempts.Due(req.Context(), caller.AccountID, r.now(), limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло получить задачи к повторению")
		return
	}
	// Пустой список — [], а не null: «сегодня нечего повторять» — исправный
	// случай, самый частый из всех.
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"id":    one.ID,
			"body":  json.RawMessage(one.Body),
			"dueAt": one.DueAt.Format(time.RFC3339),
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"cases": out})
}

// metricsJSON — величины каталога, какими их видит приложение.
//
// Объект всегда полон: каталог перечисляется целиком, и у врача, ещё
// ничего не решавшего, приезжают нули, а не пустой объект. Пустой объект
// заставил бы приложение помнить, что ключа может не быть, — и однажды оно
// забудет, причём на исправном случае: на новом враче.
func metricsJSON(m Metrics) map[string]int64 {
	out := make(map[string]int64, len(progress.Metrics()))
	for _, one := range progress.Metrics() {
		out[string(one.Key)] = m[one.Key]
	}
	return out
}

// signs отдаёт каталог знаков.
//
// Каталог целиком, вместе с невыданными: знак, о котором врач не знает, не
// мотивирует никого. Доля обладателей считается здесь, а не на устройстве:
// из собственной истории врача её не вывести вовсе.
func (r *routes) signs(w http.ResponseWriter, req *http.Request, caller Caller) {
	list, err := r.attempts.Signs(req.Context(), caller.AccountID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Не вышло получить ваши знаки")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		row := map[string]any{
			"slug":         one.Slug,
			"title":        one.Title,
			"kind":         string(one.Kind),
			"issued":       one.Issued,
			"progress":     one.Progress,
			"issuedCount":  one.IssuedCount,
			"editionSize":  one.EditionSize,
			"holdersShare": one.HoldersShare,
			"serial":       one.Serial,
			"issuedAt":     "",
			"revoked":      one.Revoked,
		}
		if one.Issued {
			row["issuedAt"] = one.IssuedAt.Format(time.RFC3339)
		}
		out = append(out, row)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"signs": out})
}
