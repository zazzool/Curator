package schema

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"curator/server/internal/dbgate"
)

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

func readSchema(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "schema.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("схема %s не прочитана: %v", path, err)
	}
	return string(raw)
}

// TestPgБазаПрежнегоВыпускаДогоняетсяНакатом — та самая проверка, ради
// которой всё это написано.
//
// Разыгрывается настоящий случай: таблица заведена прежним выпуском, в
// схему с тех пор дописали колонку. CREATE TABLE IF NOT EXISTS такую
// таблицу пропускает целиком и об этом молчит — колонка не появляется, а
// накат отчитывается об успехе. Отказ вылезает позже, при первом
// обращении: у врача, а не в журнале выкатки.
func TestPgБазаПрежнегоВыпускаДогоняетсяНакатом(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)
	name := fmt.Sprintf("прежний_выпуск_%d", time.Now().UnixNano())

	// Таблица, какой её завёл прежний выпуск: без колонки про кэш.
	if _, err := gate.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %s (id BIGSERIAL PRIMARY KEY, prompt_tokens BIGINT NOT NULL DEFAULT 0)`,
		name)); err != nil {
		t.Fatalf("таблица прежнего выпуска не заведена: %v", err)
	}
	t.Cleanup(func() {
		_, _ = gate.Exec(context.Background(), "DROP TABLE IF EXISTS "+name)
	})

	// Схема сегодняшнего выпуска: колонка дописана.
	today := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    id             BIGSERIAL PRIMARY KEY,
    prompt_tokens  BIGINT NOT NULL DEFAULT 0,
    cached_tokens  BIGINT NOT NULL DEFAULT 0
);`, name)

	// Обычный накат проходит и ничего не меняет — в этом и беда.
	for _, stmt := range Split(today) {
		if _, err := gate.Exec(ctx, stmt); err != nil {
			t.Fatalf("накат отказал: %v", err)
		}
	}
	have, err := Snapshot(ctx, gate)
	if err != nil {
		t.Fatalf("снимок базы не снят: %v", err)
	}
	if _, present := have[name]["cached_tokens"]; present {
		t.Fatal("колонка появилась сама — проверка потеряла смысл, " +
			"CREATE TABLE IF NOT EXISTS таблицу не трогает")
	}

	// Догоняющий накат её добавляет.
	stmts, bad := Grow(Tables(Split(today)), have)
	if len(bad) != 0 {
		t.Fatalf("расхождения на ровном месте: %v", bad)
	}
	if len(stmts) != 1 {
		t.Fatalf("команд догоняющего наката %d, ожидалась 1: %q", len(stmts), stmts)
	}
	for _, stmt := range stmts {
		if _, err := gate.Exec(ctx, stmt); err != nil {
			t.Fatalf("догоняющий накат отказал: %v\n%s", err, stmt)
		}
	}

	have, err = Snapshot(ctx, gate)
	if err != nil {
		t.Fatal(err)
	}
	if have[name]["cached_tokens"] != "int8" {
		t.Fatalf("колонка не доехала: %+v", have[name])
	}

	// Повторный прогон обязан не найти работы: накат идёт при каждой
	// выкатке, и второй раз он должен быть пустым, а не отказать.
	again, bad := Grow(Tables(Split(today)), have)
	if len(again) != 0 || len(bad) != 0 {
		t.Fatalf("повторный прогон нашёл работу: %q, %v", again, bad)
	}
}

// TestPgСхемаПроектаНаСвежейБазеНичегоНеДогоняет — сторож самого разбора.
//
// База, только что накатанная целиком, обязана совпасть со схемой поле в
// поле. Найдётся расхождение — значит разбор понял объявление не так, как
// понял его postgres, и догоняющий накат при первой же выкатке начнёт
// добавлять то, что уже есть.
func TestPgСхемаПроектаНаСвежейБазеНичегоНеДогоняет(t *testing.T) {
	ctx := context.Background()
	gate := testGate(t)

	text := readSchema(t)
	for i, stmt := range Split(text) {
		if _, err := gate.Exec(ctx, stmt); err != nil {
			t.Fatalf("команда %d схемы не накатилась: %v", i+1, err)
		}
	}
	have, err := Snapshot(ctx, gate)
	if err != nil {
		t.Fatal(err)
	}
	stmts, bad := Grow(Tables(Split(text)), have)
	if len(bad) != 0 {
		t.Errorf("схема разошлась с накатанной базой: %v", bad)
	}
	if len(stmts) != 0 {
		t.Errorf("на свежей базе нашлось что догонять — разбор понял схему не как postgres:\n%q", stmts)
	}
}
