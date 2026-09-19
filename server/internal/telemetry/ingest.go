package telemetry

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"curator/server/internal/dbgate"
)

// Store — события в базе.
type Store struct {
	gate *dbgate.Gate
}

func NewStore(gate *dbgate.Gate) *Store { return &Store{gate: gate} }

// Incoming — событие, как его присылает устройство.
type Incoming struct {
	Name       string         `json:"name"`
	Props      map[string]any `json:"props"`
	SessionID  string         `json:"sessionId"`
	AppVersion string         `json:"appVersion"`

	// IdemKey — ключ повторности, выданный устройством. Приложение шлёт
	// события пачкой и повторяет посылку при обрыве, как и попытки.
	IdemKey string `json:"idemKey"`

	// HappenedAt — когда это было на устройстве, а не когда доехало.
	HappenedAt time.Time `json:"happenedAt"`
}

// Taken — что стало с пачкой.
type Taken struct {
	Accepted int
	Repeated int

	// Dropped — событий, которых нет в словаре. Число возвращается
	// намеренно: молча отброшенная половина пачки выглядит в отчёте как
	// «врач ничего не делал», и объяснить это будет нечем. Приложение
	// пишет его в свой журнал, а студия видит по счёту отброшенных, что
	// её словарь отстал от сборки.
	Dropped int
}

// Record принимает пачку событий.
//
// # Неизвестное отбрасывается, пачка принимается
//
// Старый сервер обязан принять посылку новой сборки. Откажи он всей пачке
// из-за одного незнакомого имени — и выкатка приложения потребовала бы
// выкатки сервера в ту же минуту, а на руках у врачей стоят сборки, которые
// обновятся не завтра.
//
// Обратное — записать незнакомое «на всякий случай» — хуже: словарь
// перестаёт быть словарём, имена расходятся по сборкам, и отчёт строить не
// по чему.
func (s *Store) Record(ctx context.Context, accountID int64, batch []Incoming, now time.Time) (Taken, error) {
	var out Taken
	err := s.gate.InTx(ctx, func(tx pgx.Tx) error {
		for _, one := range batch {
			event, known := Known(one.Name)
			if !known {
				out.Dropped++
				continue
			}
			happened := one.HappenedAt
			if happened.IsZero() {
				happened = now
			}
			props, err := json.Marshal(clean(event, one.Props))
			if err != nil {
				return err
			}

			var id int64
			err = tx.QueryRow(ctx, `
				INSERT INTO telemetry_events
				       (account_id, session_id, name, props, app_version, idem_key, happened_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (account_id, idem_key) WHERE idem_key <> '' DO NOTHING
				RETURNING id`,
				accountID, trim(one.SessionID, 64), event.Name, props,
				trim(one.AppVersion, 32), one.IdemKey, happened).Scan(&id)
			switch err {
			case nil:
				out.Accepted++
			case pgx.ErrNoRows:
				// Это событие уже лежит: повтор посылки — доставка, а не
				// работа врача. Второй раз оно удвоило бы каждый отчёт.
				out.Repeated++
			default:
				return err
			}
		}
		return nil
	})
	return out, err
}

// clean оставляет только разрешённые свойства.
//
// Свойство, приехавшее по месту, в базе выглядит данными: оно есть, его
// видно в строке, и первый же, кто его увидит, построит по нему отчёт — а
// шлёт его одна сборка из пяти.
func clean(event Event, props map[string]any) map[string]any {
	// Пустой набор свойств — {}, а не null: колонка объявлена не-null, и
	// событие без свойств — исправный случай, а не отсутствие данных.
	out := map[string]any{}
	for key, value := range props {
		if event.Allowed(key) {
			out[key] = value
		}
	}
	return out
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
