// Накат схемы.
//
// Схема накатывается на контуре, а не с машины разработчика: база видна
// только из сети сервера. Поэтому утилита едет вместе с сервером в его
// образ, а не остаётся в дереве исходников.
//
// # Почему показ, а не накат по умолчанию
//
// Без -apply утилита только считает команды и ничего не меняет. Накат — это
// действие на боевой базе, и умолчание, которое его совершает, однажды
// совершит его не там: строка подключения берётся из окружения, а окружение
// у соседнего терминала бывает другим.
//
// Схема растёт только добавлением, все команды идемпотентны, поэтому
// повторный накат безопасен и делается при каждой выкатке — до подмены
// кода. Код, вышедший раньше своей схемы, падает не при старте, а на первом
// обращении к недостающей колонке, то есть у врача, а не в журнале выкатки.
//
// # Три захода, а не один
//
// Сперва команды схемы, потом догоняющий накат, потом указатели. Второй заход нужен потому,
// что CREATE TABLE IF NOT EXISTS растит базу только до первой выкатки:
// таблица уже есть — команда пропускается целиком, вместе с колонкой,
// дописанной в неё на прошлой неделе. И пропускается молча. Разбор,
// который считает недостающее по самой схеме, — в internal/schema; там же
// записано, почему это считается, а не пишется ALTER'ами руками.
//
// Третий заход — по той же причине, но на уровень выше: указатель по
// колонке, дописанной в объявление таблицы, обязан идти ПОСЛЕ догоняющего
// наката. Пойди он в общем порядке — и на живой базе он встретит колонку,
// которой ещё нет, а на пустой всё сойдётся. Отказ, случающийся только
// там, где база уже есть, — это отказ только на бою.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/envfile"
	"curator/server/internal/schema"
)

func main() {
	apply := flag.Bool("apply", false, "выполнить команды, а не только показать их число")
	path := flag.String("schema", "schema.sql", "путь к файлу схемы")
	flag.Parse()

	if err := envfile.Load(".env"); err != nil {
		log.Fatalf("настройки из .env не прочитаны: %v", err)
	}

	text, err := os.ReadFile(*path)
	if err != nil {
		log.Fatalf("схема не прочитана: %v", err)
	}
	stmts := schema.Split(string(text))
	fmt.Printf("команд в схеме: %d\n", len(stmts))
	if !*apply {
		fmt.Println("это показ; чтобы накатить, добавьте -apply")
		return
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// Отказ, а не молчаливый успех: накат, которому некуда катить,
		// обязан сказать об этом громко — иначе выкатка сочтёт схему
		// применённой.
		log.Fatal("DATABASE_URL не задан: накатывать некуда")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	gate, err := dbgate.Open(ctx, dsn, dbgate.Options{})
	if err != nil {
		log.Fatalf("база: %v", err)
	}
	defer gate.Close()

	// Каждая команда своей транзакцией, а не всё одной: половина схемы,
	// откатившаяся из-за одной опечатки, оставляет базу в том же
	// состоянии, что и до наката, — но найти опечатку по «откатилось всё»
	// труднее, чем по номеру команды, на которой стало.
	//
	// Указатели откладываются на потом, и это не уборка, а порядок,
	// без которого накат встаёт. Указатель по колонке, дописанной в
	// объявление таблицы на этой неделе, на живой базе этой колонки ещё
	// не застаёт: её добавляет догоняющий заход, а он идёт ПОСЛЕ команд
	// схемы. Выкатка падала на «column … does not exist» — и падала бы
	// только на базах, которые уже есть, то есть на боевой и ни на одной
	// пустой.
	tables, indexes := apart(stmts)
	for i, stmt := range tables {
		if _, err := gate.Exec(ctx, stmt); err != nil {
			log.Fatalf("команда %d из %d не выполнена: %v\n%s", i+1, len(tables), err, first(stmt))
		}
	}
	fmt.Printf("накатано команд: %d\n", len(tables))

	grow(ctx, gate, stmts)

	for i, stmt := range indexes {
		if _, err := gate.Exec(ctx, stmt); err != nil {
			log.Fatalf("указатель %d из %d не заведён: %v\n%s",
				i+1, len(indexes), err, first(stmt))
		}
	}
	fmt.Printf("указателей: %d\n", len(indexes))
}

// apart делит команды схемы на всё прочее и заведение указателей.
//
// Порядок внутри каждой половины сохраняется: команды схемы ссылаются
// друг на друга, и перестановка соседей сломала бы накат на пустой базе,
// где сейчас всё сходится.
func apart(stmts []string) (rest, indexes []string) {
	for _, stmt := range stmts {
		upper := strings.ToUpper(strings.TrimSpace(stmt))
		if strings.HasPrefix(upper, "CREATE INDEX") ||
			strings.HasPrefix(upper, "CREATE UNIQUE INDEX") {
			indexes = append(indexes, stmt)
			continue
		}
		rest = append(rest, stmt)
	}
	return rest, indexes
}

// grow догоняет базу, заведённую прежним выпуском.
func grow(ctx context.Context, gate *dbgate.Gate, stmts []string) {
	have, err := schema.Snapshot(ctx, gate)
	if err != nil {
		log.Fatalf("снимок базы не снят: %v", err)
	}
	add, bad := schema.Grow(schema.Tables(stmts), have)

	// Расхождение типа называется и останавливает накат. Править его
	// самому нельзя: смена типа у живой колонки — это перенос данных, и
	// ALTER … TYPE, подставленный молча, однажды перепишет деньги.
	if len(bad) > 0 {
		for _, m := range bad {
			fmt.Fprintf(os.Stderr, "расхождение типа: %s\n", m.Error())
		}
		log.Fatalf("накат остановлен: расхождений типов %d. "+
			"Это перенос данных, а не накат, и делается он отдельно", len(bad))
	}

	if len(add) == 0 {
		fmt.Println("догонять нечего: база совпадает со схемой")
		return
	}
	for i, stmt := range add {
		if _, err := gate.Exec(ctx, stmt); err != nil {
			log.Fatalf("догоняющая команда %d из %d не выполнена: %v\n%s",
				i+1, len(add), err, first(stmt))
		}
		fmt.Println(stmt)
	}
	fmt.Printf("догнано колонок: %d\n", len(add))
}

// first — начало команды для сообщения об отказе. Целиком команда в журнал
// не пишется: в схеме есть команды на десятки строк, и человек ищет глазами
// первую.
func first(stmt string) string {
	if len(stmt) > 200 {
		return stmt[:200] + "…"
	}
	return stmt
}
