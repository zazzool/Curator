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

	"curator/server/internal/app"
	"curator/server/internal/casestore"
	"curator/server/internal/dbgate"
	"curator/server/internal/envfile"
	"curator/server/internal/gen"
	"curator/server/internal/llm"
	"curator/server/internal/llmusage"
	"curator/server/internal/progress"
	"curator/server/internal/signs"
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
		Handler:           routes(ctx, gate),
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
func routes(ctx context.Context, gate *dbgate.Gate) http.Handler {
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

	// Дверь приложения. За ней стоит не сотрудник, а установленная
	// сборка, которую никто не обновит по нашей просьбе, — потому она и
	// отдельная: правила у неё другие, и смешивать их со студией значит
	// однажды поменять формат ради удобства студии.
	// Ключи программ нужны обеим дверям: /v1 их сверяет, студия заводит.
	// Объявлены снаружи обоих блоков и заполняются только при базе —
	// шлюз, отданный в хранилище пустым, отвечал бы не отказом, а паникой
	// на первом же обращении.
	var keys *app.Keys
	if gate != nil {
		// Выпуски знаков заводятся при старте по каталогу: каталог
		// остаётся единственным местом, где знак объявлен, а строки в базе
		// — следом выдачи, а не вторым объявлением. Отказ здесь роняет
		// старт намеренно: единственная его причина — тираж, уменьшенный
		// ниже уже выданного, а выданный знак не отбирают.
		if err := signs.Ensure(ctx, gate); err != nil {
			log.Fatalf("выпуски знаков не заведены: %v", err)
		}

		keys = app.NewKeys(gate)
		door := app.NewDoor(keys, app.NewAccounts(gate))
		app.Routes(door, app.NewFeed(gate), app.NewAttempts(gate, progress.Default()))
		mux.Handle("/v1/", door.Handler())
	}

	// Редакционное API. Без базы его нет вовсе, и это честнее заглушки:
	// поднятые ручки, отвечающие пустотой, работа примет за правду и
	// запишет пустоту как результат.
	if gate != nil {
		desk := studio.NewDesk(studio.NewUsers(gate), studio.NewSessions(gate))
		studio.Routes(desk)
		source.Routes(desk, source.NewStore(gate))
		casestore.Routes(desk, casestore.NewStore(gate))
		app.KeyRoutes(desk, keys)
		generation(ctx, gate, desk)
		mux.Handle("/admin/api/", desk.Handler())
	}

	// Статика студии. Пусто — раздача выключена, и это нормальный режим
	// разработки: студия идёт своим сервером Vite.
	if dir := os.Getenv("CURATOR_EDITOR_DIR"); dir != "" {
		mux.Handle("/", http.FileServer(http.Dir(dir)))
	}
	return mux
}

// generation поднимает второй этап пути: ручки заказа и исполнителя очереди.
//
// Ручки объявляются всегда, а исполнитель заводится только при настроенном
// поставщике. Так разделено намеренно: студия без ключа должна показывать
// очередь и задания (они уже есть в базе от прежних прогонов) и говорить,
// почему новое не пишется, а не прятать раздел. Спрятанный раздел человек
// принимает за поломку студии и идёт искать её в студии.
func generation(ctx context.Context, gate *dbgate.Gate, desk *studio.Desk) {
	jobs := gen.NewJobs(gate)
	prompts := gen.NewPrompts(gate)
	gen.Routes(desk, jobs, gen.NewResolver(gate), prompts)

	// Затравки кладутся при подъёме и только недостающие: правленое в
	// студии задание затирать накатом нельзя. Отказ здесь не валит
	// сервер — без заданий не пишутся задачи, но всё остальное работает,
	// и отказ подъёма отнял бы и его.
	seedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := prompts.Seed(seedCtx); err != nil {
		log.Printf("затравки заданий не положены: %v", err)
	}

	providers := llmProviders()
	if len(providers) == 0 {
		log.Print("OPENROUTER_API_KEY не задан: очередь генерации разбирать некому")
		return
	}

	ledger := llmusage.NewStore(gate)
	prices, err := ledger.Prices(seedCtx)
	if err != nil {
		// Прайс — про оценку цены, а не про работу: без него обращения
		// запишутся без суммы и будут помечены как неоценённые. Это
		// честнее, чем не писать их вовсе.
		log.Printf("прайс моделей не прочитан, расход будет без оценки: %v", err)
		prices = llm.Prices{}
	}

	chain := llm.NewChain(func() []llm.ProviderConfig { return providers }, publicOrigin(), "Куратор")

	// Chain.Record здесь намеренно не назначается: учёт ведёт исполнитель
	// (Runner.account), и он один знает, к какому заданию и узлу относится
	// обращение, — а единица разбора именно задание. Плата за это названа
	// вслух: неудавшаяся попытка перебора своей строки пока не получает,
	// и сумма за месяц окажется меньше счёта поставщика ровно на попытки,
	// после которых отвечал сосед. Чинится это тем, что задание и узел
	// поедут к цепочке в контексте обращения, и тогда записывать будет
	// цепочка — одна, всё, что состоялось. Это отдельная работа.
	runner := gen.NewRunner(jobs, prompts, chain).WithLedger(ledger, prices)
	go gen.Work(ctx, runner)
	log.Printf("генерация: исполнитель очереди поднят, поставщиков %d", len(providers))
}

// llmProviders — список поставщиков из окружения.
//
// Пока один и из переменных, а не из базы: настройка поставщиков в студии
// приедет вместе с разделом «Модели». Функция заведена сразу списком,
// чтобы этот переезд не менял ничего, кроме её тела, — перебор соседей уже
// написан и работает.
func llmProviders() []llm.ProviderConfig {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil
	}
	base := os.Getenv("OPENROUTER_BASE_URL")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = "anthropic/claude-sonnet-4.6"
	}
	return []llm.ProviderConfig{{
		ID: "openrouter", Name: "OpenRouter", Kind: llm.KindOpenAI,
		BaseURL: base, Model: model, Enabled: true, APIKey: key,
		// Канал только к поставщику моделей: прокси всему серверу увёл бы
		// туда же обращения к базе и почте.
		ProxyURL: os.Getenv("CURATOR_LLM_PROXY"),
	}}
}

// publicOrigin — адрес контура.
//
// Одно место на весь проект. Адрес, вписанный литералом во второй файл,
// расходится молча, и узнают об этом по чужому адресу в чужом журнале —
// у поставщика моделей он уезжает заголовком атрибуции.
func publicOrigin() string {
	if v := os.Getenv("CURATOR_PUBLIC_URL"); v != "" {
		return v
	}
	return "https://curator.psync.ru"
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
