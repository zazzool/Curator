// Снимок базы руками.
//
// # Зачем отдельная утилита, если служба снимает сама
//
// Снимок нужен ещё и перед тем, чего служба не предвидит: перед накатом,
// который страшно катить, перед ввозом, перед правкой данных руками.
// Ждать суточного срока в такую минуту никто не станет.
//
// # Почему в образе
//
// Там же и потому же, что накат схемы и заведение пользователя: база
// видна только из сети сервера, а pg_dump обязан быть той же версии, что
// и она. Утилита, которую в аварии надо сперва собрать, в аварии
// бесполезна.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"curator/server/internal/backup"
	"curator/server/internal/envfile"
)

func main() {
	verify := flag.Bool("verify", false,
		"развернуть снимок в отдельную базу и сверить с живой")
	prune := flag.Bool("prune", false, "убрать старые снимки")
	flag.Parse()

	if err := envfile.Load(".env"); err != nil {
		log.Fatalf("настройки из .env не прочитаны: %v", err)
	}

	keeper := &backup.Keeper{
		DSN:       os.Getenv("DATABASE_URL"),
		Dir:       os.Getenv("CURATOR_BACKUP_DIR"),
		VerifyDSN: os.Getenv("CURATOR_BACKUP_VERIFY_DSN"),
		Keep:      number(os.Getenv("CURATOR_BACKUP_KEEP")),
		Offsite:   offsite(),
	}
	if !keeper.Ready() {
		log.Fatal("снимки не настроены: нужны DATABASE_URL и CURATOR_BACKUP_DIR")
	}

	// Часа хватит и большой базе, а бессрочная съёмка в аварии висит до
	// утра, не сказав ни слова.
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	snap, err := keeper.Take(ctx)
	if err != nil {
		log.Fatalf("%v", err)
	}
	fmt.Printf("снимок: %s (%d байт)\n", snap.Path, snap.Bytes)

	if *verify {
		if err := keeper.Verify(ctx, snap.Path); err != nil {
			// Отказ именно здесь — самое ценное, что эта утилита умеет:
			// снимок есть, а верить ему нельзя.
			log.Fatalf("снимку верить нельзя: %v", err)
		}
		fmt.Println("снимок развёрнут в отдельную базу и сошёлся по строкам")
	} else {
		fmt.Println("снимок не сверялся: добавьте -verify и CURATOR_BACKUP_VERIFY_DSN")
	}

	if *prune {
		removed, err := keeper.Prune()
		if err != nil {
			log.Fatalf("%v", err)
		}
		fmt.Printf("старых снимков убрано: %d\n", removed)
	}
}

// number разбирает число, не отказывая на пустом.
//
// Пустая переменная — это «не задано», а не ноль: ноль в числе снимков
// понимался бы как «стереть все», и защита стёрла бы себя от пустой
// строки в .env.
func number(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// offsite — второе хранилище снимков, если оно настроено.
//
// Негодная настройка роняет разовую съёмку так же, как и службу: молча
// снять снимок и не вывезти его — значит оставить человека уверенным, что
// вторая копия есть.
func offsite() backup.Offsite {
	out, err := backup.FromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("вывоз снимков: %v", err)
	}
	return out
}
