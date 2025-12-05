package bill

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

func workflowIDForBill(billID string) string {
	return "bill-" + billID
}

// ==============================
// Response DTO shapes
// ==============================

type MoneyDTO struct {
	AmountMinor int64    `json:"amount_minor"`
	Currency    Currency `json:"currency"`
}

type BillDTO struct {
	ID        string     `json:"id"`
	Status    BillStatus `json:"status"`
	Currency  Currency   `json:"currency"`
	Total     MoneyDTO   `json:"total"`
	CreatedAt string     `json:"created_at"`
	ClosedAt  *string    `json:"closed_at,omitempty"`
}

type LineItemDTO struct {
	ID          string `json:"id"`
	BillID      string `json:"bill_id"`
	Description string `json:"description"`
	AmountMinor int64  `json:"amount_minor"`
	CreatedAt   string `json:"created_at"`
}

// ==============================
// DTO mappers
// ==============================

func billToDTO(b *Bill) BillDTO {
	var closedAtStr *string
	if b.ClosedAt != nil {
		s := b.ClosedAt.UTC().Format(time.RFC3339Nano)
		closedAtStr = &s
	}

	return BillDTO{
		ID:       b.ID,
		Status:   b.Status,
		Currency: b.Currency,
		Total: MoneyDTO{
			AmountMinor: b.TotalMinor,
			Currency:    b.Currency,
		},
		CreatedAt: b.CreatedAt.UTC().Format(time.RFC3339Nano),
		ClosedAt:  closedAtStr,
	}
}

func lineItemsToDTOs(items []*LineItem) []LineItemDTO {
	if len(items) == 0 {
		return []LineItemDTO{}
	}
	out := make([]LineItemDTO, 0, len(items))
	for _, li := range items {
		out = append(out, LineItemDTO{
			ID:          li.ID,
			BillID:      li.BillID,
			Description: li.Description,
			AmountMinor: li.AmountMinor,
			CreatedAt:   li.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return out
}

// ==============================
// Join-based store function
// ==============================

func listBillsWithItemsJoin(ctx context.Context, status *BillStatus) ([]*Bill, map[string][]*LineItem, error) {
	var (
		rows *sqldb.Rows
		err  error
	)

	if status == nil {
		rows, err = db.Query(ctx, `
			SELECT
				b.id, b.status, b.currency, b.total_minor, b.created_at, b.closed_at,
				li.id, li.bill_id, li.description, li.amount_minor, li.created_at
			FROM bills b
			LEFT JOIN bill_line_items li ON li.bill_id = b.id
			ORDER BY b.created_at DESC, li.created_at ASC
		`)
	} else {
		rows, err = db.Query(ctx, `
			SELECT
				b.id, b.status, b.currency, b.total_minor, b.created_at, b.closed_at,
				li.id, li.bill_id, li.description, li.amount_minor, li.created_at
			FROM bills b
			LEFT JOIN bill_line_items li ON li.bill_id = b.id
			WHERE b.status = $1
			ORDER BY b.created_at DESC, li.created_at ASC
		`, *status)
	}

	if err != nil {
		return nil, nil, errs.B().Code(errs.Internal).Msg("list bills join").Err()
	}
	defer rows.Close()

	billsByID := make(map[string]*Bill)
	itemsByBill := make(map[string][]*LineItem)

	for rows.Next() {
		// Bill columns
		var (
			bID        string
			bStatus    string
			bCurrency  string
			bTotal     int64
			bCreatedAt time.Time
			bClosedAt  sql.NullTime
		)

		// Line item columns (nullable because LEFT JOIN)
		var (
			liID        sql.NullString
			liBillID    sql.NullString
			liDesc      sql.NullString
			liAmount    sql.NullInt64
			liCreatedAt sql.NullTime
		)

		if err := rows.Scan(
			&bID, &bStatus, &bCurrency, &bTotal, &bCreatedAt, &bClosedAt,
			&liID, &liBillID, &liDesc, &liAmount, &liCreatedAt,
		); err != nil {
			return nil, nil, errs.B().Code(errs.Internal).Msg("scan bills join").Err()
		}

		// Create bill once
		b, ok := billsByID[bID]
		if !ok {
			b = &Bill{
				ID:         bID,
				Status:     BillStatus(bStatus),
				Currency:   Currency(bCurrency),
				TotalMinor: bTotal,
				CreatedAt:  bCreatedAt,
			}
			if bClosedAt.Valid {
				b.ClosedAt = &bClosedAt.Time
			}
			billsByID[bID] = b
		}

		// Add line item if present
		if liID.Valid {
			li := &LineItem{
				ID:          liID.String,
				BillID:      bID,
				Description: liDesc.String,
				AmountMinor: liAmount.Int64,
				CreatedAt:   liCreatedAt.Time,
			}
			itemsByBill[bID] = append(itemsByBill[bID], li)
		}
	}

	// Convert map to slice and preserve ordering by created_at desc
	bills := make([]*Bill, 0, len(billsByID))
	for _, b := range billsByID {
		bills = append(bills, b)
	}
	sort.Slice(bills, func(i, j int) bool {
		return bills[i].CreatedAt.After(bills[j].CreatedAt)
	})

	return bills, itemsByBill, nil
}

// One join for a single bill
func getBillWithItemsJoin(ctx context.Context, billID string) (*Bill, []*LineItem, error) {
	rows, err := db.Query(ctx, `
		SELECT
			b.id, b.status, b.currency, b.total_minor, b.created_at, b.closed_at,
			li.id, li.bill_id, li.description, li.amount_minor, li.created_at
		FROM bills b
		LEFT JOIN bill_line_items li ON li.bill_id = b.id
		WHERE b.id = $1
		ORDER BY li.created_at ASC
	`, billID)
	if err != nil {
		return nil, nil, errs.B().Code(errs.Internal).Msg("get bill join").Err()
	}
	defer rows.Close()

	var (
		bill  *Bill
		items []*LineItem
	)

	for rows.Next() {
		// Bill columns
		var (
			bID        string
			bStatus    string
			bCurrency  string
			bTotal     int64
			bCreatedAt time.Time
			bClosedAt  sql.NullTime
		)

		// Line item columns (nullable)
		var (
			liID        sql.NullString
			liBillID    sql.NullString
			liDesc      sql.NullString
			liAmount    sql.NullInt64
			liCreatedAt sql.NullTime
		)

		if err := rows.Scan(
			&bID, &bStatus, &bCurrency, &bTotal, &bCreatedAt, &bClosedAt,
			&liID, &liBillID, &liDesc, &liAmount, &liCreatedAt,
		); err != nil {
			return nil, nil, errs.B().Code(errs.Internal).Msg("scan bill join").Err()
		}

		// Create bill once
		if bill == nil {
			bill = &Bill{
				ID:         bID,
				Status:     BillStatus(bStatus),
				Currency:   Currency(bCurrency),
				TotalMinor: bTotal,
				CreatedAt:  bCreatedAt,
			}
			if bClosedAt.Valid {
				bill.ClosedAt = &bClosedAt.Time
			}
		}

		// Add line item if present
		if liID.Valid {
			items = append(items, &LineItem{
				ID:          liID.String,
				BillID:      bID,
				Description: liDesc.String,
				AmountMinor: liAmount.Int64,
				CreatedAt:   liCreatedAt.Time,
			})
		}
	}

	if bill == nil {
		return nil, nil, errs.B().Code(errs.NotFound).Msg("bill not found").Err()
	}

	return bill, items, nil
}

func getBillStatusAndCurrency(ctx context.Context, billID string) (BillStatus, Currency, error) {
	row := db.QueryRow(ctx, `
		SELECT status, currency
		FROM bills
		WHERE id = $1
	`, billID)

	var status string
	var currency string
	if err := row.Scan(&status, &currency); err != nil {
		return "", "", errs.B().Code(errs.NotFound).Msg("bill not found").Err()
	}

	return BillStatus(status), Currency(currency), nil
}
