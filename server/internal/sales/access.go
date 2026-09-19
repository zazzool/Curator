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

	"curator/server/internal/dbgate"
)

// Access отвечает, открыт ли набор врачу.
type Access struct {
	gate *dbgate.Gate
}

func NewAccess(gate *dbgate.Gate) *Access { return &Access{gate: gate} }

// Allowed — открыт ли набор.
//
// Порядок доводов важен, и он же порядок дешевизны: бесплатный набор не
// требует ни одного взгляда на права, подписка открывает всё, право на
// набор открывает его один.
//
// # Набор без цены — бесплатный
//
// Правило названо вслух, потому что молчаливое «нет цены — значит закрыто»
// закрыло бы всё, что составитель ещё не оценил, и он узнал бы об этом от
// врача. Обратное — «нет цены, значит открыто» — ошибается в сторону
// щедрости, и это верная сторона: незаданная цена видна в студии, а
// незаслуженно закрытый набор не виден никому.
func (a *Access) Allowed(ctx context.Context, accountID int64, slug string, now time.Time) (bool, error) {
	var paid bool
	if err := a.gate.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM prices
		                 WHERE purpose = $1 AND enabled AND amount_kopecks > 0)`,
		"pack:"+slug).Scan(&paid); err != nil {
		return false, err
	}
	if !paid {
		return true, nil
	}

	var allowed bool
	err := a.gate.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM entitlements e
		     LEFT JOIN packs p ON p.id = e.pack_id
		     WHERE e.account_id = $1
		       AND e.revoked_at IS NULL
		       AND e.starts_at <= $3
		       AND (e.expires_at IS NULL OR e.expires_at > $3)
		       AND (e.kind = 'subscription' OR p.slug = $2)
		)`, accountID, slug, now).Scan(&allowed)
	return allowed, err
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
	if err == pgx.ErrNoRows {
		return 0, fmt.Errorf("набора %s нет", slug)
	}
	return id, err
}
