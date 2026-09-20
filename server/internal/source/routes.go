package source

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"curator/server/internal/studio"
)

// Routes объявляет ручки этапа источника.
//
// Право стоит первым доводом у каждой: другого способа объявить маршрут
// студии нет, и этого не даёт забыть компилятор. Читать источник и
// принимать разбор — разные права намеренно: принять разбор значит решить,
// что теперь считается истиной источника, и давать это всякому, кто может
// посмотреть, незачем.
//
// Все ручки здесь говорят про любой источник, а не про МКБ: ни одна не
// спрашивает, классификация перед ней или приказ. Условие «если это МКБ»
// в этом файле — дефект, а не частный случай.
func Routes(desk *studio.Desk, store *Store) {
	r := &routes{store: store}

	desk.Handle(studio.PermSourceRead, "GET /admin/api/sources", r.listSources)
	desk.Handle(studio.PermSourceAccept, "POST /admin/api/sources", r.createSource)
	desk.Handle(studio.PermSourceRead, "GET /admin/api/sources/{id}", r.showSource)

	// Правка паспорта стоит под тем же правом, что и приёмка: словарь
	// интерфейса, ось и полнота — это объявления источника о себе, и по
	// ним считаются доли охвата и решается, складываются ли два
	// источника в один список.
	desk.Handle(studio.PermSourceAccept, "PUT /admin/api/sources/{id}", r.updateSource)

	// Объявление источника действующим стоит под тем же правом, что и
	// приёмка разбора: и то и другое решает, что теперь считается истиной
	// источника, а отдавать это всякому, кто может посмотреть, незачем.
	desk.Handle(studio.PermSourceAccept, "PUT /admin/api/sources/{id}/status", r.setStatus)
	desk.Handle(studio.PermSourceRead, "GET /admin/api/sources/{id}/units", r.listUnits)
	desk.Handle(studio.PermSourceRead, "GET /admin/api/sources/{id}/statements", r.listStatements)
	desk.Handle(studio.PermSourceRead, "GET /admin/api/sources/{id}/documents", r.listDocuments)
	desk.Handle(studio.PermSourceAccept, "POST /admin/api/sources/{id}/documents", r.uploadDocument)

	desk.Handle(studio.PermSourceRead, "GET /admin/api/documents/{id}/fragments", r.listFragments)
	desk.Handle(studio.PermSourceRead, "GET /admin/api/documents/{id}/draft", r.showDraft)
	desk.Handle(studio.PermSourceAccept, "PUT /admin/api/documents/{id}/draft", r.putDraft)
	desk.Handle(studio.PermSourceAccept, "POST /admin/api/documents/{id}/accept", r.acceptDraft)
}

type routes struct {
	store *Store
}

// sourceJSON — паспорт источника, каким его видит студия.
//
// Отдельный вид, а не Source с метками полей: проводной формат меняется по
// своим правилам (только добавлением необязательных полей), и связывать его
// с внутренним видом значит менять его всякий раз, когда меняется он.
type sourceJSON struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	UnitWord      string `json:"unitWord"`
	StatementWord string `json:"statementWord"`
	Purpose       string `json:"purpose"`
	Hierarchy     string `json:"hierarchy"`
	Completeness  string `json:"completeness"`
	Edition       string `json:"edition"`
	Status        string `json:"status"`
}

func toSourceJSON(src Source) sourceJSON {
	return sourceJSON{
		ID: src.ID, Slug: src.Slug, Kind: string(src.Kind), Title: src.Title,
		UnitWord: src.UnitWord, StatementWord: src.StatementWord,
		Purpose: string(src.Purpose), Hierarchy: string(src.Hierarchy),
		Completeness: string(src.Completeness), Edition: src.Edition, Status: src.Status,
	}
}

type unitJSON struct {
	Label       string `json:"label"`
	ParentLabel string `json:"parentLabel"`
	Title       string `json:"title"`
	Path        string `json:"path"`
	Depth       int    `json:"depth"`
	Kind        string `json:"kind"`
	Answerable  bool   `json:"answerable"`
	Ord         int    `json:"ord"`
}

func toUnitsJSON(units []Unit) []unitJSON {
	// Пустой список — [], а не null: студия ходит по нему циклом, и null
	// роняет её белым экраном именно на исправном случае — на источнике,
	// куда ещё ничего не принято.
	out := make([]unitJSON, 0, len(units))
	for _, u := range units {
		out = append(out, unitJSON{
			Label: u.Label, ParentLabel: u.ParentLabel, Title: u.Title,
			Path: u.Path, Depth: u.Depth, Kind: u.Kind,
			Answerable: u.Answerable, Ord: u.Ord,
		})
	}
	return out
}

type statementJSON struct {
	UnitLabel   string `json:"unitLabel"`
	Kind        string `json:"kind"`
	Designation string `json:"designation"`
	Body        string `json:"body"`
	PlaceRef    string `json:"placeRef"`
	Ord         int    `json:"ord"`
}

func toStatementsJSON(statements []Statement) []statementJSON {
	out := make([]statementJSON, 0, len(statements))
	for _, st := range statements {
		out = append(out, statementJSON{
			UnitLabel: st.UnitLabel, Kind: st.Kind, Designation: st.Designation,
			Body: st.Body, PlaceRef: st.PlaceRef, Ord: st.Ord,
		})
	}
	return out
}

type documentJSON struct {
	ID         int64  `json:"id"`
	SourceID   int64  `json:"sourceId"`
	Filename   string `json:"filename"`
	MIME       string `json:"mime"`
	ByteSize   int64  `json:"byteSize"`
	SHA256     string `json:"sha256"`
	UploadedBy string `json:"uploadedBy"`
}

func toDocumentJSON(doc Document) documentJSON {
	return documentJSON{
		ID: doc.ID, SourceID: doc.SourceID, Filename: doc.Filename, MIME: doc.MIME,
		ByteSize: doc.ByteSize, SHA256: doc.SHA256, UploadedBy: doc.UploadedBy,
	}
}

func (r *routes) listSources(w http.ResponseWriter, req *http.Request, _ studio.User) {
	sources, err := r.store.Sources(req.Context())
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Список источников не прочитан")
		return
	}
	out := make([]sourceJSON, 0, len(sources))
	for _, src := range sources {
		out = append(out, toSourceJSON(src))
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"sources": out})
}

type createSourceRequest struct {
	Slug          string `json:"slug"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	UnitWord      string `json:"unitWord"`
	StatementWord string `json:"statementWord"`
	Purpose       string `json:"purpose"`
	Hierarchy     string `json:"hierarchy"`
	Completeness  string `json:"completeness"`
	Edition       string `json:"edition"`
}

func (r *routes) createSource(w http.ResponseWriter, req *http.Request, _ studio.User) {
	var body createSourceRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	id, err := r.store.CreateSource(req.Context(), Source{
		Slug: body.Slug, Kind: Kind(body.Kind), Title: body.Title,
		UnitWord: body.UnitWord, StatementWord: body.StatementWord,
		Purpose: Purpose(body.Purpose), Hierarchy: Hierarchy(body.Hierarchy),
		Completeness: Completeness(body.Completeness), Edition: body.Edition,
	})
	if err != nil {
		// Текст отказа уезжает человеку как есть: он написан по-русски и
		// говорит, чего не хватает, — а «проверьте поля» отправляет
		// перебирать их вслепую.
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// updateSource правит паспорт источника.
//
// # Краткое имя не правится, и это не забывчивость
//
// По нему приложение спрашивает справочник: `/v1/reference/sources/{slug}`.
// На руках у врачей стоят сборки, которые скачали разделы по этому имени
// и держат их у себя; смени его — и источник для них просто исчезнет, а
// узнают они об этом не сообщением, а пустым справочником. Поэтому имя
// задаётся один раз, при заведении, и здесь его нет вовсе: поле,
// принимаемое и молча отбрасываемое, хуже отсутствующего.
//
// Всё остальное правится: название, словарь интерфейса, вид, ось, смысл
// вложенности, полнота и редакция. Это объявления источника о себе, и
// объявленное однажды по ошибке иначе остаётся навсегда — а полнота,
// названная неверно, врёт в долях охвата ровно там, где на них смотрят.
func (r *routes) updateSource(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	var body updateSourceRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	src, err := r.store.UpdateSource(req.Context(), id, Source{
		Kind: Kind(body.Kind), Title: body.Title,
		UnitWord: body.UnitWord, StatementWord: body.StatementWord,
		Purpose: Purpose(body.Purpose), Hierarchy: Hierarchy(body.Hierarchy),
		Completeness: Completeness(body.Completeness), Edition: body.Edition,
	})
	if err != nil {
		// Текст отказа уезжает человеку как есть: он написан по-русски и
		// говорит, чего не хватает.
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, toSourceJSON(src))
}

// updateSourceRequest — паспорт без краткого имени. Довод — у updateSource.
type updateSourceRequest struct {
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	UnitWord      string `json:"unitWord"`
	StatementWord string `json:"statementWord"`
	Purpose       string `json:"purpose"`
	Hierarchy     string `json:"hierarchy"`
	Completeness  string `json:"completeness"`
	Edition       string `json:"edition"`
}

func (r *routes) showSource(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	src, err := r.store.SourceByID(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusNotFound, "Такого источника нет")
		return
	}
	studio.WriteJSON(w, http.StatusOK, toSourceJSON(src))
}

// MaxUnitsShown — сколько единиц ручка отдаёт за один раз.
//
// Предела не было вовсе, при том что пояснение этой же ручки говорило
// «у классификации единиц тысячи». МКБ-10 — около четырнадцати тысяч
// единиц: они приезжали целиком и разворачивались в студии в четырнадцать
// тысяч узлов списка и в выпадающий список из четырнадцати тысяч строк
// без поиска, перерисовываясь на каждое нажатие клавиши в отборе.
//
// Пятьсот: столько человек не просматривает, но столько оставляет отбор
// вроде «F3» у крупного класса, и обрезать его на ста значило бы прятать
// работу. Кому нужно точнее — сужает путь, и ручка сама об этом говорит.
const MaxUnitsShown = 500

// listUnits отдаёт единицы источника, целиком или срезом по пути.
//
// Срез берётся запросом, а не подъёмом всего источника в память: у
// классификации единиц тысячи, и «всё, что под F3» — самый частый вопрос
// подбора. Правило среза написано дважды, на Go и на SQL, и сверяет их
// проверка на живой базе, поле за полем.
//
// # Обрезанное называется числом, а не пропадает молча
//
// Отбор, показавший пятьсот единиц из четырнадцати тысяч, и отбор,
// показавший все пятьсот, какие есть, выглядят одинаково — а решения по
// ним принимаются разные: по первому составитель заказывает генерацию,
// думая, что видит весь класс. Поэтому рядом со списком едет, сколько
// единиц подошло всего.
func (r *routes) listUnits(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	path := req.URL.Query().Get("path")

	// Спрашивается на одну больше предела: так «их ровно пятьсот» и «их
	// больше пятисот» различаются без второго запроса со счётом.
	units, err := r.store.SliceUnits(req.Context(), id, path, MaxUnitsShown+1)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Единицы источника не прочитаны")
		return
	}
	more := len(units) > MaxUnitsShown
	if more {
		units = units[:MaxUnitsShown]
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"units": toUnitsJSON(units),
		"limit": MaxUnitsShown,
		"more":  more,
	})
}

// listStatements отдаёт положения, принятые в источник: все или одной
// единицы. На них ссылается разметка задачи, и смотреть их человек будет
// рядом с единицей, а не в черновике — черновик к тому времени уже принят.
func (r *routes) listStatements(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	statements, err := r.store.UnitStatements(req.Context(), id, req.URL.Query().Get("unit"))
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Положения источника не прочитаны")
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"statements": toStatementsJSON(statements)})
}

func (r *routes) listDocuments(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	docs, err := r.store.Documents(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Документы источника не прочитаны")
		return
	}
	out := make([]documentJSON, 0, len(docs))
	for _, doc := range docs {
		out = append(out, toDocumentJSON(doc))
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"documents": out})
}

// uploadDocument принимает файл, режет его на куски и кладёт оба.
//
// Куски считаются сразу при загрузке, а не по отдельной команде: документ,
// лежащий неразобранным, выглядит как загруженный и молча не работает —
// человек ждёт черновика, которого никто не начинал.
func (r *routes) uploadDocument(w http.ResponseWriter, req *http.Request, user studio.User) {
	sourceID, ok := pathID(w, req)
	if !ok {
		return
	}
	filename, body, err := readUpload(w, req)
	if err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	prepared, err := Prepare(Upload{Filename: filename, Body: body})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrPDF) {
			// Отдельный код у PDF, потому что студия показывает этот отказ
			// иначе: это не ошибка ввода, а объявленная граница проекта.
			status = http.StatusUnsupportedMediaType
		}
		studio.WriteError(w, status, studio.Sentence(err.Error()))
		return
	}

	docID, err := r.store.SaveDocument(req.Context(), Document{
		SourceID: sourceID, Filename: filename, MIME: prepared.MIME,
		SHA256: prepared.SHA256, Body: body, UploadedBy: user.Login,
	})
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Документ не сохранён")
		return
	}
	frags := SplitText(prepared.Markdown)
	if err := r.store.SaveFragments(req.Context(), docID, frags); err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Документ сохранён, но не разрезан на куски")
		return
	}
	studio.WriteJSON(w, http.StatusCreated, map[string]any{
		"id":        docID,
		"format":    string(prepared.Format),
		"fragments": len(frags),
	})
}

func (r *routes) listFragments(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	frags, err := r.store.Fragments(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Куски документа не прочитаны")
		return
	}
	out := make([]map[string]any, 0, len(frags))
	for i, f := range frags {
		out = append(out, map[string]any{
			"ord": i, "body": f.Body, "charFrom": f.CharFrom, "charTo": f.CharTo,
		})
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{"fragments": out})
}

func (r *routes) showDraft(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	units, err := r.store.DraftUnits(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Черновик не прочитан")
		return
	}
	statements, err := r.store.DraftStatements(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Положения черновика не прочитаны")
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"units":      toUnitsJSON(units),
		"statements": toStatementsJSON(statements),
	})
}

type draftRequest struct {
	Units []struct {
		Label       string `json:"label"`
		ParentLabel string `json:"parentLabel"`
		Title       string `json:"title"`

		// Род записи: 'group' — вход в навигацию, 'entry' (или пусто) —
		// то, по чему спрашивают. Без него справочник в девятьсот строк
		// остаётся без входа.
		Kind string `json:"kind"`
	} `json:"units"`
	Statements []struct {
		UnitLabel   string `json:"unitLabel"`
		Kind        string `json:"kind"`
		Designation string `json:"designation"`
		Body        string `json:"body"`
		PlaceRef    string `json:"placeRef"`
	} `json:"statements"`
}

// putDraft кладёт черновик разбора целиком, заменяя прежний.
//
// Целиком, а не по единице: черновик — один ответ модели на один документ,
// и склейка двух ответов даёт разбор, которого не делал никто. Пока
// генерация не приехала (этап 2), этой ручкой черновик кладут руками —
// так весь путь источника можно пройти и проверить уже сейчас.
func (r *routes) putDraft(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	var body draftRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, "Запрос не разобран: "+err.Error())
		return
	}
	units := make([]Unit, 0, len(body.Units))
	for _, u := range body.Units {
		units = append(units, Unit{
			Label: u.Label, ParentLabel: u.ParentLabel, Title: u.Title,
			Kind: u.Kind, Answerable: u.Kind != KindGroup,
		})
	}
	statements := make([]Statement, 0, len(body.Statements))
	for _, st := range body.Statements {
		statements = append(statements, Statement{
			UnitLabel: st.UnitLabel, Kind: st.Kind, Designation: st.Designation,
			Body: st.Body, PlaceRef: st.PlaceRef,
		})
	}
	if err := r.store.SaveDraft(req.Context(), id, units, statements); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"units": len(units), "statements": len(statements),
	})
}

// acceptDraft переводит черновик в источник.
//
// Номер источника берётся у документа, а не из тела запроса: документ
// принесли в источник, и принять его в другой — значит смешать два
// справочника, не заметив этого.
func (r *routes) acceptDraft(w http.ResponseWriter, req *http.Request, user studio.User) {
	docID, ok := pathID(w, req)
	if !ok {
		return
	}
	doc, err := r.store.DocumentByID(req.Context(), docID)
	if err != nil {
		studio.WriteError(w, http.StatusNotFound, "Такого документа нет")
		return
	}
	if doc.SourceID == 0 {
		studio.WriteError(w, http.StatusBadRequest, "Документ не привязан к источнику: принимать его некуда")
		return
	}
	accepted, err := r.store.AcceptDraft(req.Context(), doc.SourceID, docID, user.Login)
	if err != nil {
		// Отказ целиком: источник с половиной принятой ветки хуже
		// непринятого — у части единиц путь есть, у части нет, и срез
		// отдаёт то одно, то другое.
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	studio.WriteJSON(w, http.StatusOK, map[string]any{
		"sourceId": doc.SourceID, "accepted": accepted,
	})
}

// readUpload достаёт файл из запроса.
//
// Два способа намеренно: студия шлёт форму с полем file, а проверить путь
// из командной строки удобнее телом запроса с именем файла в заголовке.
// Второй способ — не послабление: имя всё равно обязательно, потому что из
// него берётся формат.
func readUpload(w http.ResponseWriter, req *http.Request) (string, []byte, error) {
	// Потолок ставится на ТЕЛО, а не на разбор. Довод у ParseMultipartForm
	// обманчив: её число — предел памяти, а всё сверх него уезжает во
	// временные файлы, и общего потолка у них нет. Гигабайтная форма
	// разберётся без отказа и ляжет на диск контейнера; повторённая
	// десяток раз — забьёт его, и упадёт не загрузка, а сервер целиком,
	// потому что писать снимки и журнал станет некуда.
	//
	// Запас вдвое: границу MaxDocumentBytes стережёт Prepare, и отказ
	// оттуда объясняет врачу, ЧТО не так («файл больше 30 МБ»). Обрежь
	// тело ровно по границе — и файл на 30 МБ ровно оборвался бы
	// невнятным «форма с файлом не разобрана».
	req.Body = http.MaxBytesReader(w, req.Body, 2*MaxDocumentBytes)

	if strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/form-data") {
		if err := req.ParseMultipartForm(MaxDocumentBytes); err != nil {
			return "", nil, errors.New("форма с файлом не разобрана")
		}
		file, head, err := req.FormFile("file")
		if err != nil {
			return "", nil, errors.New("в форме нет поля file с документом")
		}
		defer file.Close()
		body, err := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
		if err != nil {
			return "", nil, errors.New("файл не дочитан")
		}
		return head.Filename, body, nil
	}
	filename := req.Header.Get("X-Filename")
	if filename == "" {
		return "", nil, errors.New("не назван файл: пришлите форму с полем file или заголовок X-Filename")
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, MaxDocumentBytes+1))
	if err != nil {
		return "", nil, errors.New("файл не дочитан")
	}
	return filename, body, nil
}

// pathID достаёт номер из пути и сам отвечает на негодный.
func pathID(w http.ResponseWriter, req *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		studio.WriteError(w, http.StatusBadRequest, "Номер в адресе не разобран")
		return 0, false
	}
	return id, true
}

type statusRequest struct {
	Status string `json:"status"`
}

// setStatus объявляет источник действующим, черновиком или отменённым.
//
// Отдельная ручка, а не поле в паспорте: паспорт правят походя, а это
// решение о том, увидят ли источник врачи. Отдельное действие видно и в
// студии, и в журнале обращений.
func (r *routes) setStatus(w http.ResponseWriter, req *http.Request, _ studio.User) {
	id, ok := pathID(w, req)
	if !ok {
		return
	}
	var body statusRequest
	if err := studio.DecodeBody(req, &body); err != nil {
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	if err := r.store.SetStatus(req.Context(), id, body.Status); err != nil {
		// Отказ уезжает своими словами: он говорит, что делать («примите
		// разбор хотя бы одного документа»), а «400 Bad Request» не
		// говорит ничего.
		studio.WriteError(w, http.StatusBadRequest, studio.Sentence(err.Error()))
		return
	}
	src, err := r.store.SourceByID(req.Context(), id)
	if err != nil {
		studio.WriteError(w, http.StatusInternalServerError, "Состояние записано, но паспорт не перечитан")
		return
	}
	studio.WriteJSON(w, http.StatusOK, toSourceJSON(src))
}
