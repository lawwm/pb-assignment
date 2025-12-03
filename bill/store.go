package bill

import (
	"context"
	"database/sql"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

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

// --- Bills ---

func createBill(ctx context.Context, currency Currency) (*Bill, error) {
	if !currency.Valid() {
		return nil, errs.B().Code(errs.InvalidArgument).Msg("unsupported currency").Err()
	}

	id := uuid.New().String()

	row := db.QueryRow(ctx, `
        INSERT INTO bills (id, status, currency, total_minor)
        VALUES ($1, $2, $3, 0)
        RETURNING id, status, currency, total_minor, created_at, closed_at
    `, id, string(StatusOpen), string(currency))

	var b Bill
	var closedAt sql.NullTime
	if err := row.Scan(&b.ID, &b.Status, &b.Currency, &b.TotalMinor, &b.CreatedAt, &closedAt); err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("insert bill").Err()
	}
	if closedAt.Valid {
		b.ClosedAt = &closedAt.Time
	}

	return &b, nil
}

func getBill(ctx context.Context, id string) (*Bill, error) {
	row := db.QueryRow(ctx, `
        SELECT id, status, currency, total_minor, created_at, closed_at
        FROM bills
        WHERE id = $1
    `, id)

	var b Bill
	var closedAt sql.NullTime
	if err := row.Scan(&b.ID, &b.Status, &b.Currency, &b.TotalMinor, &b.CreatedAt, &closedAt); err != nil {
		if err == sqldb.ErrNoRows {
			return nil, errs.B().Code(errs.NotFound).Msg("bill not found").Err()
		}
		return nil, errs.B().Code(errs.Internal).Msg("query bill").Err()
	}
	if closedAt.Valid {
		b.ClosedAt = &closedAt.Time
	}

	return &b, nil
}

func listBills(ctx context.Context, status *BillStatus) ([]*Bill, error) {
	var (
		rows *sqldb.Rows
		err  error
	)

	if status == nil {
		rows, err = db.Query(ctx, `
            SELECT id, status, currency, total_minor, created_at, closed_at
            FROM bills
            ORDER BY created_at DESC
        `)
	} else {
		rows, err = db.Query(ctx, `
            SELECT id, status, currency, total_minor, created_at, closed_at
            FROM bills
            WHERE status = $1
            ORDER BY created_at DESC
        `, *status)
	}

	if err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("list bills").Err()
	}
	defer rows.Close()

	var out []*Bill
	for rows.Next() {
		var b Bill
		var closedAt sql.NullTime
		if err := rows.Scan(&b.ID, &b.Status, &b.Currency, &b.TotalMinor, &b.CreatedAt, &closedAt); err != nil {
			return nil, errs.B().Code(errs.Internal).Msg("scan bill").Err()
		}
		if closedAt.Valid {
			b.ClosedAt = &closedAt.Time
		}
		out = append(out, &b)
	}
	return out, nil
}

func closeBill(ctx context.Context, bill *Bill, totalMinor int64) (*Bill, error) {
	if bill.Status != StatusOpen {
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("bill already closed").Err()
	}

	now := time.Now()

	row := db.QueryRow(ctx, `
        UPDATE bills
        SET status = $2, total_minor = $3, closed_at = $4
        WHERE id = $1
        RETURNING id, status, currency, total_minor, created_at, closed_at
    `, bill.ID, string(StatusClosed), totalMinor, now)

	var b Bill
	var closedAt sql.NullTime
	if err := row.Scan(&b.ID, &b.Status, &b.Currency, &b.TotalMinor, &b.CreatedAt, &closedAt); err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("update bill").Err()
	}
	if closedAt.Valid {
		b.ClosedAt = &closedAt.Time
	}

	return &b, nil
}

// --- Line items ---

func insertLineItem(ctx context.Context, bill *Bill, description string, amountMinor int64) (*LineItem, error) {
	if bill.Status != StatusOpen {
		return nil, errs.B().Code(errs.FailedPrecondition).
			Msg("cannot add line item to closed bill").Err()
	}
	if amountMinor <= 0 {
		return nil, errs.B().Code(errs.InvalidArgument).
			Msg("line item amount must be positive").Err()
	}

	id := uuid.New().String()

	row := db.QueryRow(ctx, `
        INSERT INTO bill_line_items (id, bill_id, description, amount_minor)
        VALUES ($1, $2, $3, $4)
        RETURNING id, bill_id, description, amount_minor, created_at
    `, id, bill.ID, description, amountMinor)

	var li LineItem
	if err := row.Scan(&li.ID, &li.BillID, &li.Description, &li.AmountMinor, &li.CreatedAt); err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("insert line item").Err()
	}

	return &li, nil
}

func listLineItems(ctx context.Context, billID string) ([]*LineItem, error) {
	rows, err := db.Query(ctx, `
        SELECT id, bill_id, description, amount_minor, created_at
        FROM bill_line_items
        WHERE bill_id = $1
        ORDER BY created_at ASC
    `, billID)
	if err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("list line items").Err()
	}
	defer rows.Close()

	var out []*LineItem
	for rows.Next() {
		var li LineItem
		if err := rows.Scan(&li.ID, &li.BillID, &li.Description, &li.AmountMinor, &li.CreatedAt); err != nil {
			return nil, errs.B().Code(errs.Internal).Msg("scan line item").Err()
		}
		out = append(out, &li)
	}
	return out, nil
}
