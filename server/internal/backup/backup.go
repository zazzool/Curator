// Снимки базы.
//
// # Зачем
//
// Задачи, разметка и разборы существуют в одном экземпляре. Разбор
// источника и генерация стоят денег и времени составителя, а восстановить
// их из ничего нельзя: модель напишет другое. Каталог под снимки выкатка
// заводила с самого начала, права на нём выставляла — и не писал в него
// никто.
//
// # Почему pg_dump, а не свой обход таблиц
//
// Свой обход пишет то, что помнил писавший. Новая таблица в схеме в него
// не попадёт — и не попадёт молча, потому что снимок всё равно получится.
// Выяснится это в тот единственный день, когда снимок нужен. pg_dump
// читает схему у самой базы и не умеет о ней забыть.
//
// # Снимок, который никто не восстанавливал, — не снимок
//
// Файл может быть непустым, читаемым и негодным. Поэтому здесь есть
// Verify: снимок разворачивается в ОТДЕЛЬНУЮ базу, и развёрнутое
// сверяется с живой базой таблица за таблицей. Список таблиц берётся у
// самой базы, а не записан здесь: список, который надо пополнять руками,
// перестаёт пополняться на третьей таблице.
//
// # Почему числа строк НЕ сверяются поштучно
//
// Живая база растёт всё время, пока идут съёмка и разворачивание: врач
// решает задачу, приложение шлёт телеметрию. Поштучная сверка кричала бы
// на каждый такой снимок — то есть на каждый. Проверка, кричащая всегда,
// перестаёт читаться, и настоящий отказ пройдёт вместе с ложными.
//
// Поэтому сверяется то, что не зависит от хода времени: pg_restore прошёл
// без единой ошибки (--exit-on-error), в снимке есть КАЖДАЯ таблица живой
// базы, и ни одна из них не пуста там, где в живой базе есть строки.
// Этим ловятся все три настоящих отказа: оборванный файл, выросшая схема,
// о которой снимок не знает, и снимок схемы без данных.
package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Keeper снимает и стережёт снимки.
type Keeper struct {
	// DSN живой базы: откуда снимаем.
	DSN string

	// Dir — каталог снимков. Пусто — снимки не снимаются, и служба
	// говорит об этом вслух при старте: заполненная переменная выглядит
	// как заведённая защита, а пустая обязана выглядеть как её отсутствие.
	Dir string

	// VerifyDSN — ОТДЕЛЬНАЯ база под проверку восстановлением. Проверка
	// очищает таблицы перед вставкой, и указанная сюда живая база была бы
	// стёрта собственной защитой.
	VerifyDSN string

	// Keep — сколько снимков держать. Ноль означает умолчание, а не «не
	// держать ни одного»: ноль в этом поле чаще всего незаполненная
	// переменная, и понимать её как «стереть всё» нельзя.
	Keep int

	// Now — источник времени. Полем ради проверок: им нужно разложить
	// снимки по разным дням, не дожидаясь суток.
	Now func() time.Time
}

// Snapshot — сделанный снимок.
type Snapshot struct {
	Path  string
	Bytes int64
	Taken time.Time
}

const defaultKeep = 14

// Ready говорит, есть ли куда снимать.
func (k *Keeper) Ready() bool { return k != nil && k.Dir != "" && k.DSN != "" }

func (k *Keeper) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

// Take снимает базу.
//
// Формат custom, а не текстовый SQL: он сжат, из него можно развернуть
// отдельную таблицу, и pg_restore читает его версией новее той, что
// писала. Текстовый дамп базы с задачами — это сотни мегабайт текста,
// который никто не прочтёт глазами, но за который все заплатят местом.
func (k *Keeper) Take(ctx context.Context) (Snapshot, error) {
	if !k.Ready() {
		return Snapshot{}, fmt.Errorf("снимки не настроены: каталог или строка подключения пусты")
	}
	if err := os.MkdirAll(k.Dir, 0o750); err != nil {
		return Snapshot{}, fmt.Errorf("каталог снимков недоступен: %w", err)
	}

	taken := k.now()
	path := filepath.Join(k.Dir, "curator-"+taken.UTC().Format("20060102-150405")+".dump")

	// --no-owner и --no-acl: снимок разворачивается и в проверочную базу,
	// и на другом контуре, где роли зовут иначе. Снимок, требующий роли с
	// тем же именем, восстанавливается только туда, откуда снят, — то есть
	// ровно не туда, куда нужно.
	cmd := exec.CommandContext(ctx, "pg_dump",
		"--format=custom", "--no-owner", "--no-acl",
		"--file="+path, k.DSN)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Недописанный файл убираем: снимок на половине — это снимок,
		// который выглядит как снимок.
		_ = os.Remove(path)
		return Snapshot{}, fmt.Errorf("снимок не снят: %w: %s", err, strings.TrimSpace(string(out)))
	}

	info, err := os.Stat(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("снимок не найден после съёмки: %w", err)
	}
	if info.Size() == 0 {
		_ = os.Remove(path)
		return Snapshot{}, fmt.Errorf("снимок пуст: %s", path)
	}
	return Snapshot{Path: path, Bytes: info.Size(), Taken: taken}, nil
}

// Verify разворачивает снимок в отдельную базу и сверяет числа строк.
//
// Отказ здесь означает, что снимка нет, — как бы ни выглядел файл.
func (k *Keeper) Verify(ctx context.Context, path string) error {
	if k.VerifyDSN == "" {
		return fmt.Errorf("проверять снимок негде: не задана отдельная база")
	}
	if k.VerifyDSN == k.DSN {
		// Проверка очищает таблицы перед вставкой. Совпади базы — защита
		// стёрла бы то, что стережёт.
		return fmt.Errorf("база проверки совпадает с живой: проверка стёрла бы боевые данные")
	}

	// --clean --if-exists: база проверки остаётся от прошлого раза, и
	// вставка в непустую упала бы на первом же ключе. --exit-on-error,
	// потому что pg_restore по умолчанию ДОКЛАДЫВАЕТ об ошибках по
	// отдельным объектам и продолжает: без этого довода он возвращает
	// успех, восстановив половину.
	//
	// Довод этот проверкой НЕ покрыт, и снимать его поэтому нельзя:
	// оборванный файл pg_restore отвергает и без него (это и проверяет
	// TestPgОборванныйСнимокНеПроходитСверку), а отказ по отдельному
	// объекту подделать здесь нечем — нашей схеме не нужны ни расширения,
	// ни роли.
	cmd := exec.CommandContext(ctx, "pg_restore",
		"--clean", "--if-exists", "--no-owner", "--no-acl",
		"--exit-on-error", "--dbname="+k.VerifyDSN, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("снимок не разворачивается: %w: %s", err, strings.TrimSpace(string(out)))
	}

	live, err := counts(ctx, k.DSN)
	if err != nil {
		return fmt.Errorf("живая база не сосчитана: %w", err)
	}
	restored, err := counts(ctx, k.VerifyDSN)
	if err != nil {
		return fmt.Errorf("развёрнутая база не сосчитана: %w", err)
	}
	if len(live) == 0 {
		// Пустой список таблиц — это не «всё сошлось», а «сверять было
		// нечем». Молчаливый успех здесь хуже отказа.
		return fmt.Errorf("в живой базе не нашлось ни одной таблицы: сверять нечего")
	}

	var разошлись []string
	for table, n := range live {
		count, есть := restored[table]
		switch {
		case !есть:
			// Таблицы нет вовсе. Ровно этого и боялись, заводя сверку:
			// схема выросла, снимок о новой таблице не знает, и снимок
			// при этом получается — непустой, читаемый и негодный.
			разошлись = append(разошлись, table+": в снимке такой таблицы нет")
		case n > 0 && count == 0:
			// Таблица есть, а строк в ней нет. Так выглядит снимок,
			// развернувший схему и не развернувший данные.
			разошлись = append(разошлись,
				fmt.Sprintf("%s: в базе %d строк, в снимке ни одной", table, n))
		}
	}
	if len(разошлись) > 0 {
		sort.Strings(разошлись)
		return fmt.Errorf("снимок неполон: %s", strings.Join(разошлись, "; "))
	}
	return nil
}

// counts считает строки в каждой таблице.
//
// Список таблиц берётся у самой базы. Записанный здесь список пришлось бы
// пополнять руками, а руками его перестанут пополнять на третьей таблице —
// и снимок без новой таблицы пройдёт проверку молча.
func counts(ctx context.Context, dsn string) (map[string]int64, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx,
		`SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename`)
	if err != nil {
		return nil, err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make(map[string]int64, len(tables))
	for _, table := range tables {
		var n int64
		// Имя таблицы приходит из pg_tables, а не снаружи, но в запрос
		// оно всё равно уезжает кавычками: незакавыченное имя однажды
		// окажется ключевым словом, и отказ будет непонятным.
		if err := conn.QueryRow(ctx,
			`SELECT count(*) FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`).Scan(&n); err != nil {
			return nil, fmt.Errorf("таблица %s: %w", table, err)
		}
		out[table] = n
	}
	return out, nil
}

// Prune убирает старые снимки, оставляя последние Keep.
//
// Возвращает, сколько убрал. Каталог, который никто не чистит,
// заканчивается кончившимся местом на диске — и заканчивается он им в тот
// день, когда снимок нужнее всего.
func (k *Keeper) Prune() (int, error) {
	keep := k.Keep
	if keep <= 0 {
		keep = defaultKeep
	}
	entries, err := os.ReadDir(k.Dir)
	if err != nil {
		return 0, fmt.Errorf("каталог снимков не прочитан: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "curator-") && strings.HasSuffix(e.Name(), ".dump") {
			names = append(names, e.Name())
		}
	}
	// Имя несёт отметку времени в том виде, в каком она сортируется как
	// строка. Порядок по времени файла был бы неверен после копирования
	// каталога: копия получает время копирования, и самым свежим стал бы
	// самый старый.
	sort.Strings(names)
	if len(names) <= keep {
		return 0, nil
	}
	removed := 0
	for _, name := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(k.Dir, name)); err != nil {
			return removed, fmt.Errorf("старый снимок не убран: %w", err)
		}
		removed++
	}
	return removed, nil
}
