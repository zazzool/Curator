// Куратор: сервер.
//
// Одна программа отдаёт всё — API приложения, редакционное API студии,
// статику студии и страницу раздачи сборки. Разделения на службы нет
// намеренно: связь между ними частая, а выигрыш от разделения появился бы
// только там, где части живут на разных машинах.
//
// Сейчас здесь подъём и проверка живости, и больше ничего: остальное
// приезжает по этапам сквозного пути (docs/plan.md). Пустой, но
// поднимающийся сервер лучше заготовки на будущее — по нему сразу видно,
// доехала ли схема и отвечает ли база.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"curator/server/internal/dbgate"
	"curator/server/internal/envfile"
	"curator/server/internal/source"
	"curator/server/internal/studio"
)

func main() {
	if err := envfile.Load(".env"); err != nil {
		log.Fatalf("настройки из .env не прочитаны: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var gate *dbgate.Gate
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		g, err := dbgate.Open(ctx, dsn, slowThreshold())
		if err != nil {
			// Отказ, а не работа без базы: поднявшийся сервер без базы
			// выглядит исправным и отвечает пустотой, а пустоту работа
			// принимает за правду.
			log.Fatalf("база: %v", err)
		}
		gate = g
		defer gate.Close()
		log.Print("база подключена")
	} else {
		log.Print("DATABASE_URL не задан: поднимаемся без базы, данных не будет")
	}

	srv := &http.Server{
		Addr:              addr(),
		Handler:           routes(gate),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("слушаем %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("сервер остановлен: %v", err)
		}
	}()

	<-ctx.Done()

	// Остановка с запасом: обрыв обращения на полуслове выглядит у клиента
	// как отказ службы, а не как выкатка.
	shutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Printf("остановка не доведена: %v", err)
	}
	log.Print("остановлены")
}

// routes собирает маршруты.
//
// Шлюз передаётся доводом, а не берётся из глобальной переменной: проверке
// нужно поднять маршруты без базы, а глобальная переменная означала бы, что
// сделать это можно только в одном экземпляре на весь прогон.
func routes(gate *dbgate.Gate) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		// Живость отвечает и без базы: её спрашивает docker, и ответ
		// «жив» про процесс, а не про хранилище. Готовность базы — другой
		// вопрос и другая ручка, она появится вместе с тем, что от базы
		// зависит.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if gate == nil {
			_, _ = w.Write([]byte("жив, без базы\n"))
			return
		}
		_, _ = w.Write([]byte("жив\n"))
	})

	// Редакционное API. Без базы его нет вовсе, и это честнее заглушки:
	// поднятые ручки, отвечающие пустотой, работа примет за правду и
	// запишет пустоту как результат.
	if gate != nil {
		desk := studio.NewDesk(studio.NewUsers(gate), studio.NewSessions(gate))
		studio.Routes(desk)
		source.Routes(desk, source.NewStore(gate))
		mux.Handle("/admin/api/", desk.Handler())
	}

	// Статика студии. Пусто — раздача выключена, и это нормальный режим
	// разработки: студия идёт своим сервером Vite.
	if dir := os.Getenv("CURATOR_EDITOR_DIR"); dir != "" {
		mux.Handle("/", http.FileServer(http.Dir(dir)))
	}
	return mux
}

// addr — что слушать.
func addr() string {
	if v := os.Getenv("CURATOR_ADDR"); v != "" {
		return v
	}
	return ":8080"
}

// slowThreshold — с какой длительности запрос попадает в журнал.
//
// Ноль выключает журнал. Умолчание выбрано так, чтобы в него не попадали
// обычные запросы: журнал, куда пишется всё, не читают вовсе.
func slowThreshold() time.Duration {
	raw := os.Getenv("CURATOR_SLOW_MS")
	if raw == "" {
		return 500 * time.Millisecond
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 0 {
		log.Printf("CURATOR_SLOW_MS = %q: не число, берём умолчание", raw)
		return 500 * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}
