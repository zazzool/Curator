package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"curator/server/internal/dbgate"
)

// Проверки ввоза идут на настоящей базе.
//
// Поддельное хранилище проверило бы перевод тела и ничего больше, а весь
// риск ввоза — как раз в том, что творится между двумя базами: считанный
// путь, пропущенное удалённое, задача, приписанная чужому источнику,
// повторный заход после обрыва. Поэтому «прежняя» база здесь настоящая:
// её таблицы заводятся по схеме донора, ровно теми объявлениями, по
// которым ввоз и будет читать боевую.

func testGate(t *testing.T) *dbgate.Gate {
	t.Helper()
	dsn := os.Getenv("CURATOR_TEST_DSN")
	if dsn == "" {
		t.Fatal("CURATOR_TEST_DSN не задан: проверки на живой базе не идут. " +
			"Это отказ, а не пропуск — см. server/.env.example")
	}
	gate, err := dbgate.Open(context.Background(), dsn, dbgate.Options{})
	if err != nil {
		t.Fatalf("проверочная база недоступна: %v", err)
	}
	t.Cleanup(gate.Close)
	return gate
}

// прежняя — таблицы донора, какие читает ввоз.
//
// Объявления сняты со схемы донора и урезаны до читаемых ввозом колонок.
// Урезаны намеренно: лишние колонки донора (организации, квоты, теория)
// ввоз не читает, и держать их здесь значило бы обещать, что он их
// понимает.
func прежняя(t *testing.T, gate *dbgate.Gate, schema string) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`CREATE SCHEMA IF NOT EXISTS ` + schema,
		`CREATE TABLE IF NOT EXISTS ` + schema + `.sources (
			id BIGSERIAL PRIMARY KEY, slug TEXT NOT NULL UNIQUE,
			kind TEXT NOT NULL, title TEXT NOT NULL,
			unit_word TEXT NOT NULL, statement_word TEXT NOT NULL,
			edition TEXT NOT NULL DEFAULT '', issued TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '', legal_note TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			profile JSONB NOT NULL DEFAULT '{}')`,
		`CREATE TABLE IF NOT EXISTS ` + schema + `.source_units (
			id BIGSERIAL PRIMARY KEY, source_id BIGINT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'entry', label TEXT NOT NULL,
			parent_label TEXT NOT NULL DEFAULT '', title TEXT NOT NULL,
			answerable BOOLEAN NOT NULL DEFAULT TRUE, ord INTEGER NOT NULL DEFAULT 0,
			attrs JSONB NOT NULL DEFAULT '{}')`,
		`CREATE TABLE IF NOT EXISTS ` + schema + `.source_unit_statements (
			id BIGSERIAL PRIMARY KEY, source_id BIGINT NOT NULL,
			unit_label TEXT NOT NULL, kind TEXT NOT NULL DEFAULT '',
			designation TEXT NOT NULL DEFAULT '', body_md TEXT NOT NULL,
			place_ref TEXT NOT NULL DEFAULT '', ord INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS ` + schema + `.cases (
			id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'draft',
			origin TEXT NOT NULL DEFAULT 'manual', body JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			published_at TIMESTAMPTZ NULL,
			deleted_at TIMESTAMPTZ NULL,
			source_id BIGINT NULL)`,
		`CREATE TABLE IF NOT EXISTS ` + schema + `.rollup_case_stats (
			case_id TEXT PRIMARY KEY, attempts INTEGER NOT NULL,
			correct INTEGER NOT NULL, median_solve_ms INTEGER)`,
	}
	for _, one := range stmts {
		if _, err := gate.Exec(ctx, one); err != nil {
			t.Fatalf("прежняя база не заведена: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = gate.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
	})
}

// старая — вид на прежнюю базу: то же соединение, но своя схема.
//
// Своим соединением к другой базе здесь было бы честнее, но второй базы в
// проверочном контуре нет. Схема даёт то же, что важно: ввоз читает
// ЧУЖИЕ таблицы и не может случайно прочесть свои.
type старая struct {
	gate   *dbgate.Gate
	schema string
}

func (s старая) Query(ctx context.Context, sql string, args ...any) (pgxRows, error) {
	return s.gate.Query(ctx, withSchema(sql, s.schema), args...)
}

func (s старая) QueryRow(ctx context.Context, sql string, args ...any) pgxRow {
	return s.gate.QueryRow(ctx, withSchema(sql, s.schema), args...)
}

func (s старая) Exec(ctx context.Context, sql string, args ...any) (commandTag, error) {
	return s.gate.Exec(ctx, withSchema(sql, s.schema), args...)
}

// withSchema приписывает схему к именам таблиц донора.
func withSchema(sql, schema string) string {
	for _, name := range []string{"sources", "source_units",
		"source_unit_statements", "cases", "rollup_case_stats"} {
		sql = replaceAll(sql, " "+name+" ", " "+schema+"."+name+" ")
		sql = replaceAll(sql, " "+name+"\n", " "+schema+"."+name+"\n")
	}
	return sql
}

func replaceAll(s, from, to string) string {
	out := ""
	for {
		at := indexOf(s, from)
		if at < 0 {
			return out + s
		}
		out += s[:at] + to
		s = s[at+len(from):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func тело(target string, options []string, extra map[string]any) []byte {
	body := map[string]any{
		"id":            "c-1",
		"title":         "Задача",
		"difficulty":    2,
		"targetCode":    target,
		"options":       options,
		"segments":      []map[string]any{{"text": "Условие", "criterionIds": []string{"cr-1"}}},
		"criteria":      []map[string]any{{"id": "cr-1", "code": "G1", "label": "Первый"}},
		"explanationMd": "Разбор",
	}
	for k, v := range extra {
		body[k] = v
	}
	raw, _ := json.Marshal(body)
	return raw
}

// посеять кладёт в прежнюю базу источник, дерево и задачи.
func посеять(t *testing.T, gate *dbgate.Gate, schema, slug string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := gate.QueryRow(ctx, `INSERT INTO `+schema+`.sources
		(slug, kind, title, unit_word, statement_word)
		VALUES ($1,'classification','МКБ-10','диагноз','критерий') RETURNING id`,
		slug).Scan(&id); err != nil {
		t.Fatalf("источник не посеян: %v", err)
	}

	units := []struct {
		kind, label, parent, title string
		answerable                 bool
	}{
		{"group", "F", "", "Психические расстройства", false},
		{"entry", "F3", "", "Аффективные расстройства", true},
		{"entry", "F32", "F3", "Депрессивный эпизод", true},
		{"entry", "F32.1", "F32", "Умеренный депрессивный эпизод", true},
		{"entry", "F41", "F3", "Тревожные расстройства", true},
	}
	for i, one := range units {
		if _, err := gate.Exec(ctx, `INSERT INTO `+schema+`.source_units
			(source_id, kind, label, parent_label, title, answerable, ord)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			id, one.kind, one.label, one.parent, one.title, one.answerable, i); err != nil {
			t.Fatalf("единица не посеяна: %v", err)
		}
	}
	if _, err := gate.Exec(ctx, `INSERT INTO `+schema+`.source_unit_statements
		(source_id, unit_label, kind, designation, body_md, ord)
		VALUES ($1,'F32.1','cddg','G1','Сниженное настроение',0)`, id); err != nil {
		t.Fatalf("положение не посеяно: %v", err)
	}
	return id
}

func отчёт(t *testing.T, gate *dbgate.Gate, schema, slug string,
	adopt bool) Report {
	t.Helper()
	out, err := Import(context.Background(), старая{gate, schema}, gate, slug,
		Profile{Purpose: "topic", Hierarchy: "is-a", Completeness: "complete"},
		adopt, time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ввоз отказал: %v", err)
	}
	return out
}

func TestPgВвозСчитаетПутьИзСвязей(t *testing.T) {
	// Соблазн отрезать знаки от кода МКБ здесь особенно силён: для МКБ он
	// сработал бы. Он сработал бы ровно для одного источника на свете.
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	посеять(t, gate, schema, slug)

	out := отчёт(t, gate, schema, slug, false)

	var path string
	var depth int
	if err := gate.QueryRow(context.Background(),
		`SELECT path, depth FROM source_units
		  WHERE source_id = $1 AND label = 'F32.1'`, out.SourceID).
		Scan(&path, &depth); err != nil {
		t.Fatal(err)
	}
	if path != "F3/F32/F32.1" {
		t.Errorf("путь %q, а связи дают F3/F32/F32.1", path)
	}
	if depth != 3 {
		t.Errorf("глубина %d, а в пути три шага", depth)
	}
}

func TestPgВвозБезНазванныхСвойствОтказывает(t *testing.T) {
	// Ось классификации, смысл вложенности и полнота — то, чего прежняя
	// схема не знала. Умолчание здесь означало бы тихо неверные доли
	// охвата.
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	посеять(t, gate, schema, slug)

	_, err := Import(context.Background(), старая{gate, schema}, gate, slug,
		Profile{}, false, time.Now())
	if err == nil {
		t.Error("ввоз прошёл без названных свойств источника")
	}
}

func TestPgВвозЗадачиПереводитТелоИСтавитПуть(t *testing.T) {
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	oldID := посеять(t, gate, schema, slug)

	id := уникальный(t)
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.cases
		(id, status, origin, body, source_id) VALUES ($1,'published','manual',$2,$3)`,
		id, тело("F32.1", []string{"F32.1", "F41"}, nil), oldID); err != nil {
		t.Fatal(err)
	}

	out := отчёт(t, gate, schema, slug, false)
	if out.Cases != 1 {
		t.Fatalf("ввезено задач %d, посеяна одна: %v", out.Cases, out.Skipped)
	}

	var label, path string
	var raw []byte
	if err := gate.QueryRow(context.Background(),
		`SELECT unit_label, unit_path, body FROM cases WHERE id = $1`, id).
		Scan(&label, &path, &raw); err != nil {
		t.Fatal(err)
	}
	if label != "F32.1" || path != "F3/F32/F32.1" {
		t.Errorf("метка %q, путь %q", label, path)
	}

	var body struct {
		Kind     string `json:"kind"`
		Answer   string `json:"answer"`
		Segments []struct {
			Statements []string `json:"statements"`
		} `json:"segments"`
		Options []struct {
			Label string `json:"label"`
		} `json:"options"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Kind != "recognise" || body.Answer != "F32.1" {
		t.Errorf("вид %q, ответ %q", body.Kind, body.Answer)
	}
	// Ссылка переведена в ОБОЗНАЧЕНИЕ первоисточника, а не в наш номер.
	if len(body.Segments) != 1 || len(body.Segments[0].Statements) != 1 ||
		body.Segments[0].Statements[0] != "G1" {
		t.Errorf("разметка фрагмента переведена не обозначением: %+v", body.Segments)
	}
	if len(body.Options) != 2 || body.Options[0].Label != "F32.1" {
		t.Errorf("варианты переведены без меток: %+v", body.Options)
	}
}

func TestPgУдалённыеЗадачиНеВвозятся(t *testing.T) {
	// Ввезти удалённую значит вернуть к жизни то, что врач выбросил, —
	// причём молча и без «Корзины», из которой её можно выбросить снова.
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	oldID := посеять(t, gate, schema, slug)

	id := уникальный(t)
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.cases
		(id, status, origin, body, source_id, deleted_at)
		VALUES ($1,'published','manual',$2,$3, NOW())`,
		id, тело("F32.1", []string{"F32.1", "F41"}, nil), oldID); err != nil {
		t.Fatal(err)
	}

	out := отчёт(t, gate, schema, slug, false)
	if out.Cases != 0 {
		t.Errorf("ввезена задача из корзины")
	}
	if len(out.Skipped) != 0 {
		t.Errorf("удалённая попала в пропущенные: её там быть не должно — "+
			"она не пропущена, а не рассматривалась: %v", out.Skipped)
	}
}

func TestPgЗадачиЧужогоИсточникаНеВвозятся(t *testing.T) {
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	посеять(t, gate, schema, slug)

	// Второй источник в прежней базе и задача при нём.
	var другой int64
	if err := gate.QueryRow(context.Background(), `INSERT INTO `+schema+`.sources
		(slug, kind, title, unit_word, statement_word)
		VALUES ($1,'decree','Приказ','пункт','требование') RETURNING id`,
		уникальный(t)).Scan(&другой); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.cases
		(id, status, origin, body, source_id) VALUES ($1,'published','manual',$2,$3)`,
		уникальный(t), тело("F32.1", []string{"F32.1", "F41"}, nil), другой); err != nil {
		t.Fatal(err)
	}

	if out := отчёт(t, gate, schema, slug, false); out.Cases != 0 {
		t.Errorf("ввезена задача чужого источника")
	}
}

func TestPgЗадачиБезИсточникаБерутсяТолькоПоПросьбе(t *testing.T) {
	// Они старше самого понятия источника. Приписать их можно, но только
	// назвав это вслух: молчаливое присвоение заметить потом нечем.
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	посеять(t, gate, schema, slug)

	id := уникальный(t)
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.cases
		(id, status, origin, body, source_id) VALUES ($1,'published','manual',$2,NULL)`,
		id, тело("F32.1", []string{"F32.1", "F41"}, nil)); err != nil {
		t.Fatal(err)
	}

	if out := отчёт(t, gate, schema, slug, false); out.Cases != 0 {
		t.Errorf("задача без источника ввезена без просьбы")
	}
	if out := отчёт(t, gate, schema, slug, true); out.Cases != 1 {
		t.Errorf("задача без источника не ввезена и по просьбе: %v", out.Skipped)
	}
}

func TestPgНепереводимаяЗадачаПропускаетсяСПричиной(t *testing.T) {
	// Молча пропущенная половина задач выглядит как «их столько и было».
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	oldID := посеять(t, gate, schema, slug)

	// Верного ответа нет среди вариантов: задача не решается никем.
	id := уникальный(t)
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.cases
		(id, status, origin, body, source_id) VALUES ($1,'published','manual',$2,$3)`,
		id, тело("F32.1", []string{"F41", "F3"}, nil), oldID); err != nil {
		t.Fatal(err)
	}

	out := отчёт(t, gate, schema, slug, false)
	if out.Cases != 0 {
		t.Error("нерешаемая задача ввезена")
	}
	if out.Skipped[id] == "" {
		t.Error("пропуск не назван причиной")
	}
}

func TestPgЗадачаПоНеизвестнойЕдиницеПропускается(t *testing.T) {
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	oldID := посеять(t, gate, schema, slug)

	id := уникальный(t)
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.cases
		(id, status, origin, body, source_id) VALUES ($1,'published','manual',$2,$3)`,
		id, тело("Z99", []string{"Z99", "F41"}, nil), oldID); err != nil {
		t.Fatal(err)
	}

	out := отчёт(t, gate, schema, slug, false)
	if out.Cases != 0 || out.Skipped[id] == "" {
		t.Errorf("задача по неизвестной единице ввезена или пропущена молча: %v", out.Skipped)
	}
}

func TestPgРешаемостьВвозитсяПомеченнойЧужой(t *testing.T) {
	// Она мерила другую аудиторию на другом приложении. Сложить её с
	// нашей значит получить среднее по двум разным вещам.
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	oldID := посеять(t, gate, schema, slug)

	id := уникальный(t)
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.cases
		(id, status, origin, body, source_id) VALUES ($1,'published','manual',$2,$3)`,
		id, тело("F32.1", []string{"F32.1", "F41"}, nil), oldID); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.rollup_case_stats
		(case_id, attempts, correct, median_solve_ms) VALUES ($1, 40, 10, 4200)`,
		id); err != nil {
		t.Fatal(err)
	}

	out := отчёт(t, gate, schema, slug, false)
	if out.Stats != 1 {
		t.Fatalf("решаемость не ввезена: %+v", out)
	}

	var origin string
	var rate float32
	var median int64
	if err := gate.QueryRow(context.Background(),
		`SELECT origin, solve_rate, median_ms FROM case_stats WHERE case_id = $1`, id).
		Scan(&origin, &rate, &median); err != nil {
		t.Fatal(err)
	}
	if origin != "imported" {
		t.Errorf("ввезённая решаемость помечена как %q", origin)
	}
	if rate < 0.24 || rate > 0.26 {
		t.Errorf("доля решивших %v, а посеяно 10 из 40", rate)
	}
	if median != 4200 {
		t.Errorf("медиана %d", median)
	}
}

func TestPgРешаемостьБезПопытокНеВвозится(t *testing.T) {
	// Ноль попыток — это отсутствие сведений, а не решаемость ноль. Доля,
	// посчитанная из нуля, выглядела бы как «задачу не решил никто».
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	oldID := посеять(t, gate, schema, slug)

	id := уникальный(t)
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.cases
		(id, status, origin, body, source_id) VALUES ($1,'published','manual',$2,$3)`,
		id, тело("F32.1", []string{"F32.1", "F41"}, nil), oldID); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.rollup_case_stats
		(case_id, attempts, correct) VALUES ($1, 0, 0)`, id); err != nil {
		t.Fatal(err)
	}

	if out := отчёт(t, gate, schema, slug, false); out.Stats != 0 {
		t.Error("ввезена решаемость без единой попытки")
	}
}

func TestPgПовторныйВвозНеЗадваивает(t *testing.T) {
	// Обрыв посреди ввоза — обычное дело: база чужая, сеть между ними
	// тоже. Повторный заход обязан досчитывать, а не задваивать.
	gate := testGate(t)
	schema := уникальная(t)
	прежняя(t, gate, schema)
	slug := уникальный(t)
	oldID := посеять(t, gate, schema, slug)

	id := уникальный(t)
	if _, err := gate.Exec(context.Background(), `INSERT INTO `+schema+`.cases
		(id, status, origin, body, source_id) VALUES ($1,'published','manual',$2,$3)`,
		id, тело("F32.1", []string{"F32.1", "F41"}, nil), oldID); err != nil {
		t.Fatal(err)
	}

	first := отчёт(t, gate, schema, slug, false)
	second := отчёт(t, gate, schema, slug, false)

	if second.SourceID != first.SourceID {
		t.Errorf("второй заход завёл второй источник: %d против %d",
			second.SourceID, first.SourceID)
	}
	if second.Units != 0 || second.Statements != 0 || second.Cases != 0 {
		t.Errorf("второй заход записал заново: %+v", second)
	}

	var units, statements, cases int
	ctx := context.Background()
	if err := gate.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM source_units WHERE source_id = $1),
		(SELECT count(*) FROM source_unit_statements WHERE source_id = $1),
		(SELECT count(*) FROM cases WHERE source_id = $1)`, first.SourceID).
		Scan(&units, &statements, &cases); err != nil {
		t.Fatal(err)
	}
	if units != first.Units || statements != first.Statements || cases != first.Cases {
		t.Errorf("в базе %d единиц, %d положений, %d задач; ввезено %d/%d/%d",
			units, statements, cases, first.Units, first.Statements, first.Cases)
	}
}

var счётчик int

// уникальный — имя, не совпадающее с чужим: проверочная база общая, и
// прогоны идут рядом.
func уникальный(t *testing.T) string {
	t.Helper()
	счётчик++
	return fmt.Sprintf("imp-%d-%d", time.Now().UnixNano(), счётчик)
}

func уникальная(t *testing.T) string {
	t.Helper()
	счётчик++
	return fmt.Sprintf("old_%d_%d", time.Now().UnixNano(), счётчик)
}
