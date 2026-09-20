package gen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/source"
)

// Проверки узла разбора.
//
// Без сети и без базы там, где можно: заслон, деление на части и складывание
// частей — это правила, а не запросы, и проверять их на живой базе значило бы
// платить минутой прогона за то, что считается в памяти. На живой базе
// проверяется ровно то, что живёт в ней: отбор по роду и то, что порядок в
// черновике сквозной.

// подставнойХранитель запоминает положенное вместо базы.
type подставнойХранитель struct {
	куски       []source.Fragment
	чисток      int
	единицы     []source.Unit
	положения   []source.Statement
	отказЗаписи error
}

func (х *подставнойХранитель) Fragments(context.Context, int64) ([]source.Fragment, error) {
	return х.куски, nil
}

func (х *подставнойХранитель) ClearDraft(context.Context, int64) error {
	х.чисток++
	// Чистка стирает и запомненное: иначе проверка «чистится один раз» не
	// отличила бы одну чистку от трёх.
	х.единицы, х.положения = nil, nil
	return nil
}

func (х *подставнойХранитель) AppendDraft(_ context.Context, _ int64,
	units []source.Unit, statements []source.Statement) error {

	if х.отказЗаписи != nil {
		return х.отказЗаписи
	}
	х.единицы = append(х.единицы, units...)
	х.положения = append(х.положения, statements...)
	return nil
}

// разбор — ответ модели в том виде, в каком его ждёт заслон.
func разбор(units []map[string]any, statements []map[string]any) string {
	body, err := json.Marshal(map[string]any{"units": units, "statements": statements})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func единица(label, title, parent string, group bool) map[string]any {
	return map[string]any{"label": label, "title": title, "parent": parent, "group": group}
}

func положение(unit, designation, text, place string) map[string]any {
	return map[string]any{"unit": unit, "designation": designation, "text": text, "place": place}
}

func TestЗаслонРазбораОтбрасываетНепонятоеСоСчётом(t *testing.T) {
	// Непонятое не применяется, и отброшенное считается: молча отброшенная
	// половина ответа выглядит как «документ беден», и объяснить это нечем.
	answer := разбор(
		[]map[string]any{
			единица("3.1", "Сроки", "3", false),
			единица("", "Без метки", "3", false),       // не единица
			единица("3.2", "", "3", false),             // не единица
			единица("3.1", "Повтор той же", "", false), // второй ответ о том же месте
			единица("3", "Порядок", "", true),          // группа
		},
		[]map[string]any{
			положение("3.1", "абз. 1", "Срок — десять дней.", "с. 4"),
			положение("3.1", "", "", "с. 4"),               // не положение
			положение("", "абз. 2", "Текст есть.", ""),     // единица не названа
			положение("9.9", "п. 1", "Чужая единица.", ""), // подтверждать не рядом с чем
		},
	)

	units, statements, dropped, err := SanitizeParsed(answer)
	if err != nil {
		t.Fatalf("исправный ответ отбит: %v", err)
	}
	if len(units) != 2 {
		t.Errorf("годных единиц %d, ждали 2 — «3.1» и группу «3»: %+v", len(units), units)
	}
	if len(statements) != 1 {
		t.Errorf("годных положений %d, ждали 1: %+v", len(statements), statements)
	}
	if dropped != 6 {
		t.Errorf("отброшено %d, ждали 6 — три единицы и три положения", dropped)
	}

	// Род доезжает до приёмки через черновик: без него групп не бывало бы
	// вовсе, а без групп большой документ остаётся без входа.
	var group source.Unit
	for _, u := range units {
		if u.Label == "3" {
			group = u
		}
	}
	if group.Kind != "group" {
		t.Errorf("единица «3» пришла родом %q, а помечена была группой", group.Kind)
	}
}

func TestОтветБезЕдиницОтбрасываетсяЦеликом(t *testing.T) {
	// Разбор без единиц — это не разбор части, а его отсутствие. Класть
	// его черновиком значило бы подменить документ пустотой молча.
	_, _, _, err := SanitizeParsed(разбор(nil, []map[string]any{
		положение("3.1", "абз. 1", "Положение без своей единицы.", ""),
	}))
	if err == nil {
		t.Fatal("ответ без единиц принят")
	}
	if !strings.Contains(err.Error(), "ни одной единицы") {
		t.Errorf("отказ не называет причину: %v", err)
	}
}

func TestРазборСнимаетОградуJSON(t *testing.T) {
	// Модели обрамляют ответ ```json даже там, где схема запрещает, и
	// ронять из-за обрамления оплаченный разбор незачем.
	fenced := "```json\n" + разбор(
		[]map[string]any{единица("1", "Первый", "", false)}, nil) + "\n```"
	units, _, _, err := SanitizeParsed(fenced)
	if err != nil {
		t.Fatalf("обрамлённый ответ отбит: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("единиц %d, ждали 1", len(units))
	}
}

func TestЧастиСобираютсяИзКусковПодПотолком(t *testing.T) {
	// Часть — это подряд идущие куски, пока укладываются. Не «по N знаков»:
	// положение, разрезанное посередине, не разберёт никто.
	крупный := strings.Repeat("я", MaxPartChars+100)
	parts := SplitParts([]source.Fragment{
		{Level: 1, Title: "Раздел 1", Body: "Первый абзац."},
		{Level: 2, Title: "Подраздел", Body: "Второй абзац."},
		{Level: 1, Title: "Раздел 2", Body: крупный},
		{Level: 1, Title: "Раздел 3", Body: "Третий абзац."},
	})
	if len(parts) != 3 {
		t.Fatalf("частей %d, ждали 3: %+v", len(parts), названия(parts))
	}
	if !strings.Contains(parts[0].Body, "Первый абзац") ||
		!strings.Contains(parts[0].Body, "Второй абзац") {
		t.Errorf("два мелких куска не собрались в одну часть: %q", parts[0].Body)
	}
	if parts[0].Title != "Раздел 1" {
		t.Errorf("часть названа %q, ждали «Раздел 1»", parts[0].Title)
	}
	// Кусок, не влезающий в потолок, идёт частью в одиночку и не режется.
	if parts[1].Title != "Раздел 2" || strings.Contains(parts[1].Body, "Третий абзац") {
		t.Errorf("крупный кусок ушёл не в одиночку: %q", first(parts[1].Body))
	}
	if len(parts[1].Body) <= MaxPartChars {
		t.Errorf("крупный кусок порезан: его длина %d, а резать его нельзя", len(parts[1].Body))
	}
	// Заголовок уходит модели вместе с телом: без него кусок теряет то
	// единственное, что говорит, о чём он.
	if !strings.Contains(parts[0].Body, "# Раздел 1") {
		t.Errorf("заголовок не уехал вместе с телом: %q", parts[0].Body)
	}
}

func TestМестоНазываетсяТолькоУДокументаИзНесколькихЧастей(t *testing.T) {
	// Не зная, что перед ней часть, модель достраивает недостающее по
	// памяти — ровно то, что запрещено ей первым правилом задания. Но
	// «часть 1 из 1» заставило бы её искать соседние части, которых нет.
	if line := placeLine(Part{Title: "Раздел"}, 0, 1); line != "" {
		t.Errorf("у документа из одной части названо место: %q", line)
	}
	line := placeLine(Part{Title: "Диагностика"}, 11, 80)
	if !strings.Contains(line, "12 из 80") || !strings.Contains(line, "Диагностика") {
		t.Errorf("место названо невнятно: %q", line)
	}
}

func TestПоложениеСсылаетсяНаЕдиницуИзПрежнейЧасти(t *testing.T) {
	// Заслон, помнящий только свою часть, отбрасывал бы исправное:
	// положение из части третьей ссылается на единицу из первой сплошь.
	known := map[string]bool{}

	units, _ := keepNew([]source.Unit{{Label: "3.1", Title: "Сроки"}}, nil, known)
	if len(units) != 1 {
		t.Fatalf("первая часть отдала %d единиц, ждали 1", len(units))
	}

	// Вторая часть называет ту же единицу заново и добавляет положение к ней.
	units, statements := keepNew(
		[]source.Unit{{Label: "3.1", Title: "Сроки (повтор)"}, {Label: "3.2", Title: "Отказ"}},
		[]source.Statement{
			{UnitLabel: "3.1", Body: "Срок продлевается."},
			{UnitLabel: "3.2", Body: "Отказ оформляется письменно."},
			{UnitLabel: "9.9", Body: "Единицы такой нет нигде."},
		}, known)

	if len(units) != 1 || units[0].Label != "3.2" {
		t.Errorf("повтор единицы не отсеян: %+v", units)
	}
	if len(statements) != 2 {
		t.Fatalf("положений %d, ждали 2: %+v", len(statements), statements)
	}
	if statements[0].UnitLabel != "3.1" {
		t.Errorf("положение к единице прежней части потеряно: %+v", statements)
	}
}

func TestPgРазборПишетЧастиПоМереРаботыИСчитаетОтброшенное(t *testing.T) {
	// Главное свойство узла: всё, что записано, обрыв уже не отнимет.
	// Средняя часть здесь не даётся — соседние обязаны дойти до черновика,
	// а причина обязана остаться замечанием при задании.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	docID := документ(t, gate, sourceID)

	keeper := &подставнойХранитель{куски: []source.Fragment{
		{Level: 1, Title: "Раздел 1", Body: "Первый."},
		{Level: 1, Title: "Раздел 2", Body: strings.Repeat("я", MaxPartChars-50)},
		{Level: 1, Title: "Раздел 3", Body: strings.Repeat("ю", MaxPartChars-50)},
	}}
	model := &подставнаяМодель{ответы: []string{
		разбор([]map[string]any{единица("1", "Первый", "", false)},
			[]map[string]any{положение("1", "абз. 1", "Положение первой части.", "с. 1")}),
		"ответ, который не разобрать",
		разбор([]map[string]any{единица("3", "Третий", "", false)},
			[]map[string]any{положение("3", "абз. 1", "Положение третьей части.", "с. 3")}),
	}}

	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	plan, err := NewResolver(gate).ResolveParse(ctx, docID, "")
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.PlaceParse(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}

	runner := NewParseRunner(jobs, prompts, model, keeper)
	taken, err := взятьРазбор(ctx, jobs, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.run(ctx, taken)
	if err != nil {
		t.Fatalf("разбор отбит целиком, а две части из трёх дались: %v", err)
	}

	if result.Parts != 3 || result.PartsDone != 2 {
		t.Errorf("частей %d, разобрано %d — ждали 3 и 2", result.Parts, result.PartsDone)
	}
	if len(keeper.единицы) != 2 || len(keeper.положения) != 2 {
		t.Errorf("в черновик легло единиц %d, положений %d — ждали по две",
			len(keeper.единицы), len(keeper.положения))
	}
	if keeper.чисток != 1 {
		t.Errorf("черновик чистился %d раз, а чистка нужна одна: иначе части стирают друг друга",
			keeper.чисток)
	}

	// Причина, по которой часть не далась, обязана остаться при задании:
	// задание, отчитавшееся успехом и промолчавшее о потерянной части,
	// выдаёт неполный разбор за полный.
	after, err := jobs.Job(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Notes) == 0 {
		t.Fatal("о неразобравшейся части не сказано ничего")
	}
	if !strings.Contains(strings.Join(after.Notes, " "), "часть 2 из 3") {
		t.Errorf("замечание не называет, какая часть не далась: %v", after.Notes)
	}
}

func TestPgНиОднойЧастиЭтоОтказАНеПустойЧерновик(t *testing.T) {
	// Зелёная отметка над пустым черновиком врёт: составитель примет её за
	// «в документе нечего размечать».
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	docID := документ(t, gate, sourceID)

	keeper := &подставнойХранитель{куски: []source.Fragment{{Title: "Раздел", Body: "Текст."}}}
	model := &подставнаяМодель{отказ: errors.New("провайдер не ответил")}

	prompts := NewPrompts(gate)
	if err := prompts.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(gate)
	plan, err := NewResolver(gate).ResolveParse(ctx, docID, "")
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.PlaceParse(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	taken, err := взятьРазбор(ctx, jobs, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewParseRunner(jobs, prompts, model, keeper).run(ctx, taken); err == nil {
		t.Fatal("разбор, не разобравший ни одной части, объявлен удавшимся")
	}
}

func TestPgИсполнителиНеБерутЧужихЗаданий(t *testing.T) {
	// Исполнитель, взявший чужое задание, прочёл бы чужой заказ и выполнил
	// не то. Род стоит в отборе, а не проверяется после: взятое и
	// положенное обратно задание уже помечено идущим.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	docID := документ(t, gate, sourceID)

	jobs := NewJobs(gate)
	plan, err := NewResolver(gate).ResolveParse(ctx, docID, "")
	if err != nil {
		t.Fatal(err)
	}
	parse, err := jobs.PlaceParse(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}

	// База одна на весь прогон, и чужие задания в очереди — обычное дело:
	// ищем своё, а не первое попавшееся.
	for {
		job, ok, err := jobs.Take(ctx, KindCase)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if job.ID == parse.ID {
			t.Fatal("исполнитель задач взял разбор документа")
		}
		if err := jobs.Done(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
	}

	got, err := взятьРазбор(ctx, jobs, parse.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParsePlan.DocumentID != docID {
		t.Errorf("разбору достался документ %d, а заказан был %d",
			got.ParsePlan.DocumentID, docID)
	}
}

func TestPgИдущийРазборНеЗаводитВторого(t *testing.T) {
	// Два разбора одного документа пишут в один черновик: второй затирает
	// части первого, и на выходе получается разбор, которого не делал
	// никто, — притом обе работы отчитываются успехом.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	docID := документ(t, gate, sourceID)

	jobs := NewJobs(gate)
	plan, err := NewResolver(gate).ResolveParse(ctx, docID, "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := jobs.PlaceParse(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	id, running, err := jobs.RunningParse(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	if !running || id != first.ID {
		t.Fatalf("идущий разбор не найден: нашлось %d, идёт %v", id, running)
	}

	// Закрытый разбор идущим не считается: переделка — обычная работа
	// составителя, и запрещено здесь параллельное, а не повторное.
	if err := jobs.Done(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, running, err = jobs.RunningParse(ctx, docID); err != nil {
		t.Fatal(err)
	} else if running {
		t.Error("закрытый разбор считается идущим — переделать документ стало нельзя")
	}
}

func TestPgЧужойДокументНеРазбирается(t *testing.T) {
	// Черновик ложится ПРИ документе, а принимается В источник. Документ
	// без источника принимать некуда, и узнать об этом лучше при заказе,
	// чем отказом приёмки через час.
	ctx := context.Background()
	gate := testGate(t)

	var docID int64
	err := gate.QueryRow(ctx,
		`INSERT INTO source_documents (source_id, filename, mime, byte_size, sha256, body)
		 VALUES (NULL, 'ничей.md', 'text/markdown', 7, $1, $2)
		 RETURNING id`, отпечаток(fmt.Sprintf("ничей-%d", time.Now().UnixNano())),
		[]byte("# Ничей")).Scan(&docID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewResolver(gate).ResolveParse(ctx, docID, ""); err == nil {
		t.Fatal("документ без источника принят к разбору")
	}
}

func TestPgПорядокВЧерновикеСквознойМеждуЧастями(t *testing.T) {
	// Модель, размечающая третью часть, нумерует со своего нуля — она не
	// знает ни о первой, ни о второй. Взять её номера значило бы поставить
	// третью часть в начало черновика, а по порядку документа потом
	// выбирается головное положение единицы.
	ctx := context.Background()
	gate := testGate(t)
	sourceID := источник(t, gate)
	docID := документ(t, gate, sourceID)
	store := source.NewStore(gate)

	if err := store.ClearDraft(ctx, docID); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendDraft(ctx, docID,
		[]source.Unit{{Label: "1", Title: "Первый"}},
		[]source.Statement{{UnitLabel: "1", Body: "Первое положение."}}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendDraft(ctx, docID,
		[]source.Unit{{Label: "2", Title: "Второй"}},
		[]source.Statement{{UnitLabel: "2", Body: "Второе положение."}}); err != nil {
		t.Fatal(err)
	}

	units, err := store.DraftUnits(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 || units[0].Label != "1" || units[1].Label != "2" {
		t.Fatalf("порядок единиц разъехался: %+v", units)
	}
	statements, err := store.DraftStatements(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 2 || statements[0].Body != "Первое положение." {
		t.Fatalf("порядок положений разъехался: %+v", statements)
	}
}

// взятьРазбор достаёт из очереди именно наше задание.
//
// База одна на весь прогон, и чужие разборы в очереди — обычное дело:
// взять первое попавшееся значило бы проверить чужую работу.
func взятьРазбор(ctx context.Context, jobs *Jobs, id int64) (Job, error) {
	for {
		job, ok, err := jobs.Take(ctx, KindParse)
		if err != nil {
			return Job{}, err
		}
		if !ok {
			return Job{}, fmt.Errorf("задание %d не нашлось в очереди разбора", id)
		}
		if job.ID == id {
			return job, nil
		}
		if err := jobs.Done(ctx, job.ID); err != nil {
			return Job{}, err
		}
	}
}

// документ кладёт файл при источнике: разбор заказывается по документу.
//
// Тело настоящее, а не пустое: колонка требует непустого, и отпечаток у
// каждого свой — иначе второй документ той же проверки лёг бы первым (в
// таблице стоит указатель по паре «источник, отпечаток»).
func документ(t *testing.T, gate *dbgate.Gate, sourceID int64) int64 {
	t.Helper()
	body := fmt.Sprintf("# Документ %d-%d", time.Now().UnixNano(), rand.Intn(1000))
	var id int64
	err := gate.QueryRow(context.Background(),
		`INSERT INTO source_documents (source_id, filename, mime, byte_size, sha256, body)
		 VALUES ($1, 'приказ.md', 'text/markdown', $2, $3, $4)
		 RETURNING id`,
		sourceID, len(body), отпечаток(body), []byte(body)).Scan(&id)
	if err != nil {
		t.Fatalf("документ не заведён: %v", err)
	}
	return id
}

// отпечаток — то, чем в таблице различаются два принесённых файла.
func отпечаток(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// названия — заголовки частей, чтобы отказ проверки было что читать.
func названия(parts []Part) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.Title)
	}
	return out
}

// first — начало длинной строки: целиком она в отказе не читается.
func first(text string) string {
	if len(text) > 80 {
		return text[:80] + "…"
	}
	return text
}
