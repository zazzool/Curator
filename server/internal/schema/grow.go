// Догоняющий накат: рост существующей базы вслед за схемой.
//
// # Зачем это вообще нужно
//
// Таблицы в schema.sql заводятся через CREATE TABLE IF NOT EXISTS, и это
// правильно: повторный накат обязан быть безопасным. Но у такого наката
// есть провал, и он молчаливый. Колонка, дописанная в уже объявленную
// таблицу, до существующей базы НЕ ДОЕЗЖАЕТ: таблица есть, значит команда
// пропускается целиком — вместе с новой колонкой. Накат при этом
// отчитывается об успехе.
//
// Поймано это было случайно, на проверочной базе, заведённой раньше, чем в
// llm_calls появились колонки про префиксный кэш: набор упал пятью
// отказами «column does not exist» — то есть не при накате, а при первом
// обращении. На боевой базе это означало бы отказ у врача, а не в журнале
// выкатки. Цена ошибки здесь как раз та, ради которой в правилах написано
// «накат применяется до подмены кода».
//
// # Почему не список ALTER руками
//
// Очевидное решение — дописывать в конец схемы
// ALTER TABLE … ADD COLUMN IF NOT EXISTS на каждую новую колонку. Оно
// отвергнуто: это второе место для того же самого объявления, а два места
// для одного расходятся молча. Забыть дописать ALTER нечем помешать, а
// заметить пропажу можно только на обновлении боевой базы — то есть там же,
// где и сейчас.
//
// Поэтому ALTER'ы не пишутся, а СЧИТАЮТСЯ: объявление колонки в схеме
// единственное, и догоняющий накат читает его же.
//
// # Что он делает и чего не делает
//
// Добавляет недостающие колонки. Не удаляет лишние: схема растёт только
// добавлением, а колонка, пропавшая из схемы, могла остаться от выпуска,
// который ещё крутится рядом.
//
// Расхождение типа он НЕ ПРАВИТ, а называет вслух и отказывает. Смена типа
// у живой колонки — это перенос данных, а не накат, и решать его молча,
// подставив ALTER … TYPE, значит однажды переписать деньги. Тип, который
// разобрать не удалось, не сверяется вовсе и говорит об этом: выдумать
// расхождение там, где его нет, хуже, чем не сверить, — ложная тревога
// останавливает выкатку и учит не верить сторожу.
package schema

import (
	"fmt"
	"sort"
	"strings"
)

// Table — таблица, как она объявлена в схеме.
type Table struct {
	Name    string
	Columns []Column
}

// Column — колонка, как она объявлена.
//
// Def хранится целиком, вместе с NOT NULL, DEFAULT и ссылками: догоняющий
// накат обязан завести колонку ровно такой, какой её заводит CREATE TABLE,
// иначе база, доросшая накатом, и база, заведённая с нуля, разойдутся — и
// разойдутся молча.
type Column struct {
	Name string
	Type string
	Def  string
}

// Existing — что в базе сейчас: таблица → колонка → тип, как его называет
// сам postgres (udt_name: int8, text, timestamptz, jsonb, bool).
type Existing map[string]map[string]string

// Mismatch — расхождение, которое накат не берётся править сам.
type Mismatch struct {
	Table  string
	Column string
	Want   string
	Have   string
}

func (m Mismatch) Error() string {
	return fmt.Sprintf("%s.%s: в схеме %s, в базе %s", m.Table, m.Column, m.Want, m.Have)
}

// Tables вынимает объявления таблиц из команд схемы.
//
// Всё, что не CREATE TABLE, пропускается молча: в схеме есть индексы,
// представления и вставки словарей, и они догоняющего наката не касаются.
func Tables(stmts []string) []Table {
	out := []Table{}
	for _, stmt := range stmts {
		name, body, ok := head(stmt)
		if !ok {
			continue
		}
		cols := []Column{}
		for _, part := range pieces(body) {
			col, ok := column(part)
			if ok {
				cols = append(cols, col)
			}
		}
		if len(cols) > 0 {
			out = append(out, Table{Name: name, Columns: cols})
		}
	}
	return out
}

// Grow считает, чего базе недостаёт.
//
// Таблицы, которой в базе нет вовсе, здесь не касаются: её заводит сам
// CREATE TABLE, и заводит целиком.
func Grow(tables []Table, have Existing) ([]string, []Mismatch) {
	stmts := []string{}
	var bad []Mismatch

	for _, t := range tables {
		columns, known := have[t.Name]
		if !known {
			continue
		}
		for _, c := range t.Columns {
			actual, present := columns[c.Name]
			if !present {
				stmts = append(stmts, fmt.Sprintf(
					"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s", t.Name, c.Name, c.Def))
				continue
			}
			want, comparable := udt(c.Type)
			if comparable && want != actual {
				bad = append(bad, Mismatch{
					Table: t.Name, Column: c.Name, Want: c.Type, Have: actual,
				})
			}
		}
	}
	sort.Strings(stmts)
	return stmts, bad
}

// head разбирает начало объявления: имя таблицы и тело в скобках.
func head(stmt string) (string, string, bool) {
	flat := strings.Join(strings.Fields(stmt), " ")
	const prefix = "CREATE TABLE IF NOT EXISTS "
	if !strings.HasPrefix(strings.ToUpper(flat), prefix) {
		return "", "", false
	}
	rest := strings.TrimSpace(flat[len(prefix):])
	open := strings.Index(rest, "(")
	close := strings.LastIndex(rest, ")")
	if open <= 0 || close <= open {
		return "", "", false
	}
	name := strings.TrimSpace(rest[:open])
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", "", false
	}
	return name, rest[open+1 : close], true
}

// pieces режет тело таблицы по запятым верхнего уровня.
//
// По верхнему уровню, а не по всякой запятой: CHECK (a IN ('x','y')) и
// REFERENCES t (a, b) полны запятых внутри скобок, и резать по ним значит
// получить обрывки вместо объявлений.
func pieces(body string) []string {
	var out []string
	depth, quoted, start := 0, false, 0
	for i, r := range body {
		switch {
		case r == '\'':
			quoted = !quoted
		case quoted:
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == ',' && depth == 0:
			out = append(out, body[start:i])
			start = i + 1
		}
	}
	return append(out, body[start:])
}

// keywords — то, с чего начинается ограничение таблицы, а не колонка.
var keywords = map[string]bool{
	"PRIMARY": true, "UNIQUE": true, "CHECK": true, "FOREIGN": true,
	"CONSTRAINT": true, "EXCLUDE": true, "LIKE": true,
}

func column(part string) (Column, bool) {
	fields := strings.Fields(strings.TrimSpace(part))
	if len(fields) < 2 {
		return Column{}, false
	}
	if keywords[strings.ToUpper(fields[0])] {
		return Column{}, false
	}
	return Column{
		Name: fields[0],
		Type: strings.ToUpper(fields[1]),
		Def:  strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), fields[0])),
	}, true
}

// udt переводит имя типа из схемы в то, которым его называет postgres.
//
// Словарь закрыт намеренно. Тип, которого в нём нет, не сверяется — второе
// значение false, — и это честнее догадки: ложная тревога останавливает
// выкатку и учит не верить сторожу, а несверенный тип хотя бы не врёт.
func udt(declared string) (string, bool) {
	switch declared {
	case "BIGINT", "BIGSERIAL":
		return "int8", true
	case "INTEGER", "INT", "SERIAL":
		return "int4", true
	case "SMALLINT":
		return "int2", true
	case "TEXT":
		return "text", true
	case "BOOLEAN":
		return "bool", true
	case "TIMESTAMPTZ":
		return "timestamptz", true
	case "DATE":
		return "date", true
	case "JSONB":
		return "jsonb", true
	case "BYTEA":
		return "bytea", true
	case "UUID":
		return "uuid", true
	case "TEXT[]":
		return "_text", true
	default:
		return "", false
	}
}
