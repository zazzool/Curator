package sales

import (
	"net/http"
	"strconv"
	"time"

	"curator/server/internal/studio"
)

// Ручки продаж в студии.
//
// Право стоит первым доводом у каждой. Цены и приход закрыты правом sales,
// а платежи и права одного врача — правом clients: смотреть карточку
// клиента и менять цены это разные занятия разных людей.
func Routes(desk *studio.Desk, payments *Payments, prices *Prices, access *Access, clients *Clients) {
	r := &routes{payments: payments, prices: prices, access: access, clients: clients}

	desk.Handle(studio.PermSales, "GET /admin/api/prices", r.listPrices)
	desk.Handle(studio.PermSales, "PUT /admin/api/prices", r.setPrice)
	desk.Handle(studio.PermSales, "POST /admin/api/payments", r.accept)
	desk.Handle(studio.PermSales, "POST /admin/api/payments/{id}/refund", r.refund)

	desk.Handle(studio.PermClients, "GET /admin/api/clients", r.findClients)
	desk.Handle(studio.PermClients, "GET /admin/api/clients/{id}", r.showClient)
	desk.Handle(studio.PermClients, "PUT /admin/api/clients/{id}/blocked", r.setBlocked)
	desk.Handle(studio.PermClients, "GET /admin/api/clients/{id}/payments", r.clientPayments)
	desk.Handle(studio.PermClients, "GET /admin/api/clients/{id}/entitlements", r.rights)
}

type routes struct {
	payments *Payments
	prices   *Prices
	access   *Access
	clients  *Clients
}

type priceRequest struct {
	Purpose string `json:"purpose"`
	Kopecks int64  `json:"kopecks"`
	Enabled bool   `json:"enabled"`
}

// setPrice задаёт цену.
//
// Принимаются копейки, а не рубли: рубль с копейками, приехавший дробным
// числом, теряет копейку на первом же переводе в двоичную дробь. Рубли —
// дело витрины, и перевод делается при показе.
func (r *routes) setPrice(w http.ResponseWriter, req *http.Request, user studio.User) {
	var body priceRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	if err := r.prices.Set(req.Context(), user.Login, body.Purpose, body.Kopecks, body.Enabled); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"purpose": body.Purpose, "kopecks": body.Kopecks, "enabled": body.Enabled,
	})
}

func (r *routes) listPrices(w http.ResponseWriter, req *http.Request, _ studio.User) {
	list, err := r.prices.All(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Цены не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"purpose": one.Purpose, "kopecks": one.Kopecks, "enabled": one.Enabled,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"prices": out})
}

type incomeRequest struct {
	AccountID int64  `json:"accountId"`
	Purpose   string `json:"purpose"`
	Kopecks   int64  `json:"kopecks"`
	IdemKey   string `json:"idemKey"`
	Note      string `json:"note"`
}

// accept оформляет приход.
//
// Имя оформившего берётся из сессии, а не из тела запроса: приход,
// подтверждённый вне системы, проверяется только разговором с тем, кто его
// принял, и подписаться чужим именем тут не должно быть возможности.
func (r *routes) accept(w http.ResponseWriter, req *http.Request, user studio.User) {
	var body incomeRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	out, err := r.payments.Accept(req.Context(), Income{
		AccountID: body.AccountID, Purpose: body.Purpose, Kopecks: body.Kopecks,
		IdemKey: body.IdemKey, Note: body.Note, By: user.Login,
	}, time.Now())
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	status := http.StatusCreated
	if out.Repeated {
		// Не отказ: оператор нажал дважды, а не принял деньги дважды.
		// Двухсотый с прежним платежом говорит ему, что приход уже
		// оформлен, и удерживает от второй попытки другим ключом.
		status = http.StatusOK
	}
	studio.WriteJSON(w, status, map[string]any{
		"id": out.ID, "purpose": out.Purpose, "kopecks": out.Kopecks,
		"status": out.Status, "repeated": out.Repeated,
	})
}

type refundRequest struct {
	Note string `json:"note"`
}

func (r *routes) refund(w http.ResponseWriter, req *http.Request, user studio.User) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		studio.WriteError(w, http.StatusBadRequest, "Номер платежа в адресе не разобран")
		return
	}
	var body refundRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	if err := r.payments.Refund(req.Context(), id, body.Note, user.Login, time.Now()); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"status": "refunded"})
}

func (r *routes) clientPayments(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	list, err := r.payments.Recent(req.Context(), id, 0)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Платежи не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, map[string]any{
			"id": one.ID, "source": one.Source, "purpose": one.Purpose,
			"kopecks": one.Kopecks, "status": one.Status, "note": one.Note,
			"by": one.By, "at": one.At.Format(time.RFC3339),
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"payments": out})
}

func (r *routes) rights(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	list, err := r.access.Live(req.Context(), id, time.Now())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Права не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		row := map[string]any{
			"kind": one.Kind, "pack": one.Pack, "origin": one.Origin,
			"startsAt": one.StartsAt.Format(time.RFC3339), "expiresAt": "",
		}
		if one.ExpiresAt != nil {
			row["expiresAt"] = one.ExpiresAt.Format(time.RFC3339)
		}
		out = append(out, row)
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"entitlements": out})
}

func pathID(w http.ResponseWriter, req *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		studio.WriteError(w, http.StatusBadRequest, "Номер в адресе не разобран")
		return 0, false
	}
	return id, true
}

// findClients ищет клиентов, говоря, если показал не всех.
//
// Молчаливое отсечение здесь дороже, чем кажется: оператор ищет врача по
// куску фамилии, видит пятьдесят однофамильцев без пятьдесят первого и
// заводит вторую учётную запись тому, у кого она есть. Поэтому рядом со
// списком едет и предел, и признак того, что подошедших больше.
func (r *routes) findClients(w http.ResponseWriter, req *http.Request, _ studio.User) {
	asked, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	limit := ClientsShown(asked)

	// На одного больше предела: так «их ровно столько» и «их больше»
	// различаются без второго запроса со счётом.
	list, err := r.clients.Find(req.Context(), req.URL.Query().Get("q"), limit+1)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Клиенты не прочитаны")
		return
	}
	more := len(list) > limit
	if more {
		list = list[:limit]
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, clientJSON(one))
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"clients": out,
		"limit":   limit,
		"more":    more,
	})
}

func (r *routes) showClient(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	one, err := r.clients.One(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusNotFound, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, clientJSON(one))
}

type blockedRequest struct {
	Blocked bool `json:"blocked"`
}

func (r *routes) setBlocked(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	var body blockedRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	if err := r.clients.SetBlocked(req.Context(), id, body.Blocked, time.Now()); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"blocked": body.Blocked})
}

// clientJSON — одно место на список и карточку: разойдись они, оператор
// увидел бы в списке одно, а открыв — другое, и не понял бы, что смотрит
// на того же человека.
func clientJSON(one Client) map[string]any {
	row := map[string]any{
		"id": one.ID, "email": one.Email, "displayName": one.DisplayName,
		"createdAt": one.CreatedAt.Format(time.RFC3339), "lastSeen": "",
		"blocked": one.Blocked, "devices": one.Devices, "rights": one.Rights,
	}
	if one.LastSeen != nil {
		row["lastSeen"] = one.LastSeen.Format(time.RFC3339)
	}
	return row
}
