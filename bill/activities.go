package bill

import (
	"context"
	"database/sql"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

type CreateBillRowInput struct {
	BillID   string
	Currency Currency
}

// CreateBillRowActivity inserts the bill row.
// Idempotent by primary key.
func CreateBillRowActivity(ctx context.Context, in CreateBillRowInput) (*Bill, error) {
	if !in.Currency.Valid() {
		return nil, errs.B().Code(errs.InvalidArgument).Msg("unsupported currency").Err()
	}

	_, err := db.Exec(ctx, `
		INSERT INTO bills (id, status, currency, total_minor)
		VALUES ($1, $2, $3, 0)
		ON CONFLICT (id) DO NOTHING
	`, in.BillID, string(StatusOpen), string(in.Currency))
	if err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("insert bill").Err()
	}

	row := db.QueryRow(ctx, `
		SELECT id, status, currency, total_minor, created_at, closed_at
		FROM bills WHERE id = $1
	`, in.BillID)

	var b Bill
	var closed sql.NullTime
	if err := row.Scan(&b.ID, &b.Status, &b.Currency, &b.TotalMinor, &b.CreatedAt, &closed); err != nil {
		if err == sqldb.ErrNoRows {
			return nil, errs.B().Code(errs.NotFound).Msg("bill not found after insert").Err()
		}
		return nil, errs.B().Code(errs.Internal).Msg("read bill").Err()
	}
	if closed.Valid {
		b.ClosedAt = &closed.Time
	}
	return &b, nil
}

type AddLineItemInput struct {
	LineItemID  string
	BillID      string
	Description string
	AmountMinor int64
	Currency    Currency
}

// AddLineItemActivity inserts a line item.
// Idempotent by primary key.
func AddLineItemActivity(ctx context.Context, in AddLineItemInput) (*LineItem, error) {
	if in.AmountMinor <= 0 {
		return nil, errs.B().Code(errs.InvalidArgument).Msg("amount must be positive").Err()
	}

	row := db.QueryRow(ctx, `
		SELECT status, currency FROM bills WHERE id = $1
	`, in.BillID)

	var status string
	var currency string
	if err := row.Scan(&status, &currency); err != nil {
		return nil, errs.B().Code(errs.NotFound).Msg("bill not found").Err()
	}
	if BillStatus(status) != StatusOpen {
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("bill is closed").Err()
	}
	if Currency(currency) != in.Currency {
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("currency mismatch").Err()
	}

	_, err := db.Exec(ctx, `
		INSERT INTO bill_line_items (id, bill_id, description, amount_minor)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING
	`, in.LineItemID, in.BillID, in.Description, in.AmountMinor)
	if err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("insert line item").Err()
	}

	liRow := db.QueryRow(ctx, `
		SELECT id, bill_id, description, amount_minor, created_at
		FROM bill_line_items WHERE id = $1
	`, in.LineItemID)

	var li LineItem
	if err := liRow.Scan(&li.ID, &li.BillID, &li.Description, &li.AmountMinor, &li.CreatedAt); err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("read line item").Err()
	}

	return &li, nil
}

type CloseBillInput struct {
	BillID     string
	TotalMinor int64
}

// CloseBillActivity marks bill closed with final total.
func CloseBillActivity(ctx context.Context, in CloseBillInput) (*Bill, error) {
	row := db.QueryRow(ctx, `
		UPDATE bills
		SET status = $2, total_minor = $3, closed_at = now()
		WHERE id = $1 AND status = 'OPEN'
		RETURNING id, status, currency, total_minor, created_at, closed_at
	`, in.BillID, string(StatusClosed), in.TotalMinor)

	var b Bill
	var closed sql.NullTime
	if err := row.Scan(&b.ID, &b.Status, &b.Currency, &b.TotalMinor, &b.CreatedAt, &closed); err != nil {
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("bill already closed or not found").Err()
	}
	if closed.Valid {
		b.ClosedAt = &closed.Time
	}
	return &b, nil
}
