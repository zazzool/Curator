package rules

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"curator/server/internal/studio"
)

// Ручки свода.
//
// Право — то же, что у заданий модели, и это не экономия на словаре.
// Свод уходит в задание блоком: правя правило, составитель решает,
// какими будут все будущие задачи по этому источнику, — ровно та власть,
// ради которой право заданий и отделено от права заказать одну задачу.
// Завести здесь второе право значило бы раздать ту же власть дважды и
// потом гадать, какое из двух её на самом деле держит.
func Routes(desk *studio.Desk, store *Store, talker Talker) {
	r := &routes{store: store, talker: talker}

	desk.Handle(studio.PermPrompts, "GET /admin/api/rules", r.list)
	// Каталог предикатов отдаётся отдельной ручкой, а не вкладывается в
	// список правил: он не меняется от правила к правилу, а список
	// перечитывается на каждую правку — семь описаний ездили бы туда и
	// обратно без нужды. И главное, он нужен ДО того, как правило
	// появилось: форма нового правила строится по нему.
	desk.Handle(studio.PermPrompts, "GET /admin/api/rules/checks", r.checks)
	desk.Handle(studio.PermPrompts, "POST /admin/api/rules", r.create)
	desk.Handle(studio.PermPrompts, "PUT /admin/api/rules/{id}", r.save)

	// Уплотнение — две ручки, и обе POST. Предложение плана тратит
	// деньги у поставщика моделей, а GET браузер и прокси вправе
	// повторить сами: страница, открытая дважды, платила бы дважды.
	desk.Handle(studio.PermPrompts, "POST /admin/api/rules/compaction", r.suggestCompaction)
	desk.Handle(studio.PermPrompts, "POST /admin/api/rules/compaction/apply", r.applyCompaction)
}

type routes struct {
	store  *Store
	talker Talker
}

// ruleRequest — что составителю позволено назвать.
//
// Полей ровно столько, и это не забывчивость. Счётчика подтверждений
// здесь нет: он считается работой, а не назначается рукой, иначе кворум
// перестал бы что-либо значить. Нет и источника: правило, написанное
// здесь, написано составителем по определению, и позволить назвать его
// «встроенным» значило бы дать ему чужое старшинство при споре.
type ruleRequest struct {
	Title  string `json:"title"`
	Text   string `json:"text"`
	Why    string `json:"why"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Scope  Scope  `json:"scope"`

	// Check — проверка правила, и состояний у поля ТРИ, а не два.
	//
	// Поля нет вовсе — проверку не трогали: правка формулировки не
	// должна снимать проверку, которую никто не снимал. Пришло null —
	// проверку снимают намеренно. Пришла запись — ставят её.
	//
	// Поэтому сырой JSON, а не *Check: разобранный в указатель, «нет
	// поля» и «null» становятся одним и тем же nil, и правка текста
	// правила молча снимала бы его проверку.
	Check json.RawMessage `json:"check"`
}

// checkEdit разбирает присланную проверку.
//
// Отвечает тремя величинами: что поставить, снимать ли, и отказ. Отказ
// здесь ОБЯЗАН быть словами: свод выбрасывает негодную проверку сам
// (Rule.Normalize), и это верно для проверки, пришедшей из ввоза или от
// модели, — непонятое не применяется. Но составителю, заполнившему
// форму, тот же выброс ответил бы успехом и оставил правило без
// проверки: он видел бы в списке «проверяется машинно» ровно до
// перечитывания страницы.
func checkEdit(raw json.RawMessage) (set *Check, clear bool, complaint string) {
	if len(raw) == 0 {
		return nil, false, ""
	}
	if string(raw) == "null" {
		return nil, true, ""
	}
	var check Check
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&check); err != nil {
		return nil, false, "проверка не разобрана: " + err.Error()
	}
	// Незнакомое ловится ДО причёсывания: Normalize выбрасывает его
	// молча, и составитель получил бы проверку, которая меряет не то.
	if u := check.Unknown(); u != "" {
		return nil, false, u
	}
	check.Normalize()
	if c := check.Complaint(); c != "" {
		return nil, false, c
	}
	return &check, false, ""
}

func (r *routes) list(w http.ResponseWriter, req *http.Request, _ studio.User) {
	list, err := r.store.All(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, studio.Sentence(err.Error()))
		return
	}
	// Пустой свод — пустой список, а не null: студия ходит по нему циклом
	// и падает белым экраном ровно на исправном случае — на новой
	// установке, где правил ещё нет.
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, ruleJSON(one))
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"rules": out})
}

func (r *routes) create(w http.ResponseWriter, req *http.Request, user studio.User) {
	var body ruleRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	// Опознаватель даёт сервер, а не студия: правило, названное составителем
	// «builtin:show-dont-name», встало бы на место встроенного.
	rule := Rule{
		ID:     fmt.Sprintf("curator:%s:%d", user.Login, r.store.now().UnixNano()),
		Title:  body.Title,
		Text:   body.Text,
		Why:    body.Why,
		Kind:   Kind(strings.TrimSpace(body.Kind)),
		Source: FromCurator,
		Scope:  body.Scope,
		Status: Active,
		// Написанное составителем действует с этой минуты и кворума не
		// ждёт: он и есть подтверждение. Признак назначенного человеком
		// стоит здесь же, чтобы пересчёт по кворуму не увёл правило в
		// кандидаты, ответив при этом успехом.
		Pinned: true,
	}
	check, _, complaint := checkEdit(body.Check)
	if complaint != "" {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(complaint))
		return
	}
	rule.Check = check
	saved, err := r.store.Save(req.Context(), rule)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, ruleJSON(saved))
}

// save правит существующее правило.
//
// Счётчики, задания-подтверждения и даты правило приносит с собой из
// базы: правка текста не должна обнулять того, что накоплено работой.
// Прежде тут стояла сборка правила из одного запроса, и она молча
// сбрасывала подтверждения — правило, дозревшее за неделю, после первой
// же правки формулировки возвращалось в кандидаты.
func (r *routes) save(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body ruleRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	id := req.PathValue("id")
	list, err := r.store.All(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, studio.Sentence(err.Error()))
		return
	}
	var rule *Rule
	for i := range list {
		if list[i].ID == id {
			rule = &list[i]
		}
	}
	if rule == nil {
		studio.WriteError(w, http.StatusNotFound, "Такого правила в своде нет.")
		return
	}

	check, clear, complaint := checkEdit(body.Check)
	if complaint != "" {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(complaint))
		return
	}

	rule.Title = body.Title
	rule.Text = body.Text
	rule.Why = body.Why
	rule.Kind = Kind(strings.TrimSpace(body.Kind))
	rule.Scope = body.Scope
	switch {
	case clear:
		rule.Check = nil
	case check != nil:
		rule.Check = check
	}
	if s := Status(strings.TrimSpace(body.Status)); s != "" && s != rule.Status {
		rule.Status = s
		// Состояние, названное рукой, счётчик больше не пересчитывает —
		// иначе включённое правило молча вернулось бы в кандидаты.
		rule.Pinned = true
	}

	saved, err := r.store.Save(req.Context(), *rule)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, ruleJSON(saved))
}

// checks отдаёт закрытый каталог предикатов и словари их доводов.
//
// Вместе, а не порознь: форма строится по обоим сразу, и список «куда
// смотреть», приехавший отдельно от предикатов, разошёлся бы с ними на
// том предикате, который добавили последним.
func (r *routes) checks(w http.ResponseWriter, _ *http.Request, _ studio.User) {
	specs := make([]map[string]any, 0, len(Catalog()))
	for _, spec := range Catalog() {
		specs = append(specs, map[string]any{
			"type":   string(spec.Type),
			"title":  spec.Title,
			"about":  spec.About,
			"params": spec.Params,
			// Составителю предикат доступен любой — он и есть тот, кому
			// «только составитель» разрешает. Признак уезжает всё равно:
			// форма подписывает им поле, чтобы правящий видел, что
			// модель этого предиката не получит.
			"doctorOnly": spec.DoctorOnly,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"checks": specs,
		// Словари закрыты, и студия их не повторяет: повторённый список
		// разошёлся бы молча — ровно на том значении, которое добавили
		// последним, — и составитель выбрал бы то, чего исполнитель не
		// знает.
		"where": []map[string]string{
			{"value": FieldSegments, "word": "фрагменты условия"},
			{"value": FieldTitle, "word": "заголовок"},
			{"value": FieldExplanation, "word": "разбор"},
		},
		"what": []map[string]string{
			{"value": CountSegments, "word": CountTitle(CountSegments)},
			{"value": CountOptions, "word": CountTitle(CountOptions)},
			{"value": CountMarked, "word": CountTitle(CountMarked)},
		},
		"gender": []map[string]string{
			{"value": GenderMale, "word": "условие о мужчине"},
			{"value": GenderFemale, "word": "условие о женщине"},
		},
	})
}

// ruleJSON — правило, каким его видит студия.
//
// Собирается структурой полей по одной, а не отдачей Rule целиком: поле,
// добавленное во внутреннюю запись, попало бы в ответ само собой, и
// проводной формат менялся бы, когда никто этого не решал.
func ruleJSON(r Rule) map[string]any {
	jobs := r.SeenJobs
	if jobs == nil {
		jobs = []int64{}
	}
	out := map[string]any{
		"id":       r.ID,
		"title":    r.Title,
		"text":     r.Text,
		"why":      r.Why,
		"kind":     string(r.Kind),
		"kindWord": KindWord(r.Kind),
		"source":   string(r.Source),
		"status":   string(r.Status),
		"pinned":   r.Pinned,
		"scope":    r.Scope,
		// Проверка отдаётся и записью, и фразой. Фраза считается ЗДЕСЬ,
		// а не в студии: собери её там — и составитель читал бы в списке
		// одно, а в журнале другое, разойдясь на первой же правке
		// формулировки.
		"check":         r.Check,
		"checkWords":    Describe(r.Check),
		"confirmations": r.Confirmations,
		"seenJobs":      jobs,
		"quorum":        Quorum,
		"validFrom":     r.ValidFrom,
		"updatedAt":     r.UpdatedAt,
	}
	if r.ValidTo != nil {
		out["validTo"] = r.ValidTo
	}
	if r.MergedInto != "" {
		out["mergedInto"] = r.MergedInto
	}
	if !r.LastSeenAt.IsZero() {
		out["lastSeenAt"] = r.LastSeenAt
	}
	return out
}

// compactionRequest — план, вернувшийся из браузера.
//
// Составитель мог отклонить часть групп, и потому принятый план приходит
// обратно списком. Дописать себе прав он при этом не может: список
// проходит тот же Sanitize, что и пришедший от модели, — права
// проверяются здесь, а не там, где план составляли.
type compactionRequest struct {
	Groups []CompactionGroup `json:"groups"`
}

// suggestCompaction просит у модели план уплотнения.
func (r *routes) suggestCompaction(w http.ResponseWriter, req *http.Request, _ studio.User) {
	if r.talker == nil {
		// Не отказ студии, а объяснение: без ключа поставщика уплотнять
		// некому, и сказать об этом словами полезнее, чем спрятать
		// кнопку. Спрятанную кнопку составитель принимает за поломку.
		studio.WriteError(w, http.StatusNotImplemented,
			"Модель не настроена: уплотнять свод некому.")
		return
	}
	book, err := r.store.Book(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, studio.Sentence(err.Error()))
		return
	}
	// Модель не названа: уплотнение идёт умолчальной моделью поставщика.
	// Выбор модели на каждый узел — отдельная работа, и завести здесь
	// своё поле раньше неё значило бы поселить выбор во втором месте.
	plan, err := SuggestCompaction(req.Context(), r.talker, "", book)
	if err != nil {
		studio.WriteError(w, http.StatusBadGateway, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, plan)
}

// applyCompaction проводит принятый составителем план по своду.
func (r *routes) applyCompaction(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body compactionRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	book, err := r.store.Book(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, studio.Sentence(err.Error()))
		return
	}
	safe := Sanitize(body.Groups, book)

	done, failed, err := r.store.ApplyCompaction(req.Context(), safe, r.store.now())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, studio.Sentence(err.Error()))
		return
	}

	// «Сколько просили» и «сколько прошло» отдаются парой. Отдай мы одно
	// «применено 3», и составитель, пославший пять групп, не узнал бы,
	// что две отвергнуты заслоном, — и решил бы, что свод уплотнён.
	after, err := r.store.Book(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, studio.Sentence(err.Error()))
		return
	}
	bytesAfter, _ := blockBytes(after.inForce())
	if failed == nil {
		failed = []string{}
	}
	list, err := r.store.All(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, studio.Sentence(err.Error()))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, one := range list {
		out = append(out, ruleJSON(one))
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"asked":   len(body.Groups),
		"merged":  done,
		"failed":  failed,
		"before":  safe.Before,
		"after":   bytesAfter,
		"limit":   BlockLimit(),
		"dropped": after.Dropped(),
		"rules":   out,
	})
}
