package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Лента задач: то, что приложение качает и показывает.
//
// Ленте видно только опубликованное. Черновик, задача на выверке и снятая
// с раздачи для приложения не существуют — и это условие стоит в запросе,
// а не в проверке после чтения: отбор после чтения однажды забудут, и
// узнают об этом по черновику на экране у врача.

// Feed — лента.
type Feed struct {
	gate *dbgate.Gate
}

func NewFeed(gate *dbgate.Gate) *Feed { return &Feed{gate: gate} }

// Case — задача, какой её видит приложение.
//
// Поля названы поимённо, а не отданы картой: проводной формат меняется
// только добавлением необязательных полей, и уследить за этим можно лишь
// тогда, когда поля перечислены в одном месте.
type Case struct {
	ID        string
	SourceID  int64
	UnitLabel string
	UnitPath  string
	Body      json.RawMessage
	Version   int64
}

// Cursor — откуда продолжать.
type Cursor struct {
	After string
	Path  string
	Limit int
}

// Page — страница ленты.
type Page struct {
	Cases []Case

	// Next — курсор следующей страницы. Пусто — страниц больше нет.
	Next string

	// Version — версия содержания на момент выдачи страницы. Уезжает с
	// каждой страницей, чтобы приложение заметило выпуск, случившийся
	// посреди обхода, и обошло заново.
	Version int64
}

// Page отдаёт страницу ленты.
//
// Порядок по номеру задачи, а не по времени выпуска: время у двух задач,
// выпущенных одной транзакцией, совпадает, и курсор по нему зациклился бы
// на них навсегда. Номер уникален, и это делает страницы устойчивыми.
func (f *Feed) Page(ctx context.Context, c Cursor) (Page, error) {
	limit := c.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	version, err := f.Version(ctx)
	if err != nil {
		return Page{}, err
	}

	rows, err := f.gate.Query(ctx,
		`SELECT id, source_id, unit_label, unit_path, body
		   FROM cases
		  WHERE status = 'published'
		    AND ($1 = '' OR id > $1)
		    AND ($2 = '' OR unit_path = $2 OR unit_path LIKE $3 ESCAPE '\')
		  ORDER BY id
		  LIMIT $4`,
		c.After, c.Path, escapeLike(c.Path)+`/%`, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("лента не прочитана: %w", err)
	}
	defer rows.Close()

	// Пустой список — [], а не nil: приложение ходит по нему циклом, и на
	// исправном случае — на дочитанной до конца ленте — null уронил бы его.
	out := Page{Cases: []Case{}, Version: version}
	for rows.Next() {
		var one Case
		if err := rows.Scan(&one.ID, &one.SourceID, &one.UnitLabel, &one.UnitPath, &one.Body); err != nil {
			return Page{}, fmt.Errorf("задача ленты не прочитана: %w", err)
		}
		one.Version = version
		out.Cases = append(out.Cases, one)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("лента не дочитана: %w", err)
	}

	// Лишняя задача запрошена нарочно: она отвечает на вопрос «есть ли
	// ещё», не требуя второго запроса со счётом. Счёт по растущей таблице
	// дорожает со временем, а этот приём — нет.
	if len(out.Cases) > limit {
		out.Cases = out.Cases[:limit]
		out.Next = out.Cases[limit-1].ID
	}
	return out, nil
}

// Case отдаёт одну опубликованную задачу.
func (f *Feed) Case(ctx context.Context, id string) (Case, error) {
	version, err := f.Version(ctx)
	if err != nil {
		return Case{}, err
	}
	var one Case
	err = f.gate.QueryRow(ctx,
		`SELECT id, source_id, unit_label, unit_path, body
		   FROM cases WHERE id = $1 AND status = 'published'`, id).
		Scan(&one.ID, &one.SourceID, &one.UnitLabel, &one.UnitPath, &one.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return Case{}, errors.New("такой задачи нет")
	}
	if err != nil {
		return Case{}, fmt.Errorf("задача не прочитана: %w", err)
	}
	one.Version = version
	return one, nil
}

// Version — версия опубликованного содержания.
func (f *Feed) Version(ctx context.Context) (int64, error) {
	var version int64
	if err := f.gate.QueryRow(ctx, `SELECT version FROM content_version WHERE id`).Scan(&version); err != nil {
		return 0, fmt.Errorf("версия содержания не прочитана: %w", err)
	}
	return version, nil
}

// escapeLike обезвреживает знаки, значимые для LIKE.
//
// В метке приказа подчёркивание и процент вполне возможны, а для LIKE это
// «любой знак» и «любая строка». Без экранирования срез по пути «п_1»
// захватил бы «п-1» и «п.1» — то есть отдал бы чужие задачи, и молча.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// pageJSON — страница ленты на проводе.
func pageJSON(p Page) map[string]any {
	cases := make([]map[string]any, 0, len(p.Cases))
	for _, one := range p.Cases {
		cases = append(cases, caseJSON(one))
	}
	return map[string]any{
		"cases":   cases,
		"next":    p.Next,
		"version": p.Version,
	}
}

// caseJSON — задача на проводе.
//
// Содержание уезжает как есть, тем же JSON, каким лежит в базе: разбирать
// его здесь, чтобы тут же собрать обратно, значит завести второе место, где
// живёт формат задачи, — и разойдутся эти два места молча.
func caseJSON(c Case) map[string]any {
	return map[string]any{
		"id":        c.ID,
		"sourceId":  c.SourceID,
		"unitLabel": c.UnitLabel,
		"unitPath":  c.UnitPath,
		"body":      json.RawMessage(c.Body),
		"version":   c.Version,
	}
}
