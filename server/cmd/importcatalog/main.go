// Ввоз каталога из файла выгрузки.
//
// Отдельной утилитой, как и ввоз из прежней базы, и по той же причине:
// это делается один раз, файлом в несколько мегабайт, и ручка, делающая
// такое, однажды будет нажата второй раз — не тем человеком и не на той
// базе.
//
// # Файла в репозитории нет и не будет
//
// Путь к нему называет человек, флагом. Репозиторий открытый, а выгрузка
// несёт текст, права на который принадлежат не нам: положить его в
// открытый репозиторий — это раздать чужое, а не «прикрепить данные».
//
// # Без -apply ничего не пишется
//
// Умолчание показывает, что ввоз СОБИРАЕТСЯ сделать, и в базу не ходит
// вовсе. Разбор при этом делается настоящий и числа считаются настоящие:
// показ, считающий не то, что запишет накат, хуже отсутствия показа.
//
// Пример:
//
//	importcatalog -catalog icd10_catalog_v1.json -source icd10 \
//	              -title 'МКБ-10, психические расстройства' \
//	              -unit-word рубрика -statement-word критерий -apply
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/envfile"
	"curator/server/internal/source"
)

func main() {
	log.SetFlags(0)

	path := flag.String("catalog", "", "путь к файлу выгрузки каталога")
	slug := flag.String("source", "", "краткое имя источника")
	title := flag.String("title", "", "название источника")
	unitWord := flag.String("unit-word", "", "как звать единицу: рубрика, диагноз, пункт")
	stWord := flag.String("statement-word", "", "как звать положение: критерий, пункт, требование")
	kind := flag.String("kind", string(source.KindClassification),
		"вид: classification, decree, guidelines, standard, handbook, other")
	purpose := flag.String("purpose", string(source.PurposeTopic),
		"ось: topic, system, discipline, task, level, legal, other")
	hierarchy := flag.String("hierarchy", string(source.HierarchyIsA),
		"смысл вложенности: is-a, part-of, grouped")
	completeness := flag.String("completeness", string(source.CompletenessComplete),
		"полнота: complete или fragment")
	edition := flag.String("edition", "", "редакция источника")
	activate := flag.Bool("activate", false, "объявить источник действующим после приёмки")
	apply := flag.Bool("apply", false, "записывать; без него только разбор и числа")
	flag.Parse()

	envfile.Load(".env")

	if *path == "" {
		log.Fatal("не назван файл: -catalog")
	}
	if *slug == "" || *title == "" || *unitWord == "" || *stWord == "" {
		log.Fatal("не назван паспорт источника: нужны -source, -title, -unit-word и -statement-word")
	}

	raw, err := os.ReadFile(*path)
	if err != nil {
		log.Fatalf("файл не прочитан: %v", err)
	}

	// Разбор идёт до всякого соединения: ругаться на чужую версию схемы
	// после минуты ожидания базы значит потратить эту минуту зря.
	catalog, err := source.ParseCatalog(raw)
	if err != nil {
		log.Fatalf("%v", err)
	}
	// Пути считаются здесь же, хотя их посчитает и приёмка: оборванная
	// цепочка родителей роняет приёмку целиком, и узнать об этом до
	// записи дешевле, чем после.
	if _, err := source.BuildPaths(catalog.Units); err != nil {
		log.Fatalf("дерево не сходится: %v", err)
	}

	show(catalog)

	if !*apply {
		log.Println("\nпоказ: в базу ничего не записано (для записи нужен -apply)")
		return
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL не задан: писать некуда")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	gate, err := dbgate.Open(ctx, dsn, dbgate.Options{})
	if err != nil {
		log.Fatalf("база недоступна: %v", err)
	}
	defer gate.Close()

	store := source.NewStore(gate)
	// Заведённый источник берётся, а не заводится заново. Ввоз идёт в
	// несколько шагов, и оборвавшись на любом из них — на разборе, на
	// приёмке — он обязан доделываться повторным заходом. Отказ «имя
	// занято» оставил бы человека с наполовину заведённым источником и
	// без его номера.
	sourceID := int64(0)
	if existing, err := store.SourceBySlug(ctx, *slug); err == nil {
		sourceID = existing.ID
		fmt.Printf("источник %s уже заведён (номер %d), ввоз идёт в него\n", *slug, sourceID)
	} else {
		sourceID, err = store.CreateSource(ctx, source.Source{
			Slug:          *slug,
			Kind:          source.Kind(*kind),
			Title:         *title,
			UnitWord:      *unitWord,
			StatementWord: *stWord,
			Purpose:       source.Purpose(*purpose),
			Hierarchy:     source.Hierarchy(*hierarchy),
			Completeness:  source.Completeness(*completeness),
			Edition:       *edition,
		})
		if err != nil {
			log.Fatalf("%v", err)
		}
	}

	// Файл выгрузки ложится документом источника. Это не хранение ради
	// хранения: приёмка разбора привязана к документу, и справочник без
	// документа, из которого он получен, нельзя ни пересобрать, ни
	// сверить с первоисточником.
	sum := sha256.Sum256(raw)
	docID, err := store.SaveDocument(ctx, source.Document{
		SourceID:   sourceID,
		Filename:   filepath.Base(*path),
		MIME:       "application/json",
		SHA256:     hex.EncodeToString(sum[:]),
		Body:       raw,
		UploadedBy: "importcatalog",
	})
	if err != nil {
		log.Fatalf("%v", err)
	}
	if err := store.SaveDraft(ctx, docID, catalog.Units, catalog.Statements); err != nil {
		log.Fatalf("черновик разбора не сохранён: %v", err)
	}
	// Приёмка, а не запись напрямую: второго пути от разбора к источнику
	// быть не должно — он разойдётся с первым молча, и разойдётся именно
	// там, где считаются пути и рода.
	taken, err := store.AcceptDraft(ctx, sourceID, docID, "importcatalog")
	if err != nil {
		log.Fatalf("приёмка отказала: %v", err)
	}
	// Пары пишутся после приёмки: они ссылаются на метки единиц, и до
	// приёмки ссылаться им не на что.
	if err := store.SaveDifferentials(ctx, sourceID, catalog.Differentials); err != nil {
		log.Fatalf("%v", err)
	}
	fmt.Printf("\nисточник %s (номер %d), принято единиц: %d, пар «путают с»: %d\n",
		*slug, sourceID, taken, len(catalog.Differentials))

	if *activate {
		if err := store.SetStatus(ctx, sourceID, source.StatusActive); err != nil {
			log.Fatalf("источник не объявлен действующим: %v", err)
		}
		fmt.Println("источник объявлен действующим: справочник поедет на устройства")
	}
}

// show печатает числа разбора.
//
// Отброшенное называется поимённо и с причиной. Число без причин не
// говорит ничего: по причинам видно, чего не хватает выгрузке, а по числу
// — только что «часть не доехала».
func show(catalog source.Catalog) {
	report := catalog.Report
	fmt.Printf("разобрано:\n")
	fmt.Printf("  разделов:    %d\n", report.Groups)
	fmt.Printf("  записей:     %d\n", report.Entries)
	fmt.Printf("  критериев:   %d\n", report.Criteria)
	fmt.Printf("  отличий:     %d\n", report.Different)
	fmt.Printf("  пар «путают с»: %d\n", len(catalog.Differentials))
	fmt.Printf("  всего единиц %d, положений %d\n",
		len(catalog.Units), len(catalog.Statements))

	if len(report.Dropped) == 0 {
		return
	}
	fmt.Printf("  отброшено:   %d\n", len(report.Dropped))
	keys := make([]string, 0, len(report.Dropped))
	for key := range report.Dropped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("    %s: %s\n", key, report.Dropped[key])
	}
}
