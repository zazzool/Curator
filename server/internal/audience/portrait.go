package audience

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Портрет врача и принадлежность к группам.
//
// # Почему признаки считает база, а правило применяет Go
//
// Правило написано в одном месте — в `Rule.Matches`. База отвечает только
// на вопросы о фактах: привязана ли почта, сколько задач решено, сколько
// дней не заходил. Второй реализации правила на SQL нет, и потому
// расходиться нечему: пара «одно правило на Go и на SQL» расходится молча
// и проверяется только сверкой поле за полем на живой базе.
//
// # Почему поведение спрашивается не всегда
//
// Поведенческая половина портрета читает попытки врача, а у давнего
// пользователя их десятки тысяч, и спрашивается портрет на каждом
// обращении за корпусом. Поэтому запрос идёт только тогда, когда о
// поведении спрашивает хоть одно живое правило. На установке без групп он
// не идёт ни разу, и группы там не стоят ничего.

// Portrait собирает портрет одного врача.
//
// behaviour — нужна ли поведенческая половина. Без неё карты попыток
// пусты, и признаки о поведении не выполняются: это верно ровно потому,
// что спрашивать их в этом случае некому.
func (s *Store) Portrait(ctx context.Context, accountID int64, now time.Time, behaviour bool) (Portrait, error) {
	var p Portrait
	err := s.gate.QueryRow(ctx, `
		SELECT a.email IS NOT NULL,
		       EXISTS (SELECT 1 FROM entitlements e
		                WHERE e.account_id = a.id AND e.kind = 'subscription'
		                  AND e.revoked_at IS NULL AND e.starts_at <= $2
		                  AND (e.expires_at IS NULL OR e.expires_at > $2)),
		       EXISTS (SELECT 1 FROM entitlements e
		                WHERE e.account_id = a.id
		                  AND e.origin IN ('purchase', 'subscription')),
		       GREATEST(0, (EXTRACT(EPOCH FROM ($2::timestamptz - a.created_at)) / 86400)::int),
		       GREATEST(0, (EXTRACT(EPOCH FROM ($2::timestamptz - coalesce(a.last_seen, a.created_at))) / 86400)::int)
		  FROM accounts a WHERE a.id = $1`, accountID, now).
		Scan(&p.EmailBound, &p.Subscribed, &p.EverPaid, &p.AgeDays, &p.IdleDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return Portrait{}, fmt.Errorf("учётной записи %d нет", accountID)
	}
	if err != nil {
		return Portrait{}, err
	}

	p.Solved = map[string]int{}
	p.Correct = map[string]int{}
	p.ActiveDays = map[string]int{}
	if !behaviour {
		return p, nil
	}

	// Окна берутся доводами, а не вписываются в текст запроса: собранный
	// из чисел, взятых в базе, SQL перестаёт быть проверяемым глазами.
	//
	// Время и число суток приведены к типу прямо в запросе, и это не
	// перестраховка. Без приведения Postgres выводит тип довода из того,
	// как он употреблён, и в `$2 - make_interval(…)` выводит ИНТЕРВАЛ:
	// разность двух интервалов — интервал, и сравнение с `happened_at`
	// отказывает целиком. Сторож поймал это первым же прогоном.
	//
	// День считается по UTC, и это решение, а не недосмотр: врачи сидят в
	// разных поясах, и «день занятий», посчитанный по поясу
	// спрашивающего, давал бы разное число при одних и тех же попытках.
	var s7, s30, s90, c0, c7, c30, c90, d0, d7, d30, d90, s0 int
	err = s.gate.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE happened_at > $2::timestamptz - make_interval(days => $3::int)),
		       count(*) FILTER (WHERE happened_at > $2::timestamptz - make_interval(days => $4::int)),
		       count(*) FILTER (WHERE happened_at > $2::timestamptz - make_interval(days => $5::int)),
		       count(*) FILTER (WHERE correct),
		       count(*) FILTER (WHERE correct AND happened_at > $2::timestamptz - make_interval(days => $3::int)),
		       count(*) FILTER (WHERE correct AND happened_at > $2::timestamptz - make_interval(days => $4::int)),
		       count(*) FILTER (WHERE correct AND happened_at > $2::timestamptz - make_interval(days => $5::int)),
		       count(DISTINCT (happened_at AT TIME ZONE 'UTC')::date),
		       count(DISTINCT (happened_at AT TIME ZONE 'UTC')::date)
		           FILTER (WHERE happened_at > $2::timestamptz - make_interval(days => $3::int)),
		       count(DISTINCT (happened_at AT TIME ZONE 'UTC')::date)
		           FILTER (WHERE happened_at > $2::timestamptz - make_interval(days => $4::int)),
		       count(DISTINCT (happened_at AT TIME ZONE 'UTC')::date)
		           FILTER (WHERE happened_at > $2::timestamptz - make_interval(days => $5::int))
		  FROM attempts WHERE account_id = $1`,
		accountID, now, WindowDays[Window7], WindowDays[Window30], WindowDays[Window90]).
		Scan(&s0, &s7, &s30, &s90, &c0, &c7, &c30, &c90, &d0, &d7, &d30, &d90)
	if err != nil {
		return Portrait{}, err
	}
	p.Solved = map[string]int{WindowAll: s0, Window7: s7, Window30: s30, Window90: s90}
	p.Correct = map[string]int{WindowAll: c0, Window7: c7, Window30: c30, Window90: c90}
	p.ActiveDays = map[string]int{WindowAll: d0, Window7: d7, Window30: d30, Window90: d90}
	return p, nil
}

// Membership — группы, в которые попадает врач.
//
// Считается одним местом на всё: витрина, корпус и выгрузка обязаны
// получать один и тот же ответ. У донора четыре места собирали сведения о
// правах каждое по-своему, и однажды разошлись — заказные наборы знали не
// все четыре. Здесь такого не будет: собирает это одна работа.
func (s *Store) Membership(ctx context.Context, accountID int64, now time.Time) ([]int64, error) {
	groups, err := s.live(ctx)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		// Групп нет — портрет не считается вовсе.
		return nil, nil
	}

	behaviour := false
	for _, one := range groups {
		if one.Rule.NeedsBehaviour() {
			behaviour = true
			break
		}
	}
	portrait, err := s.Portrait(ctx, accountID, now, behaviour)
	if err != nil {
		return nil, err
	}

	named, err := s.named(ctx, accountID)
	if err != nil {
		return nil, err
	}

	in := []int64{}
	for _, one := range groups {
		if named[one.ID] || one.Rule.Matches(portrait) {
			in = append(in, one.ID)
		}
	}
	return in, nil
}

// live читает группы, чьё правило разобралось.
//
// Группа с неразобранным правилом не применяется ни в ту, ни в другую
// сторону, и это выбор, а не умолчание. Применить её «как открывающую»
// значило бы раздать по правилу, которого мы не поняли; применить «как
// скрывающую» — отнять по нему же. Набор остаётся при своей линейке —
// состоянии, которое кто-то назвал вслух, — а студия показывает поломку
// красной строкой, чтобы она не стояла молча.
func (s *Store) live(ctx context.Context) ([]Group, error) {
	rows, err := s.gate.Query(ctx, `SELECT id, rule FROM audiences`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Group{}
	for rows.Next() {
		var one Group
		var raw []byte
		if err := rows.Scan(&one.ID, &raw); err != nil {
			return nil, err
		}
		rule, err := ParseRule(raw)
		if err != nil {
			continue
		}
		one.Rule = rule
		out = append(out, one)
	}
	return out, rows.Err()
}

// named — группы, где врач назван поимённо.
func (s *Store) named(ctx context.Context, accountID int64) (map[int64]bool, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT audience_id FROM audience_members WHERE account_id = $1`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// Bindings — все связи наборов с группами.
//
// Целиком, а не по одному набору: таблица эта мала по устройству (строка
// на пару «набор — группа»), а корпусу нужны сразу все, и второй запрос
// на каждый набор превратил бы одно обращение в сотню.
func (s *Store) Bindings(ctx context.Context) ([]Binding, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT pack_id, audience_id, mode FROM pack_audiences`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Binding{}
	for rows.Next() {
		var one Binding
		if err := rows.Scan(&one.PackID, &one.AudienceID, &one.Mode); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

// Verdict — что группы говорят об одном наборе.
type Verdict struct {
	// Granted — набор открыт группе, в которую врач попадает.
	Granted bool

	// Hidden — набор скрыт от группы, в которую врач попадает.
	Hidden bool
}

// Verdicts сводит принадлежность и связи в ответ по каждому набору.
//
// Пересечение групп неизбежно — врач бывает и в «кафедре», и в
// «уснувших», — и при споре выигрывает скрывающая. Довод тот же, по
// которому линейка набора умалчивается платной: набор, закрытый по
// ошибке, виден сразу — врач его не получит и скажет; открытый по ошибке
// не виден никому, и узнают о нём по непришедшим деньгам.
func Verdicts(in []int64, bindings []Binding) map[int64]Verdict {
	mine := map[int64]bool{}
	for _, id := range in {
		mine[id] = true
	}
	out := map[int64]Verdict{}
	for _, b := range bindings {
		if !mine[b.AudienceID] {
			continue
		}
		v := out[b.PackID]
		switch b.Mode {
		case ModeOpen:
			v.Granted = true
		case ModeHidden:
			v.Hidden = true
		default:
			// Непонятая связь не применяется: молчаливое «считаем
			// открывающей» раздало бы набор по строке, которой никто не
			// писал.
			continue
		}
		out[b.PackID] = v
	}
	return out
}

// Size считает, сколько врачей попадает в группу сейчас.
//
// # Почему это отдельная ручка, а не поле списка
//
// Счёт проходит по всем учётным записям, а у группы с поведенческим
// признаком — ещё и по всем попыткам. Считай его список групп, открытие
// раздела «Группы» стоило бы стольких проходов, сколько групп заведено,
// и дорожало бы ровно от того, что групп становится больше. Поэтому
// счёт спрашивается по одной группе и тогда, когда на неё смотрят.
//
// Поимённо названные входят в счёт, даже если не подходят по правилу: в
// группе они состоят.
func (s *Store) Size(ctx context.Context, slug string) (int, error) {
	one, err := s.One(ctx, slug)
	if err != nil {
		return 0, err
	}
	if one.Broken != "" {
		return 0, errors.New("правило не разобрано, и считать по нему нечего: " + one.Broken)
	}

	named, err := s.namedIn(ctx, one.ID)
	if err != nil {
		return 0, err
	}
	portraits, err := s.portraits(ctx, time.Now(), one.Rule.NeedsBehaviour())
	if err != nil {
		return 0, err
	}
	count := 0
	for id, p := range portraits {
		if named[id] || one.Rule.Matches(p) {
			count++
		}
	}
	// Названный поимённо, чья запись исчезла из портретов, сюда не
	// попадёт — и не должен: ссылка держится ключом, и записи без
	// портрета не бывает.
	return count, nil
}

// Of отдаёт группы, в которых состоит врач, — для его карточки в студии.
func (s *Store) Of(ctx context.Context, accountID int64, now time.Time) ([]Group, error) {
	in, err := s.Membership(ctx, accountID, now)
	if err != nil {
		return nil, err
	}
	mine := map[int64]bool{}
	for _, id := range in {
		mine[id] = true
	}
	all, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	out := []Group{}
	for _, one := range all {
		if mine[one.ID] {
			out = append(out, one)
		}
	}
	return out, nil
}

// namedIn — кто назван в группе поимённо.
func (s *Store) namedIn(ctx context.Context, audienceID int64) (map[int64]bool, error) {
	rows, err := s.gate.Query(ctx,
		`SELECT account_id FROM audience_members WHERE audience_id = $1`, audienceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// portraits собирает портреты всех врачей разом.
//
// Разом, а не по одному: счёт по группе иначе стал бы запросом на каждую
// учётную запись. Поведенческая половина приходит отдельным запросом и
// только когда о ней спрашивают — она одна и читает попытки.
func (s *Store) portraits(ctx context.Context, now time.Time, behaviour bool) (map[int64]Portrait, error) {
	rows, err := s.gate.Query(ctx, `
		SELECT a.id, a.email IS NOT NULL,
		       EXISTS (SELECT 1 FROM entitlements e
		                WHERE e.account_id = a.id AND e.kind = 'subscription'
		                  AND e.revoked_at IS NULL AND e.starts_at <= $1
		                  AND (e.expires_at IS NULL OR e.expires_at > $1)),
		       EXISTS (SELECT 1 FROM entitlements e
		                WHERE e.account_id = a.id
		                  AND e.origin IN ('purchase', 'subscription')),
		       GREATEST(0, (EXTRACT(EPOCH FROM ($1::timestamptz - a.created_at)) / 86400)::int),
		       GREATEST(0, (EXTRACT(EPOCH FROM ($1::timestamptz - coalesce(a.last_seen, a.created_at))) / 86400)::int)
		  FROM accounts a`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]Portrait{}
	for rows.Next() {
		var id int64
		var p Portrait
		if err := rows.Scan(&id, &p.EmailBound, &p.Subscribed, &p.EverPaid,
			&p.AgeDays, &p.IdleDays); err != nil {
			return nil, err
		}
		p.Solved = map[string]int{}
		p.Correct = map[string]int{}
		p.ActiveDays = map[string]int{}
		out[id] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !behaviour {
		return out, nil
	}

	deeds, err := s.gate.Query(ctx, `
		SELECT account_id, count(*),
		       count(*) FILTER (WHERE happened_at > $1::timestamptz - make_interval(days => $2::int)),
		       count(*) FILTER (WHERE happened_at > $1::timestamptz - make_interval(days => $3::int)),
		       count(*) FILTER (WHERE happened_at > $1::timestamptz - make_interval(days => $4::int)),
		       count(*) FILTER (WHERE correct),
		       count(*) FILTER (WHERE correct AND happened_at > $1::timestamptz - make_interval(days => $2::int)),
		       count(*) FILTER (WHERE correct AND happened_at > $1::timestamptz - make_interval(days => $3::int)),
		       count(*) FILTER (WHERE correct AND happened_at > $1::timestamptz - make_interval(days => $4::int)),
		       count(DISTINCT (happened_at AT TIME ZONE 'UTC')::date),
		       count(DISTINCT (happened_at AT TIME ZONE 'UTC')::date)
		           FILTER (WHERE happened_at > $1::timestamptz - make_interval(days => $2::int)),
		       count(DISTINCT (happened_at AT TIME ZONE 'UTC')::date)
		           FILTER (WHERE happened_at > $1::timestamptz - make_interval(days => $3::int)),
		       count(DISTINCT (happened_at AT TIME ZONE 'UTC')::date)
		           FILTER (WHERE happened_at > $1::timestamptz - make_interval(days => $4::int))
		  FROM attempts GROUP BY account_id`,
		now, WindowDays[Window7], WindowDays[Window30], WindowDays[Window90])
	if err != nil {
		return nil, err
	}
	defer deeds.Close()

	for deeds.Next() {
		var id int64
		var s0, s7, s30, s90, c0, c7, c30, c90, d0, d7, d30, d90 int
		if err := deeds.Scan(&id, &s0, &s7, &s30, &s90,
			&c0, &c7, &c30, &c90, &d0, &d7, &d30, &d90); err != nil {
			return nil, err
		}
		p, ok := out[id]
		if !ok {
			// Попытки без учётной записи — не наш случай: ключ этого не
			// допускает. Пропускаем, а не заводим портрет из ничего.
			continue
		}
		p.Solved = map[string]int{WindowAll: s0, Window7: s7, Window30: s30, Window90: s90}
		p.Correct = map[string]int{WindowAll: c0, Window7: c7, Window30: c30, Window90: c90}
		p.ActiveDays = map[string]int{WindowAll: d0, Window7: d7, Window30: d30, Window90: d90}
		out[id] = p
	}
	return out, deeds.Err()
}
