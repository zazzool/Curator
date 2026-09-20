package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Версии компиляторов названы в двух местах, и это неизбежно: сторож
// читает версию Go из `server/go.mod`, а `FROM` в Dockerfile читать
// оттуда нечем. Два места для одного расходятся молча — поэтому здесь
// они сверяются вслух.
//
// Проверка эта не про базу и потому не зовётся TestPg: она читает файлы
// и идёт всегда, в том числе там, где базы нет.

func TestВерсияGoВОбразеСходитсяСgomod(t *testing.T) {
	// Прежде в Dockerfile было написано, что версия берётся из go.mod, —
	// и это было неверно про ту же самую строку, что стояла ниже. Собрать
	// образ не тем компилятором можно молча: сборка пройдёт, а разойтись
	// они успеют на следующем обновлении.
	gomod := читай(t, "go.mod")
	m := regexp.MustCompile(`(?m)^go (\d+)\.(\d+)`).FindStringSubmatch(gomod)
	if m == nil {
		t.Fatal("в go.mod не нашлось строки версии Go")
	}
	хочу := m[1] + "." + m[2]

	dockerfile := читай(t, "../Dockerfile")
	d := regexp.MustCompile(`FROM golang:(\d+\.\d+)`).FindStringSubmatch(dockerfile)
	if d == nil {
		t.Fatal("в Dockerfile не нашлось строки FROM golang:")
	}
	if d[1] != хочу {
		t.Errorf("образ собирается компилятором %s, а go.mod требует %s — "+
			"сборка пройдёт, и узнают об этом не сегодня", d[1], хочу)
	}
}

func TestВерсияNodeВОбразеСходитсяСоСторожем(t *testing.T) {
	// У Node единственного места нет вовсе: её называют образ и сторож,
	// и больше назвать негде. Значит, сверяем их друг с другом.
	dockerfile := читай(t, "../Dockerfile")
	d := regexp.MustCompile(`FROM node:(\d+)`).FindStringSubmatch(dockerfile)
	if d == nil {
		t.Fatal("в Dockerfile не нашлось строки FROM node:")
	}

	workflow := читай(t, "../.github/workflows/checks.yml")
	w := regexp.MustCompile(`node-version:\s*'?(\d+)`).FindStringSubmatch(workflow)
	if w == nil {
		t.Fatal("у сторожа не нашлось строки node-version")
	}
	if d[1] != w[1] {
		t.Errorf("студию собирает Node %s, а проверяет Node %s: "+
			"проверенное и выкаченное собраны разным", d[1], w[1])
	}
}

func читай(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		// Отказ, а не пропуск: проверка, тихо пропустившая недоступный
		// файл, не сверяет ничего и выглядит при этом зелёной.
		t.Fatalf("%s не прочитан: %v", path, err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

func TestКаталогСнимковВОбразцеСходитсяСТомом(t *testing.T) {
	// Путь назван дважды и по-разному: том монтирует docker-compose, а
	// читает его служба из .env. Разойдись они — служба напишет снимок
	// внутрь контейнера, том останется пуст, и весь смысл снимка (пережить
	// контейнер, который его написал) потеряется молча. Так и было: в
	// образце стоял /data/backups, а том монтируется в /app/backups.
	compose := читай(t, "../docker-compose.yml")
	c := regexp.MustCompile(`\./backups:(\S+)`).FindStringSubmatch(compose)
	if c == nil {
		t.Fatal("в docker-compose.yml не нашлось тома снимков")
	}

	env := читай(t, ".env.example")
	e := regexp.MustCompile(`(?m)^CURATOR_BACKUP_DIR=(\S+)`).FindStringSubmatch(env)
	if e == nil {
		t.Fatal("в .env.example не нашлось CURATOR_BACKUP_DIR")
	}
	if e[1] != c[1] {
		t.Errorf("служба пишет снимки в %s, а том смонтирован в %s — "+
			"снимки останутся в контейнере и умрут вместе с ним", e[1], c[1])
	}
}

func TestКорнюКонтейнераЗапрещеноПисать(t *testing.T) {
	compose := читай(t, "../docker-compose.yml")
	// Ищем настройки службы curator, а не всего файла: у сайдкара с
	// туннелем корень пишущий по устройству (он правит iptables), и
	// совпадение из его куска выдало бы зелёное за непроверенное.
	service := compose[strings.Index(compose, "  curator:"):]
	if i := strings.Index(service, "\n  llm-vpn:"); i > 0 {
		service = service[:i]
	}

	if !strings.Contains(service, "read_only: true") {
		t.Error("корень контейнера пишущий: подложенный туда чужой код переживёт перезапуск")
	}
	// Читающий корень без временного каталога — это отказ загрузки
	// документа и несделанный снимок, и выясняется он не при старте, а на
	// первом документе.
	if !strings.Contains(service, "/tmp:size=") {
		t.Error("при читающем корне не отведён /tmp: многочастная форма и pg_dump писать некуда")
	}
	if !regexp.MustCompile(`pids_limit:\s*\d+`).MatchString(service) {
		t.Error("нет потолка нитей: их исчерпание переживает контейнер, но не хозяин")
	}
}

func TestОбразНесётpg_dump(t *testing.T) {
	// Снимок снимает pg_dump, а не свой обход таблиц. Нет его в образе —
	// снимков не будет, и выяснится это не сегодня: служба поднимется,
	// студия заработает, а в журнале раз в сутки будет строка, которую
	// никто не читает.
	//
	// Версия названа поимённо: pg_dump старше базы отказывается с ней
	// разговаривать.
	dockerfile := читай(t, "../Dockerfile")
	if !strings.Contains(dockerfile, "postgresql16-client") {
		t.Error("в образе нет postgresql16-client: снимать базу нечем")
	}
	if !strings.Contains(dockerfile, "/app/backup") {
		t.Error("в образе нет утилиты снимка: снять базу руками нечем")
	}
}
