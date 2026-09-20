// Доступ и продажи.
//
// # Право отвязано от способа оплаты
//
// Право выдаётся платежом, живёт само и проверяется одинаково, чем бы ни
// было куплено — ручным приходом, эквайером или магазином приложений. Два
// учёта, склеенные задним числом, — это миграция по живым деньгам, и делать
// её будет тот, кто не знает, почему их два.
//
// # Приход оформляет оператор
//
// Эквайера пока нет, и это решение, а не задел: настройка приёма платежей,
// включённая раньше проверки подписи уведомления, открывает всякому
// желающему ручку, которая оформляет права. Пока подписи нет, приход
// оформляет человек, и у каждого прихода есть имя того, кто его оформил.
package sales

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/audience"
	"curator/server/internal/dbgate"
	"curator/server/internal/packs"
)

// Access отвечает, открыт ли набор врачу.
type Access struct {
	gate *dbgate.Gate

	// groups — группы врачей. Они добавляют к линейке два довода:
	// «открыто группе» и «скрыто от группы».
	groups *audience.Store
}

func NewAccess(gate *dbgate.Gate) *Access {
	return &Access{gate: gate, groups: audience.NewStore(gate)}
}

// verdicts — что группы говорят о наборах этому врачу.
//
// Одна работа на оба расчёта — и на витрину с корпусом (OpenPackIDs), и
// на выгрузку (Allowed). Собирай каждый сведения по-своему, они однажды
// разойдутся: у донора четыре таких места собирали права каждое сам, и
// заказные наборы знали не все четыре.
func (a *Access) verdicts(ctx context.Context, accountID int64, now time.Time) (map[int64]audience.Verdict, error) {
	in, err := a.groups.Membership(ctx, accountID, now)
	if err != nil {
		return nil, err
	}
	if len(in) == 0 {
		// Врач не попал ни в одну группу — связи читать незачем.
		return nil, nil
	}
	bindings, err := a.groups.Bindings(ctx)
	if err != nil {
		return nil, err
	}
	return audience.Verdicts(in, bindings), nil
}

// Allowed — открыт ли набор.
//
// Решает не эта работа, а `packs.OpenTo`: она одна на витрину, корпус и
// выгрузку. Здесь только собираются сведения, которых ей не хватает.
//
// # Чем это было прежде
//
// Прежде правилом было «нет действующей цены — набор бесплатен», и оно
// ошибалось в сторону щедрости сознательно: незаданная цена видна в
// студии, а незаслуженно закрытый набор не виден никому. Правило
// держалось ровно до тех пор, пока граница бесплатного не была названа.
// Теперь она названа линейкой (решение владельца 2026-09-20), и цена
// отвечает на другой вопрос — почём продаётся, а не кому открыто. Иначе
// вышло бы два источника правды об одном: выключенная цена открывала бы
// платный набор всем, а линейка при этом говорила бы «платный».
//
// Бесплатным набор делается линейкой `sponsored`, и это видно в студии
// словом, а не отсутствием числа.
func (a *Access) Allowed(ctx context.Context, accountID int64, slug string, now time.Time) (bool, error) {
	each, err := a.StateEach(ctx, accountID, []string{slug}, now)
	if err != nil {
		return false, err
	}
	// Набора нет — и закрыт он не потому, что за него не заплатили.
	// Отвечать «открыт» здесь значило бы пустить выгрузку дальше, к
	// набору, которого нет.
	return each[slug].Open, nil
}

// State — что показывать про набор этому врачу.
type State struct {
	// Open — набор открыт: качать можно.
	Open bool

	// Hidden — набор скрыт от врача группой.
	//
	// Отдельно от Open, потому что «закрыт» и «скрыт» — разные ответы
	// витрине. Закрытый набор показывается и предлагает открыть себя
	// покупкой или входом; скрытый не показывается вовсе, иначе
	// «скрыть» означало бы «оставить на прилавке с ценником» — и врач
	// купил бы то, что мы от него прячем.
	//
	// Скрытый и при этом открытый — не противоречие, а купивший: право
	// сильнее скрытия, и такой набор остаётся на витрине.
	Hidden bool
}

// StateEach отвечает про несколько наборов разом.
//
// # Зачем разом, а не по одному
//
// Витрина спрашивает про каждый набор списка, и пока ответ стоил одного
// запроса, спрашивать по одному было незаметно. С группами ответ стоит
// ещё и портрета врача — а портрет у давнего пользователя читает его
// попытки. Двадцать наборов витрины превратились бы в двадцать таких
// чтений одного и того же.
//
// Сведения о самом враче собираются один раз, группы — один раз, и это
// же делает Allowed частным случаем: место сборки Rights в службе одно.
// У донора таких мест было четыре, каждое собирало по-своему, и однажды
// они разошлись — заказные наборы знали не все четыре.
func (a *Access) StateEach(ctx context.Context, accountID int64, slugs []string, now time.Time) (map[string]State, error) {
	out := map[string]State{}
	if len(slugs) == 0 {
		return out, nil
	}

	var subscribed, emailBound bool
	if err := a.gate.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM entitlements e
		                WHERE e.account_id = $1 AND e.kind = 'subscription'
		                  AND e.revoked_at IS NULL AND e.starts_at <= $2
		                  AND (e.expires_at IS NULL OR e.expires_at > $2)),
		       EXISTS (SELECT 1 FROM accounts a
		                WHERE a.id = $1 AND a.email IS NOT NULL)`,
		accountID, now).Scan(&subscribed, &emailBound); err != nil {
		return nil, err
	}

	verdicts, err := a.verdicts(ctx, accountID, now)
	if err != nil {
		return nil, err
	}

	rows, err := a.gate.Query(ctx, `
		SELECT p.id, p.slug, p.line,
		       EXISTS (SELECT 1 FROM entitlements e
		                WHERE e.account_id = $1 AND e.pack_id = p.id
		                  AND e.revoked_at IS NULL AND e.starts_at <= $2
		                  AND (e.expires_at IS NULL OR e.expires_at > $2))
		  FROM packs p WHERE p.slug = ANY($3)`, accountID, now, slugs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var slug, line string
		var owns bool
		if err := rows.Scan(&id, &slug, &line, &owns); err != nil {
			return nil, err
		}
		v := verdicts[id]
		out[slug] = State{
			Open: packs.OpenTo(line, packs.Rights{
				Owns: owns, Subscribed: subscribed, EmailBound: emailBound,
				Granted: v.Granted, Hidden: v.Hidden,
			}),
			Hidden: v.Hidden,
		}
	}
	return out, rows.Err()
}

// OpenPackIDs — номера наборов, открытых этой учётной записи.
//
// Второе значение — «есть что резать». Ложь означает, что у установки нет
// ни одного выпущенного набора: резать корпус нечем, и урезать его в
// пустоту значило бы показать врачу пустое приложение там, где никто
// ничего не закрывал. Это единственный случай, когда корпус отдаётся
// целиком, и он же случай только что поднятой установки.
//
// Снятый с витрины набор в список не попадает, а купившему остаётся
// открыт: покупка сильнее витрины, и набор, ушедший в архив, не должен
// исчезать из корпуса у того, кто за него заплатил.
func (a *Access) OpenPackIDs(ctx context.Context, accountID int64, now time.Time) ([]int64, bool, error) {
	var subscribed, emailBound bool
	if err := a.gate.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM entitlements e
		                WHERE e.account_id = $1 AND e.kind = 'subscription'
		                  AND e.revoked_at IS NULL AND e.starts_at <= $2
		                  AND (e.expires_at IS NULL OR e.expires_at > $2)),
		       EXISTS (SELECT 1 FROM accounts a
		                WHERE a.id = $1 AND a.email IS NOT NULL)`,
		accountID, now).Scan(&subscribed, &emailBound); err != nil {
		return nil, false, err
	}

	verdicts, err := a.verdicts(ctx, accountID, now)
	if err != nil {
		return nil, false, err
	}

	rows, err := a.gate.Query(ctx, `
		SELECT p.id, p.line, p.status,
		       EXISTS (SELECT 1 FROM entitlements e
		                WHERE e.account_id = $1 AND e.pack_id = p.id
		                  AND e.revoked_at IS NULL AND e.starts_at <= $2
		                  AND (e.expires_at IS NULL OR e.expires_at > $2))
		  FROM packs p`, accountID, now)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	open := []int64{}
	any := false
	for rows.Next() {
		var id int64
		var line, status string
		var owns bool
		if err := rows.Scan(&id, &line, &status, &owns); err != nil {
			return nil, false, err
		}
		if status == "published" {
			any = true
		}
		if status != "published" && !owns {
			// Черновик не раздаётся никому, а снятый с витрины —
			// только тому, кто его купил.
			continue
		}
		v := verdicts[id]
		if packs.OpenTo(line, packs.Rights{
			Owns: owns, Subscribed: subscribed, EmailBound: emailBound,
			Granted: v.Granted, Hidden: v.Hidden,
		}) {
			open = append(open, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return open, any, nil
}

// Entitlement — действующее право.
type Entitlement struct {
	Kind      string
	Pack      string
	Origin    string
	StartsAt  time.Time
	ExpiresAt *time.Time
}

// Live отдаёт действующие права врача.
func (a *Access) Live(ctx context.Context, accountID int64, now time.Time) ([]Entitlement, error) {
	rows, err := a.gate.Query(ctx, `
		SELECT e.kind, coalesce(p.slug, ''), e.origin, e.starts_at, e.expires_at
		  FROM entitlements e
		  LEFT JOIN packs p ON p.id = e.pack_id
		 WHERE e.account_id = $1
		   AND e.revoked_at IS NULL
		   AND e.starts_at <= $2
		   AND (e.expires_at IS NULL OR e.expires_at > $2)
		 ORDER BY e.id`, accountID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Пустой список — [], а не null: врач без единой покупки — исправный
	// случай, и поначалу единственный.
	out := []Entitlement{}
	for rows.Next() {
		var one Entitlement
		if err := rows.Scan(&one.Kind, &one.Pack, &one.Origin, &one.StartsAt, &one.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

// Purpose — назначение платежа: что именно куплено.
//
// Строкой, а не парой колонок, потому что читается человеком в
// разбирательстве. Образец закрыт: назначение с опечаткой не найдётся ни
// одним отчётом и будет выглядеть отсутствующим.
type Purpose string

// ParsePurpose разбирает назначение и отвечает, годное ли оно.
func ParsePurpose(raw string) (Purpose, error) {
	switch {
	case strings.HasPrefix(raw, "pack:"):
		slug := strings.TrimPrefix(raw, "pack:")
		if !goodSlug(slug) {
			return "", fmt.Errorf("метка набора в назначении негодна: %q", slug)
		}
		return Purpose(raw), nil
	case raw == "subscription:month", raw == "subscription:year":
		return Purpose(raw), nil
	default:
		return "", errors.New("назначение платежа непонятно: " +
			"ожидается pack:<метка> или subscription:month|year")
	}
}

// Pack отдаёт метку набора, если назначение — набор.
func (p Purpose) Pack() (string, bool) {
	if !strings.HasPrefix(string(p), "pack:") {
		return "", false
	}
	return strings.TrimPrefix(string(p), "pack:"), true
}

// Period отдаёт срок подписки, если назначение — подписка.
func (p Purpose) Period() (time.Duration, bool) {
	switch p {
	case "subscription:month":
		// Месяц считается тридцатью сутками, а не «тем же числом
		// следующего месяца»: второе даёт февральскому покупателю двадцать
		// восемь дней вместо тридцати одного, и объяснить ему это нечем.
		return 30 * 24 * time.Hour, true
	case "subscription:year":
		return 365 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

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

// packID находит набор по метке.
func packID(ctx context.Context, tx pgx.Tx, slug string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM packs WHERE slug = $1`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("набора %s нет", slug)
	}
	return id, err
}
