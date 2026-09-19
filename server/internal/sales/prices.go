package sales

import (
	"context"
	"errors"
	"time"

	"curator/server/internal/dbgate"
)

// Цены.
//
// # В копейках, и только в копейках
//
// Деньги не хранятся дробным числом никогда: рубль с копейками, сложенный
// тысячу раз в двоичной дроби, расходится с любым счётом на копейки, и
// объяснять это будет тот, кто сводил отчёт.
//
// На витрине рубли, в базе копейки, и перевод делается при показе. Это же
// правило держит и вторую границу: расход на модели считается в
// нанодолларах, и складывать его с рублями нельзя ни в одной сумме.
type Prices struct {
	gate *dbgate.Gate
}

func NewPrices(gate *dbgate.Gate) *Prices { return &Prices{gate: gate} }

// Price — цена товара.
type Price struct {
	Purpose string
	Kopecks int64
	Enabled bool
	At      time.Time
}

// Set задаёт цену.
func (p *Prices) Set(ctx context.Context, purpose string, kopecks int64, enabled bool) error {
	known, err := ParsePurpose(purpose)
	if err != nil {
		return err
	}
	if kopecks <= 0 {
		// Цена в ноль — не бесплатный товар, а описка. Бесплатный товар
		// делается отсутствием цены либо выключением её, и это видно в
		// студии; ноль же выглядит ценой и таковой не является.
		return errors.New("цена должна быть больше нуля; " +
			"бесплатный набор делается выключением цены, а не нулём")
	}
	_, err = p.gate.Exec(ctx, `
		INSERT INTO prices (purpose, amount_kopecks, enabled)
		VALUES ($1, $2, $3)
		ON CONFLICT (purpose) DO UPDATE
		   SET amount_kopecks = $2, enabled = $3, updated_at = NOW()`,
		string(known), kopecks, enabled)
	return err
}

// All отдаёт все цены.
func (p *Prices) All(ctx context.Context) ([]Price, error) {
	rows, err := p.gate.Query(ctx,
		`SELECT purpose, amount_kopecks, enabled, updated_at FROM prices ORDER BY purpose`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Price{}
	for rows.Next() {
		var one Price
		if err := rows.Scan(&one.Purpose, &one.Kopecks, &one.Enabled, &one.At); err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, rows.Err()
}

// Live отдаёт действующие цены картой: назначение → копейки.
//
// Только включённые: выключенная цена — это «пока не продаём», и показывать
// её врачу значит обещать товар, который он не сможет купить.
func (p *Prices) Live(ctx context.Context) (map[string]int64, error) {
	rows, err := p.gate.Query(ctx,
		`SELECT purpose, amount_kopecks FROM prices WHERE enabled AND amount_kopecks > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int64{}
	for rows.Next() {
		var purpose string
		var kopecks int64
		if err := rows.Scan(&purpose, &kopecks); err != nil {
			return nil, err
		}
		out[purpose] = kopecks
	}
	return out, rows.Err()
}
