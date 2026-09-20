package sales

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Payments — приход и возвраты.
type Payments struct {
	gate *dbgate.Gate
}

func NewPayments(gate *dbgate.Gate) *Payments { return &Payments{gate: gate} }

// Income — приход, оформляемый оператором.
type Income struct {
	AccountID int64
	Purpose   string
	Kopecks   int64
	IdemKey   string
	Note      string
	By        string
}

// Payment — оформленный платёж.
type Payment struct {
	ID        int64
	AccountID int64
	Source    string
	Purpose   string
	Kopecks   int64
	Status    string
	Note      string
	By        string
	At        time.Time
	Repeated  bool
}

// Accept оформляет приход и выдаёт право одной транзакцией.
//
// Одной транзакцией намеренно: платёж без права — это деньги, за которые
// врач ничего не получил, а право без платежа — доступ, за который никто
// не платил. Обрыв между двумя записями дал бы то или другое, и разбирать
// это пришлось бы по журналу.
//
// Повтор с тем же ключом не заводит второго платежа и не выдаёт второго
// права: оператор нажал дважды, а не принял деньги дважды. Держит это
// указатель базы, а не проверка перед вставкой — проверка и вставка два
// шага, а соперники видят одно состояние и сталкиваются между ними.
func (p *Payments) Accept(ctx context.Context, in Income, now time.Time) (Payment, error) {
	purpose, err := ParsePurpose(in.Purpose)
	if err != nil {
		return Payment{}, err
	}
	if in.Kopecks <= 0 {
		// Приход на ноль — это не приход, а описка. Принять его значит
		// выдать право бесплатно и записать это как продажу.
		return Payment{}, errors.New("сумма прихода должна быть больше нуля")
	}
	if in.IdemKey == "" {
		return Payment{}, errors.New("у прихода должен быть ключ повторности")
	}
	if in.By == "" {
		// Имя оформившего обязательно: приход, подтверждённый вне
		// системы, проверяется только разговором с тем, кто его принял.
		return Payment{}, errors.New("приход оформляется поимённо")
	}

	var out Payment
	err = p.gate.InTx(ctx, func(tx pgx.Tx) error {
		// Запись врача берётся под замок ПЕРВОЙ командой транзакции, и это
		// не осторожность, а починка — причём двух бед подряд.
		//
		// Первая: чтение конца действующей подписки и вставка нового права
		// — два запроса, и под обычным уровнем изоляции они не мешают друг
		// другу. Две оплаты, пришедшие разом, видели одно и то же «до» и
		// обе считали от него: врач платил за два месяца и получал один.
		// Воспроизводилось в тридцати девяти случаях из сорока.
		//
		// Вторая: замок обязан стоять ИМЕННО ЗДЕСЬ, до вставки платежа, а
		// не там, где читается срок. Вставка в payments держит внешний ключ
		// на accounts и потому сама берёт строку врача под FOR KEY SHARE.
		// Замок, поставленный после неё, — это повышение уже взятого
		// разделяемого до исключительного, и два соперника повышают его
		// одновременно: база отвечает «deadlock detected» и роняет один из
		// платежей. Замок перед вставкой выстраивает оплаты в очередь
		// раньше, чем кто-то успеет взять разделяемый.
		if _, err := tx.Exec(ctx,
			`SELECT id FROM accounts WHERE id = $1 FOR UPDATE`,
			in.AccountID); err != nil {
			return fmt.Errorf("запись врача не взята под замок: %w", err)
		}

		var id int64
		err := tx.QueryRow(ctx, `
			INSERT INTO payments (account_id, source, purpose, amount_kopecks,
			                      status, idem_key, note, created_by, created_at)
			VALUES ($1, 'operator', $2, $3, 'paid', $4, $5, $6, $7)
			ON CONFLICT (idem_key) DO NOTHING
			RETURNING id`,
			in.AccountID, string(purpose), in.Kopecks, in.IdemKey, in.Note, in.By, now).Scan(&id)

		if errors.Is(err, pgx.ErrNoRows) {
			// Такой ключ уже приходил. Отдаём прежний платёж, а не
			// отказ: оператор должен увидеть, что приход уже оформлен, и
			// не оформить его ещё раз другим ключом.
			//
			// Но прежде сверяем, ТОТ ЛИ это приход. Ключ уникален сам по
			// себе, а не в паре с врачом и назначением, и раньше ответ
			// отдавался без сверки: второй врач платил, получал чужой
			// платёж с пометкой «повтор» и оставался без прав, а оператор
			// читал уверенное «уже оформлен». Совпадение ключа у двух
			// разных приходов — это описка, и говорить о ней надо словами.
			prior, err := load(ctx, tx, in.IdemKey)
			if err != nil {
				return err
			}
			if prior.AccountID != in.AccountID ||
				prior.Purpose != string(purpose) ||
				prior.Kopecks != in.Kopecks {
				return fmt.Errorf(
					"ключ повторности %q уже занят другим приходом "+
						"(врач %d, %s, %d коп.): возьмите другой ключ",
					in.IdemKey, prior.AccountID, prior.Purpose, prior.Kopecks)
			}
			out = prior
			out.Repeated = true
			return nil
		}
		if err != nil {
			return err
		}

		if err := grant(ctx, tx, in.AccountID, purpose, id, in.By, now); err != nil {
			return err
		}
		out = Payment{ID: id, AccountID: in.AccountID, Source: "operator",
			Purpose: string(purpose), Kopecks: in.Kopecks, Status: "paid",
			Note: in.Note, By: in.By, At: now}
		return nil
	})
	if err != nil {
		return Payment{}, err
	}
	return out, nil
}

// grant выдаёт право по платежу.
func grant(ctx context.Context, tx pgx.Tx, accountID int64, purpose Purpose, paymentID int64, by string, now time.Time) error {
	if slug, ok := purpose.Pack(); ok {
		id, err := packID(ctx, tx, slug)
		if err != nil {
			return err
		}
		// Право на набор бессрочно: купленное не отбирают со временем.
		_, err = tx.Exec(ctx, `
			INSERT INTO entitlements (account_id, kind, pack_id, origin, starts_at,
			                          payment_id, granted_by)
			VALUES ($1, 'pack', $2, 'purchase', $3, $4, $5)
			ON CONFLICT (account_id, pack_id, origin) WHERE revoked_at IS NULL
			DO NOTHING`, accountID, id, now, paymentID, by)
		return err
	}

	period, ok := purpose.Period()
	if !ok {
		return fmt.Errorf("назначение %q не даёт ни набора, ни срока", purpose)
	}

	// Замок на записи врача здесь уже держится — его ставит Accept первой
	// командой транзакции. Довод, почему именно там, записан у него.

	// Подписка продлевается от конца действующей, а не от сегодня: купивший
	// второй месяц за неделю до конца первого иначе потерял бы неделю, за
	// которую уже заплатил.
	from := now
	var until *time.Time
	err := tx.QueryRow(ctx, `
		SELECT max(expires_at) FROM entitlements
		 WHERE account_id = $1 AND kind = 'subscription'
		   AND revoked_at IS NULL AND expires_at > $2`, accountID, now).Scan(&until)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if until != nil && until.After(now) {
		from = *until
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO entitlements (account_id, kind, origin, starts_at, expires_at,
		                          payment_id, granted_by)
		VALUES ($1, 'subscription', 'subscription', $2, $3, $4, $5)`,
		accountID, now, from.Add(period), paymentID, by)
	return err
}

// Refund возвращает платёж и отзывает выданное им право.
//
// Право ищется ПО платежу, а не по врачу и назначению: у врача может быть
// два права на один набор — купленное и подаренное, — и отзыв по
// назначению снял бы не то. Отозванное право не удаляется: отзыв это
// событие, и он обязан остаться видимым.
func (p *Payments) Refund(ctx context.Context, paymentID int64, note, by string, now time.Time) error {
	if by == "" {
		return errors.New("возврат оформляется поимённо")
	}
	return p.gate.InTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE payments SET status = 'refunded', refunded_at = $2, refund_note = $3
			 WHERE id = $1 AND status = 'paid'`, paymentID, now, note)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// Отказ, а не молчаливый успех: «вернули» платёж, которого
			// нет или который уже возвращён, оператор примет за правду.
			return errors.New("такого оплаченного платежа нет: возможно, возврат уже оформлен")
		}
		_, err = tx.Exec(ctx, `
			UPDATE entitlements
			   SET revoked_at = $2, revoke_note = $3
			 WHERE payment_id = $1 AND revoked_at IS NULL`, paymentID, now, note)
		return err
	})
}

// Recent отдаёт платежи врача, новые первыми.
func (p *Payments) Recent(ctx context.Context, accountID int64, limit int) ([]Payment, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := p.gate.Query(ctx, `
		SELECT id, account_id, source, purpose, amount_kopecks, status,
		       note, created_by, created_at
		  FROM payments WHERE account_id = $1 ORDER BY id DESC LIMIT $2`,
		accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Payment{}
	for rows.Next() {
		var one Payment
		if err := rows.Scan(&one.ID, &one.AccountID, &one.Source, &one.Purpose,
			&one.Kopecks, &one.Status, &one.Note, &one.By, &one.At); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

func load(ctx context.Context, tx pgx.Tx, idemKey string) (Payment, error) {
	var out Payment
	err := tx.QueryRow(ctx, `
		SELECT id, account_id, source, purpose, amount_kopecks, status,
		       note, created_by, created_at
		  FROM payments WHERE idem_key = $1`, idemKey).
		Scan(&out.ID, &out.AccountID, &out.Source, &out.Purpose, &out.Kopecks,
			&out.Status, &out.Note, &out.By, &out.At)
	return out, err
}
