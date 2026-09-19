package schema

import (
	"context"

	"curator/server/internal/dbgate"
)

// Snapshot снимает с базы то, что в ней есть сейчас: таблица → колонка →
// тип, как его называет сам postgres.
//
// Тип берётся из udt_name, а не из data_type. data_type говорит «bigint»
// и «character varying» — красиво для человека и бесполезно для сверки:
// массив в нём назван «ARRAY», и text[] от bigint[] не отличить. udt_name
// говорит int8 и _text, то есть различает то, что нам и надо различать.
//
// Смотрим только в public: расширения заводят свои таблицы в своих схемах,
// и догоняющему накату до них дела нет.
func Snapshot(ctx context.Context, gate *dbgate.Gate) (Existing, error) {
	rows, err := gate.Query(ctx, `
		SELECT table_name, column_name, udt_name
		  FROM information_schema.columns
		 WHERE table_schema = 'public'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := Existing{}
	for rows.Next() {
		var table, column, udt string
		if err := rows.Scan(&table, &column, &udt); err != nil {
			return nil, err
		}
		if out[table] == nil {
			out[table] = map[string]string{}
		}
		out[table][column] = udt
	}
	return out, rows.Err()
}
