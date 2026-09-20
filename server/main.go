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
	"crypto/ed25519"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"curator/server/internal/analytics"
	"curator/server/internal/app"
	"curator/server/internal/backup"
	"curator/server/internal/casestore"
	"curator/server/internal/dbgate"
	"curator/server/internal/envfile"
	"curator/server/internal/gen"
	"curator/server/internal/limits"
	"curator/server/internal/llm"
	"curator/server/internal/llmusage"
	"curator/server/internal/mail"
	"curator/server/internal/packs"
	"curator/server/internal/progress"
	"curator/server/internal/sales"
	"curator/server/internal/signs"
	"curator/server/internal/source"
	"curator/server/internal/studio"
	"curator/server/internal/telemetry"
)

func main() {
	if err := envfile.Load(".env"); err != nil {
		log.Fatalf("настройки из .env не прочитаны: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var gate *dbgate.Gate
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		g, err := dbgate.Open(ctx, dsn, dbgate.Options{
			Slow:    slowThreshold(),
			Timeout: queryTimeout(),
		})
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
		Addr:    addr(),
		Handler: routes(ctx, gate),

		// Сроки названы все четыре, и это не полнота ради полноты.
		// Стоял один ReadHeaderTimeout: соединение, приславшее заголовки
		// и замолчавшее на теле, держалось вечно, и десяток таких
		// занимает исполнителей без единого запроса. Так работает
		// медленная запись — приём, для которого не нужно ничего, кроме
		// открытого сокета.
		ReadHeaderTimeout: 10 * time.Second,

		// Тело целиком. Минута, а не десять секунд: сюда приносят
		// документ источника в несколько мегабайт, и узкая связь у
		// составителя — обычное дело.
		ReadTimeout: 60 * time.Second,

		// Ответ целиком. Самый долгий ответ здесь — страница задач или
		// выпуск набора; полторы минуты покрывают их с запасом. Обращения
		// к моделям идут НЕ отсюда: их ждёт фоновый исполнитель очереди,
		// а не соединение с браузером.
		WriteTimeout: 90 * time.Second,

		// Простаивающее соединение после keep-alive. Держать его дольше
		// значит платить исполнителем за тишину.
		IdleTimeout: 120 * time.Second,

		// Заголовки. Умолчание — мегабайт, и мегабайт заголовков не
		// присылает никто, кроме того, кто занимает память нарочно.
		MaxHeaderBytes: 64 << 10,
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
		// Живость отвечает и без базы: её спрашивает тот, кто решает,
		// перезапускать ли процесс, и ответ «жив» про процесс, а не про
		// хранилище. Перезапуск процесса отпавшую базу не вернёт.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if gate == nil {
			_, _ = w.Write([]byte("жив, без базы\n"))
			return
		}
		_, _ = w.Write([]byte("жив\n"))
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		// Готовность — это «могу работать», и от базы здесь зависит всё.
		// Раньше её не спрашивал никто, и HEALTHCHECK смотрел на живость:
		// база, отпавшая после подъёма, оставляла контейнер «здоровым»,
		// прокси слал в него обращения, каждое отвечало пятисотым, и
		// docker был доволен. Тихий отказ стоит дороже громкого.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if gate == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("не готов: базы нет\n"))
			return
		}
		// Срок короткий и свой: у двери он общий на все запросы и для
		// опроса готовности велик. Опрос, ждущий полминуты, узнаёт об
		// отказе позже прокси, который к тому времени уже отдал ответ
		// врачу.
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := gate.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			// Отказ базы пишется в журнал, а не в ответ: ответ читает
			// docker, а строку подключения из отказа pgx — всякий, кому
			// ручка открыта.
			log.Printf("/readyz: база не отвечает: %v", err)
			_, _ = w.Write([]byte("не готов: база не отвечает\n"))
			return
		}
		_, _ = w.Write([]byte("готов\n"))
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
	var packStore *packs.Store
	var access *sales.Access
	var prices *sales.Prices
	if gate != nil {
		// Выпуски знаков заводятся при старте по каталогу: каталог
		// остаётся единственным местом, где знак объявлен, а строки в базе
		// — следом выдачи, а не вторым объявлением. Отказ здесь роняет
		// старт намеренно: единственная его причина — тираж, уменьшенный
		// ниже уже выданного, а выданный знак не отбирают.
		if err := signs.Ensure(ctx, gate); err != nil {
			log.Fatalf("выпуски знаков не заведены: %v", err)
		}

		// Ключ подписи наборов приходит из окружения и в базу не попадает
		// никогда: утёкшая база не должна давать права подписывать. Без
		// ключа служба поднимается и раздаёт уже выпущенное — отказывает
		// только выпуск нового, и отказывает словами, а не молчанием.
		var signing ed25519.PrivateKey
		if raw := os.Getenv("PACK_SIGNING_KEY"); raw != "" {
			parsed, err := packs.ParsePrivateKey(raw)
			if err != nil {
				log.Fatalf("ключ подписи наборов негоден: %v", err)
			}
			signing = parsed
		} else {
			log.Print("ключ подписи наборов не задан: выпускать наборы нечем")
		}
		packStore = packs.NewStore(gate, signing, os.Getenv("PACK_SIGNING_KEY_ID"))
		access = sales.NewAccess(gate)
		prices = sales.NewPrices(gate)

		keys = app.NewKeys(gate)
		accounts := app.NewAccounts(gate)
		door := app.NewDoor(keys, accounts)
		app.Routes(door, app.NewFeed(gate), app.NewAttempts(gate, progress.Default()), access)
		app.ReferenceRoutes(door, app.NewReference(gate))

		// Почта. Настройки может не быть вовсе — служба обязана
		// подниматься и раздавать задачи там, где почты нет; отказывают
		// тогда только сами ручки почты, и отказывают словами.
		post := mail.FromEnv()
		if !post.Ready() {
			log.Print("почта не настроена: привязать её и вернуть доступ врач не сможет")
		}
		app.EmailRoutes(door, app.NewEmails(accounts), post, time.Now)
		app.PackRoutes(door, packStore, access, prices)
		app.TelemetryRoutes(door, telemetry.NewStore(gate))

		// Потолок на тело обращения приложения. Самое крупное здесь —
		// посылка телеметрии: до 500 событий, и потолок взят с запасом
		// на них. Без потолка разбор тела шёл до конца, а потолок в 500
		// разборов сверялся ПОСЛЕ разбора — то есть память была уже
		// занята, а контейнеру отведено 512 мегабайт.
		mux.Handle("/v1/", limits.Body(1<<20, door.Handler()))
	}

	// Редакционное API. Без базы его нет вовсе, и это честнее заглушки:
	// поднятые ручки, отвечающие пустотой, работа примет за правду и
	// запишет пустоту как результат.
	if gate != nil {
		// Ключ запечатывания секретов аутентификатора. Довод — в
		// studio/secret.go; здесь важно, что негодный ключ роняет службу,
		// а отсутствующий только объявляется: выкатка на контур, где ключ
		// ещё не положили, не должна закрывать вход в студию всем сразу.
		seal, err := studio.SealKeyFromEnv()
		if err != nil {
			log.Fatalf("ключ запечатывания секретов негоден: %v", err)
		}
		if len(seal) == 0 {
			log.Print("CURATOR_SECRET_KEY не задан: секреты аутентификаторов лежат в базе открытым текстом")
		}
		sessions := studio.NewSessions(gate)
		desk := studio.NewDesk(studio.NewUsers(gate, seal), sessions)
		studio.Routes(desk)
		studio.UserRoutes(desk)
		source.Routes(desk, source.NewStore(gate))
		casestore.Routes(desk, casestore.NewStore(gate))
		app.KeyRoutes(desk, keys)
		packs.Routes(desk, packStore)
		sales.Routes(desk, sales.NewPayments(gate), prices, access, sales.NewClients(gate))

		rollup := analytics.NewRollup(gate)
		analytics.Routes(desk, rollup)
		go sweep(ctx, rollup)
		go housekeeping(ctx, sessions, llmusage.NewStore(gate))

		// Снимки базы. Задачи, разметка и разборы существуют в одном
		// экземпляре: разбор источника и генерация стоят денег и времени
		// составителя, а восстановить их из ничего нельзя — модель
		// напишет другое. Ненастроенные снимки говорят об этом вслух при
		// старте: пустой каталог выглядит одинаково при «ещё не сняли» и
		// при «не снимаем никогда».
		(&backup.Keeper{
			DSN:       os.Getenv("DATABASE_URL"),
			Dir:       os.Getenv("CURATOR_BACKUP_DIR"),
			VerifyDSN: os.Getenv("CURATOR_BACKUP_VERIFY_DSN"),
			Keep:      positive(os.Getenv("CURATOR_BACKUP_KEEP")),
			Offsite:   offsite(),
		}).Every(ctx, 24*time.Hour)

		generation(ctx, gate, desk)

		// Потолок студии крупнее: сюда приносят документ источника, и
		// приказ на сотню страниц в DOCX весит мегабайты. Он всё равно
		// потолок: без него тело не ограничено ничем, а студия открыта
		// тому, у кого есть учётная запись, — то есть отказ здесь стоит
		// не меньше, чем отказ от постороннего.
		mux.Handle("/admin/api/", limits.Body(32<<20, desk.Handler()))
	}

	// Статика студии. Пусто — раздача выключена, и это нормальный режим
	// разработки: студия идёт своим сервером Vite.
	if dir := os.Getenv("CURATOR_EDITOR_DIR"); dir != "" {
		mux.Handle("/", studioFiles(dir))
	}

	// Заголовки безопасности — снаружи всего, включая живость и
	// готовность: обёртка, надетая на часть маршрутов, забывается ровно
	// на том, который заведут следующим.
	return limits.Headers(mux)
}

// sweep сводит решаемость задач в фоне.
//
// Не по обращению из студии: отчёт, считающий решаемость в момент показа,
// перестаёт открываться ровно тогда, когда данных становится достаточно,
// чтобы он был интересен. И не при каждой попытке: врач досылает пачку из
// метро, и сведение в его транзакции — это его ожидание ради нашего отчёта.
//
// Отказ не валит службу и не останавливает часы: сведение — это отчёт, а не
// работа врача. Упавший проход повторится следующим, и данные для него
// никуда не делись — они в попытках.
func sweep(ctx context.Context, rollup *analytics.Rollup) {
	const every = 5 * time.Minute
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			touched, err := rollup.Run(runCtx, time.Now())
			cancel()
			if err != nil {
				log.Printf("решаемость не сведена: %v", err)
				continue
			}
			if touched > 0 {
				log.Printf("решаемость сведена: задач %d", touched)
			}
		}
	}
}

// housekeeping убирает то, что копится само: истёкшие сессии студии и
// тела обращений к моделям.
//
// Оба уборщика были написаны и объяснены, и оба не звались ниоткуда,
// кроме проверок. Нашёл это аудит, а не отказ, — и не мог бы найти иначе:
// не позванный уборщик не роняет ничего, он просто копит. Таблица сессий
// превращалась в журнал входов, которого никто не заводил, а в llm_calls
// вечно лежали полные тела запросов и ответов модели.
//
// # Почему отдельная петля, а не общая со сведением решаемости
//
// Та идёт раз в пять минут, потому что отчёт должен быть свежим. Убирать
// с той же частотой — это двести восемьдесят восемь проходов в сутки
// ради работы, которой хватает одного, и каждый из них перебирает те же
// строки. Раз в сутки здесь не осторожность, а верный срок.
//
// # Почему проход делается сразу, а не через сутки
//
// Тикер срабатывает через свой промежуток, и служба, которую
// перезапускают чаще раза в сутки — а выкатка это и делает, — не убрала
// бы ни разу. Беда та же, что и у самих уборщиков: ничего не падает,
// просто не убирается.
//
// Отказ не валит службу: уборка — не работа врача. Не убранное этим
// проходом уберёт следующий.
func housekeeping(ctx context.Context, sessions *studio.Sessions, ledger *llmusage.Store) {
	const every = 24 * time.Hour

	// Месяц: журнал нужен, пока сбой разбирают, а не вечно. Числа учёта
	// при этом остаются навсегда — сводка расходов за всё время обязана
	// оставаться правдой, и убираются именно тела.
	const keepBodies = 30 * 24 * time.Hour

	run := func() {
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		now := time.Now()
		if n, err := sessions.Sweep(runCtx, now); err != nil {
			log.Printf("истёкшие сессии не убраны: %v", err)
		} else if n > 0 {
			log.Printf("истёкших сессий убрано: %d", n)
		}
		if n, err := ledger.Sweep(runCtx, keepBodies, now); err != nil {
			log.Printf("старые тела обращений не убраны: %v", err)
		} else if n > 0 {
			log.Printf("тел обращений к моделям убрано: %d", n)
		}
	}

	run()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			run()
		}
	}
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
// queryTimeout — срок на один запрос к базе.
//
// Тридцать секунд: столько не идёт ни один запрос, который ждёт человек,
// и столько не жалко подождать самому тяжёлому отчёту. Ноль снимает срок
// совсем — на случай, когда запрос идёт дольше и это известно заранее.
func queryTimeout() time.Duration {
	raw := os.Getenv("CURATOR_QUERY_TIMEOUT_MS")
	if raw == "" {
		return 30 * time.Second
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 0 {
		log.Printf("CURATOR_QUERY_TIMEOUT_MS=%q не понято, беру умолчание", raw)
		return 30 * time.Second
	}
	return time.Duration(ms) * time.Millisecond
}

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

// positive разбирает число из окружения, не отказывая на пустом.
//
// Пустая переменная — это «не задано», а не ноль. В числе снимков ноль
// понимался бы как «стереть все», и защита стёрла бы себя от пустой
// строки в .env.
func positive(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// offsite — второе хранилище снимков, если оно настроено.
//
// Негодная настройка роняет подъём, а не молча отключает вывоз. Это
// исключение из общего правила «мусор в необязательной переменной не
// роняет подъём», и оно объявлено: переменные вывоза заполняет человек
// вручную и один раз, а замечает их отсутствие — в тот единственный день,
// когда снимок нужен. Незаполненные вовсе — не мусор, а «вывоза нет».
func offsite() backup.Offsite {
	out, err := backup.FromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("вывоз снимков: %v", err)
	}
	return out
}

// studioFiles отдаёт собранную студию, подставляя её страницу на пути,
// которых на диске нет.
//
// Без подстановки адреса студии существуют только внутри уже открытой
// вкладки: `/sources/12` набранный руками, присланный ссылкой или
// переоткрытый по F5 упирается в голый файловый сервер и получает 404 —
// то есть «такого источника нет» вместо источника. Адрес, который нельзя
// послать, адресом не является.
//
// Настоящий файл и подставленная страница разведены намеренно, и порядок
// здесь несущий: сначала решается, что именно отдаётся, и лишь потом
// ставится заголовок хранения. Поставь его по одному виду пути — и
// запрос `/assets/чего-нибудь.js`, которого на диске нет, получил бы
// страницу студии с пометкой «хранить»; браузер запомнил бы разметку под
// именем скрипта, и почистить это у составителя нечем.
func studioFiles(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Существующий файл отдаётся как прежде, со всеми заголовками
		// файлового сервера: подстановка ничего не меняет в раздаче
		// сборки, она добавляет ответ там, где его не было.
		if r.URL.Path != "/" {
			name := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
			if info, err := os.Stat(name); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}

		// no-cache — не «не хранить», а «хранить, но каждый раз
		// переспрашивать». Страница называет имена файлов сборки, и
		// запомненная хоть на минуту она после выкатки послала бы
		// браузер за тем, чего уже нет.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}
