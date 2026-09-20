package audience

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Store — группы в базе.
type Store struct {
	gate *dbgate.Gate
}

func NewStore(gate *dbgate.Gate) *Store { return &Store{gate: gate} }

// Group — группа глазами студии.
type Group struct {
	ID    int64
	Slug  string
	Title string
	Note  string
	Rule  Rule

	// Broken — почему правило не применяется. Пусто — применяется.
	//
	// Строкой, а не флагом: составителю нужно знать, что именно в правиле
	// не разобрано, иначе он будет менять его наугад.
	Broken string

	// Members — сколько врачей названо поимённо.
	Members int
}

// Modes — чем набор связан с группой.
const (
	// ModeOpen — набор открыт членам группы помимо линейки.
	ModeOpen = "open"

	// ModeHidden — набор скрыт от членов группы.
	//
	// Скрытие не отбирает купленное: право на набор сильнее и его, и
	// линейки. Отобрать оплаченное сменой правила значило бы отобрать
	// деньги молча.
	ModeHidden = "hidden"
)

// Binding — связь набора с группой.
type Binding struct {
	PackID     int64
	AudienceID int64
	Mode       string
}

// All отдаёт группы.
//
// Правило, которое не разобралось, не выбрасывается и не роняет список:
// группа приезжает со словами о поломке, и составитель видит ровно то,
// что видит сервер. Молча выброшенная группа выглядела бы как
// несуществующая, а она существует и на что-то ссылается.
func (s *Store) All(ctx context.Context) ([]Group, error) {
	rows, err := s.gate.Query(ctx, `
		SELECT g.id, g.slug, g.title, g.note, g.rule,
		       (SELECT count(*) FROM audience_members m WHERE m.audience_id = g.id)
		  FROM audiences g
		 ORDER BY g.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Пустой список — [], а не null: установка без групп исправна, и
	// поначалу она единственная.
	out := []Group{}
	for rows.Next() {
		var one Group
		var raw []byte
		if err := rows.Scan(&one.ID, &one.Slug, &one.Title, &one.Note, &raw, &one.Members); err != nil {
			return nil, err
		}
		rule, err := ParseRule(raw)
		if err != nil {
			one.Broken = err.Error()
		} else {
			one.Rule = rule
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

// One отдаёт группу по метке.
func (s *Store) One(ctx context.Context, slug string) (Group, error) {
	var one Group
	var raw []byte
	err := s.gate.QueryRow(ctx, `
		SELECT g.id, g.slug, g.title, g.note, g.rule,
		       (SELECT count(*) FROM audience_members m WHERE m.audience_id = g.id)
		  FROM audiences g WHERE g.slug = $1`, slug).
		Scan(&one.ID, &one.Slug, &one.Title, &one.Note, &raw, &one.Members)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, fmt.Errorf("группы %s нет", slug)
	}
	if err != nil {
		return Group{}, err
	}
	rule, err := ParseRule(raw)
	if err != nil {
		one.Broken = err.Error()
	} else {
		one.Rule = rule
	}
	return one, nil
}

// Create заводит группу.
func (s *Store) Create(ctx context.Context, slug, title, note string, rule Rule) error {
	if title == "" {
		return errors.New("у группы должно быть название")
	}
	if !goodSlug(slug) {
		return errors.New("метка группы пишется латиницей и цифрами через дефис, " +
			"например kafedra-terapii")
	}
	if err := rule.Check(); err != nil {
		return err
	}
	raw, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	var exists bool
	if err := s.gate.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM audiences WHERE slug = $1)`, slug).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("группа с меткой %s уже заведена", slug)
	}
	_, err = s.gate.Exec(ctx,
		`INSERT INTO audiences (slug, title, note, rule) VALUES ($1, $2, $3, $4)`,
		slug, title, note, raw)
	return err
}

// Update правит группу.
func (s *Store) Update(ctx context.Context, slug, title, note string, rule Rule) error {
	if title == "" {
		return errors.New("у группы должно быть название")
	}
	if err := rule.Check(); err != nil {
		return err
	}
	raw, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	tag, err := s.gate.Exec(ctx, `
		UPDATE audiences SET title = $2, note = $3, rule = $4, updated_at = NOW()
		 WHERE slug = $1`, slug, title, note, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("группы %s нет", slug)
	}
	return nil
}

// Drop убирает группу.
//
// Группа, которой что-то открыто или от которой что-то скрыто, не
// убирается: удаление такой группы молча сменило бы состав корпуса у всех
// её членов, и объяснить это потом было бы нечем. Сперва развязывается
// набор — это видно на его карточке.
func (s *Store) Drop(ctx context.Context, slug string) error {
	return s.gate.InTx(ctx, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM audiences WHERE slug = $1`, slug).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("группы %s нет", slug)
		}
		if err != nil {
			return err
		}
		var bound int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM pack_audiences WHERE audience_id = $1`, id).Scan(&bound); err != nil {
			return err
		}
		if bound > 0 {
			return fmt.Errorf("группе открыто или от неё скрыто наборов: %d. "+
				"Развяжите их на карточках наборов, и группу можно будет убрать", bound)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM audience_members WHERE audience_id = $1`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM audiences WHERE id = $1`, id)
		return err
	})
}

// Member — врач, названный в группе поимённо.
type Member struct {
	AccountID int64
	Email     string
	AddedBy   string
	AddedAt   time.Time
}

// Members отдаёт поимённый состав группы.
func (s *Store) Members(ctx context.Context, slug string) ([]Member, error) {
	rows, err := s.gate.Query(ctx, `
		SELECT m.account_id, coalesce(a.email, ''), m.added_by, m.created_at
		  FROM audience_members m
		  JOIN audiences g ON g.id = m.audience_id
		  JOIN accounts a ON a.id = m.account_id
		 WHERE g.slug = $1
		 ORDER BY m.created_at DESC, m.account_id`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Member{}
	for rows.Next() {
		var one Member
		if err := rows.Scan(&one.AccountID, &one.Email, &one.AddedBy, &one.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

// AddMember называет врача в группе поимённо.
//
// Повторное добавление — не отказ: оператор, добавляющий кафедру списком,
// не должен спотыкаться на том, кто уже добавлен.
func (s *Store) AddMember(ctx context.Context, slug string, accountID int64, by string) error {
	tag, err := s.gate.Exec(ctx, `
		INSERT INTO audience_members (audience_id, account_id, added_by)
		SELECT g.id, a.id, $3
		  FROM audiences g, accounts a
		 WHERE g.slug = $1 AND a.id = $2
		ON CONFLICT DO NOTHING`, slug, accountID, by)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Ноль строк — это либо «уже был», либо «некого или некуда
		// добавлять». Разводится это здесь, а не молчанием: оператор,
		// опечатавшийся в номере, иначе увидел бы успех.
		var exists bool
		if err := s.gate.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM audience_members m
			                JOIN audiences g ON g.id = m.audience_id
			               WHERE g.slug = $1 AND m.account_id = $2)`,
			slug, accountID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("не нашлось группы %s или врача с номером %d", slug, accountID)
		}
	}
	return nil
}

// DropMember убирает врача из поимённого состава.
func (s *Store) DropMember(ctx context.Context, slug string, accountID int64) error {
	tag, err := s.gate.Exec(ctx, `
		DELETE FROM audience_members m
		 USING audiences g
		 WHERE g.id = m.audience_id AND g.slug = $1 AND m.account_id = $2`, slug, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("врача с номером %d в группе %s нет", accountID, slug)
	}
	return nil
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
