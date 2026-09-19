package schema

import (
	"strings"
	"testing"
)

func TestTablesВынимаетКолонкиИПропускаетОграничения(t *testing.T) {
	// Ограничение таблицы — не колонка, и выданное за колонку оно
	// превратилось бы в ALTER TABLE ADD COLUMN PRIMARY (…), то есть в
	// отказ наката на ровном месте.
	stmts := Split(`CREATE TABLE IF NOT EXISTS model_prices (
    provider             TEXT        NOT NULL,
    model                TEXT        NOT NULL,
    prompt_nano_usd      BIGINT      NOT NULL DEFAULT 0,
    PRIMARY KEY (provider, model)
);
CREATE INDEX IF NOT EXISTS idx_x ON model_prices (model);
`)
	tables := Tables(stmts)
	if len(tables) != 1 {
		t.Fatalf("таблиц %d, ожидалась 1: %+v", len(tables), tables)
	}
	if tables[0].Name != "model_prices" {
		t.Errorf("имя таблицы %q", tables[0].Name)
	}
	if len(tables[0].Columns) != 3 {
		t.Fatalf("колонок %d, ожидалось 3: %+v", len(tables[0].Columns), tables[0].Columns)
	}
	last := tables[0].Columns[2]
	if last.Name != "prompt_nano_usd" || last.Type != "BIGINT" {
		t.Errorf("третья колонка разобрана как %+v", last)
	}
	if !strings.Contains(last.Def, "DEFAULT 0") {
		t.Errorf("объявление потеряло умолчание: %q", last.Def)
	}
}

func TestTablesЗапятыеВнутриСкобокНеРежут(t *testing.T) {
	// CHECK (kind IN ('a','b')) и REFERENCES t (a, b) полны запятых, и
	// разрез по ним дал бы обрывки вместо объявлений — а обрывок, поданный
	// как колонка, уехал бы в ALTER.
	stmts := Split(`CREATE TABLE IF NOT EXISTS t (
    id    BIGSERIAL PRIMARY KEY,
    kind  TEXT NOT NULL CHECK (kind IN ('one', 'two', 'three')),
    ref   BIGINT REFERENCES other (a),
    note  TEXT NOT NULL DEFAULT ''
);`)
	tables := Tables(stmts)
	if len(tables) != 1 {
		t.Fatalf("таблиц %d", len(tables))
	}
	var names []string
	for _, c := range tables[0].Columns {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); got != "id,kind,ref,note" {
		t.Errorf("колонки разобраны как %q", got)
	}
}

func TestGrowДобавляетТолькоНедостающее(t *testing.T) {
	tables := Tables(Split(`CREATE TABLE IF NOT EXISTS llm_calls (
    id             BIGSERIAL PRIMARY KEY,
    prompt_tokens  BIGINT NOT NULL DEFAULT 0,
    cached_tokens  BIGINT NOT NULL DEFAULT 0
);`))
	have := Existing{"llm_calls": {"id": "int8", "prompt_tokens": "int8"}}

	stmts, bad := Grow(tables, have)
	if len(bad) != 0 {
		t.Fatalf("расхождения на пустом месте: %v", bad)
	}
	if len(stmts) != 1 {
		t.Fatalf("команд %d, ожидалась 1: %q", len(stmts), stmts)
	}
	want := "ALTER TABLE llm_calls ADD COLUMN IF NOT EXISTS cached_tokens BIGINT NOT NULL DEFAULT 0"
	if stmts[0] != want {
		t.Errorf("команда %q,\nожидалась %q", stmts[0], want)
	}
}

func TestGrowТаблицуКоторойНетНеТрогает(t *testing.T) {
	// Таблицу заводит сам CREATE TABLE, и заводит целиком. Догоняющий
	// накат, взявшийся добавлять в неё колонки по одной, отказал бы: её
	// ещё нет.
	tables := Tables(Split("CREATE TABLE IF NOT EXISTS свежая (id BIGSERIAL PRIMARY KEY);"))
	stmts, bad := Grow(tables, Existing{})
	if len(stmts) != 0 || len(bad) != 0 {
		t.Errorf("по отсутствующей таблице выдано %q и %v", stmts, bad)
	}
}

func TestGrowРасхождениеТипаНазываетсяИНеПравится(t *testing.T) {
	// Смена типа у живой колонки — это перенос данных, а не накат.
	// Подставив ALTER … TYPE молча, однажды перепишем деньги.
	tables := Tables(Split("CREATE TABLE IF NOT EXISTS t (cost BIGINT NOT NULL DEFAULT 0);"))
	stmts, bad := Grow(tables, Existing{"t": {"cost": "text"}})
	if len(stmts) != 0 {
		t.Errorf("расхождение типа попыталось поправиться само: %q", stmts)
	}
	if len(bad) != 1 {
		t.Fatalf("расхождений %d, ожидалось 1: %v", len(bad), bad)
	}
	if !strings.Contains(bad[0].Error(), "t.cost") {
		t.Errorf("отказ не называет колонку: %q", bad[0].Error())
	}
}

func TestGrowНеизвестныйТипНеСверяетсяИНеТревожит(t *testing.T) {
	// Ложная тревога останавливает выкатку и учит не верить сторожу.
	// Несверенный тип хотя бы не врёт.
	tables := Tables(Split("CREATE TABLE IF NOT EXISTS t (price NUMERIC(12,2) NOT NULL);"))
	_, bad := Grow(tables, Existing{"t": {"price": "int8"}})
	if len(bad) != 0 {
		t.Errorf("выдумано расхождение по типу, которого словарь не знает: %v", bad)
	}
}

func TestGrowЛишнююКолонкуНеУдаляет(t *testing.T) {
	// Схема растёт только добавлением: колонка, пропавшая из схемы, могла
	// остаться от выпуска, который ещё крутится рядом.
	tables := Tables(Split("CREATE TABLE IF NOT EXISTS t (id BIGSERIAL PRIMARY KEY);"))
	stmts, bad := Grow(tables, Existing{"t": {"id": "int8", "старое": "text"}})
	if len(stmts) != 0 || len(bad) != 0 {
		t.Errorf("лишняя колонка вызвала %q и %v", stmts, bad)
	}
}

func TestGrowВсяСхемаПроектаРазбираетсяБезПотерь(t *testing.T) {
	// Разбор, тихо потерявший таблицу, оставил бы её без догоняющего
	// наката — и заметили бы это на обновлении боевой базы.
	stmts := Split(readSchema(t))
	tables := Tables(stmts)

	var declared int
	for _, s := range stmts {
		if strings.HasPrefix(strings.ToUpper(strings.Join(strings.Fields(s), " ")),
			"CREATE TABLE IF NOT EXISTS ") {
			declared++
		}
	}
	if declared == 0 {
		t.Fatal("в схеме не нашлось ни одной таблицы — разбор сломан")
	}
	if len(tables) != declared {
		t.Fatalf("объявлено таблиц %d, разобрано %d", declared, len(tables))
	}
	for _, tbl := range tables {
		if len(tbl.Columns) == 0 {
			t.Errorf("таблица %s разобрана без колонок", tbl.Name)
		}
	}
}
