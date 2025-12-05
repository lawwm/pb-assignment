package bill

import "time"

type Currency string

const (
	CurrencyUSD Currency = "USD"
	CurrencyGEL Currency = "GEL"
)

func (c Currency) Valid() bool {
	return c == CurrencyUSD || c == CurrencyGEL
}

type BillStatus string

const (
	StatusOpen   BillStatus = "OPEN"
	StatusClosed BillStatus = "CLOSED"
)

type Bill struct {
	ID         string
	Status     BillStatus
	Currency   Currency
	TotalMinor int64
	CreatedAt  time.Time
	ClosedAt   *time.Time
}

type LineItem struct {
	ID          string
	BillID      string
	Description string
	AmountMinor int64
	CreatedAt   time.Time
}
