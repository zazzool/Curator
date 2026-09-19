package packs

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Store — пакеты в базе.
type Store struct {
	gate *dbgate.Gate

	// key и keyID — чем подписываются новые выпуски. Закрытая половина
	// приходит из окружения и в базу не попадает никогда: утёкшая база не
	// должна давать права подписывать.
	key   ed25519.PrivateKey
	keyID string
}

func NewStore(gate *dbgate.Gate, key ed25519.PrivateKey, keyID string) *Store {
	return &Store{gate: gate, key: key, keyID: keyID}
}

// CanSign отвечает, есть ли чем подписывать.
//
// Спрашивается заранее, при объявлении маршрутов: ручка выпуска, поднятая
// без ключа, отказала бы уже после того, как составитель собрал пакет.
func (s *Store) CanSign() bool { return len(s.key) == ed25519.PrivateKeySize }

// Release — выпуск пакета, каким он уехал на устройства.
type Release struct {
	PackID    int64
	Slug      string
	Version   int64
	Manifest  Manifest
	Signature string
	KeyID     string
}

// Pack — пакет в витрине.
type Pack struct {
	Slug      string
	Title     string
	SummaryMd string
	Version   int64
	Cases     int
}

// Publish собирает новый выпуск пакета и подписывает его.
//
// Выпуск неизменяем, и версия всегда следующая: правка пакета — это новый
// выпуск, а не правка старого. На устройствах стоит именно тот состав,
// который подписан, и подменить его задним числом значит сделать подпись
// бессмысленной.
func (s *Store) Publish(ctx context.Context, slug string, now time.Time) (Release, error) {
	if !s.CanSign() {
		return Release{}, errors.New("ключ подписи пакетов не задан: выпускать нечем")
	}

	var out Release
	err := s.gate.InTx(ctx, func(tx pgx.Tx) error {
		var packID int64
		var title, status string
		err := tx.QueryRow(ctx,
			`SELECT id, title, status FROM packs WHERE slug = $1 FOR UPDATE`,
			slug).Scan(&packID, &title, &status)
		if err == pgx.ErrNoRows {
			return errors.New("такого пакета нет")
		}
		if err != nil {
			return err
		}
		if status == "retired" {
			return errors.New("пакет снят с продажи: выпускать его заново незачем")
		}

		items, err := published(ctx, tx, packID)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			// Отказ, а не пустой выпуск: подписанный пустой пакет
			// установится на устройство и покажет врачу пустой экран,
			// причём с нашей подписью.
			return errors.New("в пакете нет ни одной опубликованной задачи")
		}

		var version int64
		if err := tx.QueryRow(ctx,
			`SELECT coalesce(max(version), 0) + 1 FROM pack_releases WHERE pack_id = $1`,
			packID).Scan(&version); err != nil {
			return err
		}

		manifest := Manifest{
			Pack: slug, Version: version, Title: title,
			ReleasedAt: now.UTC().Truncate(time.Second), Cases: items,
		}
		signature, err := Sign(manifest, s.key)
		if err != nil {
			return err
		}

		// В базу манифест кладётся ради показа и разбора, а подписаны
		// байты, посчитанные заново: JSONB не хранит ни порядок ключей, ни
		// пробелы, и подпись по вынутому из базы не сошлась бы нигде.
		stored, err := json.Marshal(manifestJSON(manifest))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO pack_releases (pack_id, version, manifest, signature, key_id, released_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			packID, version, stored, signature, s.keyID, manifest.ReleasedAt); err != nil {
			return err
		}

		out = Release{PackID: packID, Slug: slug, Version: version,
			Manifest: manifest, Signature: signature, KeyID: s.keyID}
		return nil
	})
	if err != nil {
		return Release{}, err
	}
	return out, nil
}

// published собирает состав пакета из опубликованных задач.
//
// Только опубликованные: черновик, уехавший в пакет, — это задача, которую
// никто не принимал, показанная врачу с нашей подписью. Снятая с раздачи
// задача в новый выпуск тоже не попадает, а в старых остаётся: выпуск
// неизменяем.
func published(ctx context.Context, tx pgx.Tx, packID int64) ([]Item, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id, i.ord, c.body
		  FROM pack_items i JOIN cases c ON c.id = i.case_id
		 WHERE i.pack_id = $1 AND c.status = 'published'
		 ORDER BY i.ord, c.id`, packID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		var id string
		var ord int
		var body []byte
		if err := rows.Scan(&id, &ord, &body); err != nil {
			return nil, err
		}
		hash, err := HashBody(body)
		if err != nil {
			return nil, fmt.Errorf("задача %s: %w", id, err)
		}
		out = append(out, Item{ID: id, Ord: ord, Hash: hash})
	}
	return out, rows.Err()
}

// Latest отдаёт последний выпуск пакета.
func (s *Store) Latest(ctx context.Context, slug string) (Release, error) {
	var out Release
	var raw []byte
	err := s.gate.QueryRow(ctx, `
		SELECT p.id, r.version, r.manifest, r.signature, r.key_id
		  FROM pack_releases r JOIN packs p ON p.id = r.pack_id
		 WHERE p.slug = $1 AND p.status = 'published'
		 ORDER BY r.version DESC
		 LIMIT 1`, slug).Scan(&out.PackID, &out.Version, &raw, &out.Signature, &out.KeyID)
	if err != nil {
		return Release{}, err
	}
	out.Slug = slug

	m, err := parseManifest(raw)
	if err != nil {
		return Release{}, err
	}
	out.Manifest = m
	return out, nil
}

// Shelf — витрина: опубликованные пакеты, у которых есть выпуск.
//
// Без выпуска пакет не показывается: пакет, который нельзя скачать, в
// витрине это обещание, а не товар.
func (s *Store) Shelf(ctx context.Context) ([]Pack, error) {
	rows, err := s.gate.Query(ctx, `
		SELECT p.slug, p.title, p.summary_md, r.version,
		       jsonb_array_length(r.manifest -> 'cases')
		  FROM packs p
		  JOIN LATERAL (
		       SELECT version, manifest FROM pack_releases
		        WHERE pack_id = p.id ORDER BY version DESC LIMIT 1
		  ) r ON TRUE
		 WHERE p.status = 'published'
		 ORDER BY p.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Пустой список — [], а не null: витрина без пакетов это исправный
	// случай, и на свежей установке он единственный.
	out := []Pack{}
	for rows.Next() {
		var one Pack
		if err := rows.Scan(&one.Slug, &one.Title, &one.SummaryMd, &one.Version, &one.Cases); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

// Bodies отдаёт содержание задач выпуска, страницей.
//
// Страницей, а не целиком: пакет на тысячу задач весит мегабайты, и
// телефон в метро не дотянет одну большую закачку. Порядок — по номеру
// задачи, чтобы продолжить с места обрыва.
func (s *Store) Bodies(ctx context.Context, slug, after string, limit int) ([]CaseBody, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.gate.Query(ctx, `
		SELECT c.id, c.body
		  FROM pack_items i
		  JOIN packs p ON p.id = i.pack_id
		  JOIN cases c ON c.id = i.case_id
		 WHERE p.slug = $1 AND p.status = 'published' AND c.status = 'published'
		   AND ($2 = '' OR c.id > $2)
		 ORDER BY c.id
		 LIMIT $3`, slug, after, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	out := []CaseBody{}
	for rows.Next() {
		var one CaseBody
		if err := rows.Scan(&one.ID, &one.Body); err != nil {
			return nil, "", err
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	// Лишняя строка спрошена нарочно: она отвечает «есть ли ещё» без
	// второго запроса и без счёта всего пакета.
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

// CaseBody — задача, как она уезжает в пакет.
type CaseBody struct {
	ID   string
	Body []byte
}

func manifestJSON(m Manifest) map[string]any {
	cases := make([]any, 0, len(m.Cases))
	for _, one := range m.Cases {
		cases = append(cases, map[string]any{"id": one.ID, "ord": one.Ord, "hash": one.Hash})
	}
	return map[string]any{
		"pack": m.Pack, "version": m.Version, "title": m.Title,
		"releasedAt": m.ReleasedAt.UTC().Format(time.RFC3339), "cases": cases,
	}
}

func parseManifest(raw []byte) (Manifest, error) {
	var stored struct {
		Pack       string `json:"pack"`
		Version    int64  `json:"version"`
		Title      string `json:"title"`
		ReleasedAt string `json:"releasedAt"`
		Cases      []struct {
			ID   string `json:"id"`
			Ord  int    `json:"ord"`
			Hash string `json:"hash"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		// Непонятое не применяется: манифест, который мы не смогли
		// разобрать, не отдаётся наполовину — по нему не сверить подпись.
		return Manifest{}, fmt.Errorf("манифест выпуска не разобран: %w", err)
	}
	at, err := time.Parse(time.RFC3339, stored.ReleasedAt)
	if err != nil {
		return Manifest{}, fmt.Errorf("время выпуска не разобрано: %w", err)
	}
	out := Manifest{Pack: stored.Pack, Version: stored.Version,
		Title: stored.Title, ReleasedAt: at}
	for _, one := range stored.Cases {
		out.Cases = append(out.Cases, Item{ID: one.ID, Ord: one.Ord, Hash: one.Hash})
	}
	return out, nil
}

// Shown — набор глазами составителя.
type Shown struct {
	Slug    string
	Title   string
	Status  string
	Cases   int
	Version int64
}

// Create заводит набор.
//
// Метка проверяется здесь, а не только указателем базы. Указатель отвечает
// на всё одинаково — «нарушено ограничение», — и отказ «метка занята либо
// негодна» отправляет составителя гадать, что из двух. Сказать, что именно
// не так, стоит трёх строк и экономит ему вечер.
func (s *Store) Create(ctx context.Context, slug, title, summary string) error {
	if title == "" {
		return errors.New("у набора должно быть название")
	}
	if !goodSlug(slug) {
		return errors.New("метка набора пишется латиницей и цифрами через дефис, " +
			"например cardio-basics")
	}
	var exists bool
	if err := s.gate.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM packs WHERE slug = $1)`, slug).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("набор с меткой %s уже заведён", slug)
	}
	if _, err := s.gate.Exec(ctx,
		`INSERT INTO packs (slug, title, summary_md, status)
		 VALUES ($1, $2, $3, 'published')`, slug, title, summary); err != nil {
		return err
	}
	return nil
}

// goodSlug — та же форма метки, что стоит в схеме.
//
// Повторение правила здесь не второе место для него: здесь оно служит
// внятному отказу, а держит форму по-прежнему база. Разойдись они — база
// откажет, и это верная сторона для расхождения.
func goodSlug(slug string) bool {
	if slug == "" {
		return false
	}
	for i, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// SetItems задаёт состав набора целиком.
//
// Целиком и одной транзакцией: состав, переписанный наполовину, — это
// набор, которого никто не собирал. Порядок берётся из порядка списка:
// он и есть то, что решил составитель.
//
// Состав правится свободно, и это не противоречит неизменяемости выпуска:
// выпуск — снимок состава на миг подписи, и новый состав доедет до
// устройств только следующим выпуском.
func (s *Store) SetItems(ctx context.Context, slug string, cases []string) (int, error) {
	var count int
	err := s.gate.InTx(ctx, func(tx pgx.Tx) error {
		var packID int64
		err := tx.QueryRow(ctx, `SELECT id FROM packs WHERE slug = $1 FOR UPDATE`, slug).Scan(&packID)
		if err == pgx.ErrNoRows {
			return errors.New("такого набора нет")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM pack_items WHERE pack_id = $1`, packID); err != nil {
			return err
		}
		seen := map[string]bool{}
		for ord, id := range cases {
			if id == "" || seen[id] {
				// Повтор в списке — не повод ронять состав: составитель
				// перетащил задачу дважды, а не сломал набор. Молча
				// пропускаем второй раз, а не кладём его вторым номером.
				continue
			}
			seen[id] = true
			tag, err := tx.Exec(ctx,
				`INSERT INTO pack_items (pack_id, case_id, ord)
				 SELECT $1, $2, $3 WHERE EXISTS (SELECT 1 FROM cases WHERE id = $2)`,
				packID, id, ord)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				// Задачи нет вовсе. Отказ с именем: состав, принятый
				// молча и без неё, выглядит собранным и таковым не
				// является.
				return fmt.Errorf("задачи %s нет", id)
			}
			count++
		}
		return nil
	})
	return count, err
}

// All отдаёт наборы составителю.
func (s *Store) All(ctx context.Context) ([]Shown, error) {
	rows, err := s.gate.Query(ctx, `
		SELECT p.slug, p.title, p.status,
		       (SELECT count(*) FROM pack_items i WHERE i.pack_id = p.id),
		       coalesce((SELECT max(version) FROM pack_releases r WHERE r.pack_id = p.id), 0)
		  FROM packs p
		 ORDER BY p.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Shown{}
	for rows.Next() {
		var one Shown
		if err := rows.Scan(&one.Slug, &one.Title, &one.Status, &one.Cases, &one.Version); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}
