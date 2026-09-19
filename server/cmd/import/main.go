// Ввоз из прежней системы.
//
// Отдельной утилитой, а не ручкой студии, и это решение: ввоз идёт один
// раз, читает чужую базу и длится долго. Ручка, делающая такое, живёт в
// коде вечно и однажды будет нажата второй раз — не тем человеком и не на
// той базе.
//
// # Без -apply ничего не пишется
//
// Умолчание показывает, что ввоз СОБИРАЕТСЯ сделать, и ничего не меняет.
// Строка подключения берётся из окружения, а окружение у соседнего
// терминала бывает другим — и умолчание, которое пишет, однажды напишет
// не туда.
//
// # Свойства источника называет человек
//
// Ось классификации, смысл вложенности и полнота — то, чего прежняя схема
// не знала. От них зависит подбор задач и расчёт охвата, и умолчание здесь
// означало бы тихо неверные доли. Поэтому их спрашивают, и без них ввоз
// отказывает.
//
// Пример для МКБ-10:
//
//	import -source icd10 -purpose topic -hierarchy is-a \
//	       -completeness complete -adopt-unassigned -apply
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
	"curator/server/internal/envfile"
	"curator/server/internal/importer"
)

// errShow откатывает работу показа. Отдельной ошибкой, а не флагом внутри:
// откат делает сама дверь, и попросить её об этом можно только отказом.
var errShow = errors.New("показ: запись откачена")

func main() {
	log.SetFlags(0)

	slug := flag.String("source", "", "метка источника в прежней базе")
	purpose := flag.String("purpose", "", "ось классификации: topic, system, discipline, task, level, legal, other")
	hierarchy := flag.String("hierarchy", "", "смысл вложенности: part-of, is-a, grouped")
	completeness := flag.String("completeness", "", "полнота: complete или fragment")
	adopt := flag.Bool("adopt-unassigned", false,
		"приписать этому источнику задачи, у которых источник не проставлен вовсе")
	apply := flag.Bool("apply", false, "записывать; без него только показ")
	flag.Parse()

	envfile.Load(".env")

	if *slug == "" {
		log.Fatal("не назван источник: -source")
	}
	oldDSN := os.Getenv("CURATOR_OLD_DATABASE_URL")
	if oldDSN == "" {
		log.Fatal("CURATOR_OLD_DATABASE_URL не задан: читать нечего")
	}
	newDSN := os.Getenv("DATABASE_URL")
	if newDSN == "" {
		log.Fatal("DATABASE_URL не задан: писать некуда")
	}

	profile := importer.Profile{
		Purpose:      *purpose,
		Hierarchy:    *hierarchy,
		Completeness: *completeness,
	}
	// Проверяется до соединений: ругаться на свойства после минуты
	// ожидания чужой базы — значит тратить эту минуту зря.
	if err := profile.Valid(); err != nil {
		log.Fatalf("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	old, err := dbgate.Open(ctx, oldDSN, 0)
	if err != nil {
		log.Fatalf("прежняя база недоступна: %v", err)
	}
	defer old.Close()

	fresh, err := dbgate.Open(ctx, newDSN, 0)
	if err != nil {
		log.Fatalf("новая база недоступна: %v", err)
	}
	defer fresh.Close()

	if !*apply {
		// Показ идёт в откатываемой работе: так он считает ровно то же,
		// что запишет накат, а не отдельным запросом, который разойдётся
		// с ним молча.
		log.Println("показ: ничего не записывается (для записи нужен -apply)")
	}

	report, err := run(ctx, old, fresh, *slug, profile, *adopt, *apply)
	if err != nil {
		log.Fatalf("ввоз отказал: %v", err)
	}

	fmt.Printf("источник %s (номер %d)\n", *slug, report.SourceID)
	fmt.Printf("  единиц:      %d\n", report.Units)
	fmt.Printf("  положений:   %d\n", report.Statements)
	fmt.Printf("  задач:       %d\n", report.Cases)
	fmt.Printf("  решаемость:  %d\n", report.Stats)

	if len(report.Skipped) > 0 {
		// Пропущенные называются поимённо и с причиной. Число без причин
		// не говорит ничего: по причинам видно, чего не знает модель, а по
		// числу — только что «часть не доехала».
		fmt.Printf("  пропущено:   %d\n", len(report.Skipped))
		ids := make([]string, 0, len(report.Skipped))
		for id := range report.Skipped {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Printf("    %s: %s\n", id, report.Skipped[id])
		}
	}
}

// run делает ввоз внутри работы и откатывает её, если это показ.
//
// Одной работой, а не по частям: ввоз наполовину — это дерево без задач
// или задачи без дерева, и разбирать такое пришлось бы руками на боевой
// базе.
func run(ctx context.Context, old, fresh *dbgate.Gate, slug string,
	profile importer.Profile, adopt, apply bool) (importer.Report, error) {

	var out importer.Report
	err := fresh.InTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = importer.Import(ctx, old, tx, slug, profile, adopt, time.Now())
		if err != nil {
			return err
		}
		if !apply {
			// Откат нарочный: показ обязан считать ровно то же, что
			// запишет накат.
			return errShow
		}
		return nil
	})
	if err != nil && err != errShow {
		return out, err
	}
	return out, nil
}
