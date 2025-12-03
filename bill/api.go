package bill

import (
	"context"
	"time"

	"encore.dev/beta/errs"
	"go.temporal.io/sdk/client"
)

// --- DTOs ---

type MoneyDTO struct {
	AmountMinor int64    `json:"amount_minor"`
	Currency    Currency `json:"currency"`
}

type LineItemDTO struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	AmountMinor int64  `json:"amount_minor"`
	CreatedAt   string `json:"created_at"`
}

type BillDTO struct {
	ID        string        `json:"id"`
	Status    BillStatus    `json:"status"`
	Currency  Currency      `json:"currency"`
	Total     MoneyDTO      `json:"total"`
	CreatedAt string        `json:"created_at"`
	ClosedAt  *string       `json:"closed_at,omitempty"`
	Items     []LineItemDTO `json:"items,omitempty"`
}

// --- Create bill ---

type CreateBillRequest struct {
	Currency Currency `json:"currency"`
}

type CreateBillResponse struct {
	Bill BillDTO `json:"bill"`
}

//encore:api public method=POST path=/bills
func (s *Service) CreateBill(ctx context.Context, req *CreateBillRequest) (*CreateBillResponse, error) {
	if !req.Currency.Valid() {
		return nil, errs.B().Code(errs.InvalidArgument).Msg("unsupported currency").Err()
	}

	b, err := createBill(ctx, req.Currency)
	if err != nil {
		return nil, err
	}

	// Start workflow for this bill
	_, err = s.temporalClient.ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:        workflowIDForBill(b.ID),
			TaskQueue: taskQueueName,
		},
		BillWorkflow,
		BillWorkflowParams{
			BillID:   b.ID,
			Currency: string(b.Currency),
		},
	)
	if err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("start bill workflow").Err()
	}

	return &CreateBillResponse{
		Bill: billToDTO(b, nil),
	}, nil
}

// --- Add line item ---

type AddLineItemRequest struct {
	Description string   `json:"description"`
	AmountMinor int64    `json:"amount_minor"`
	Currency    Currency `json:"currency"`
}

type AddLineItemResponse struct {
	Bill     BillDTO     `json:"bill"`
	LineItem LineItemDTO `json:"line_item"`
}

//encore:api public method=POST path=/bills/:id/line-items
func (s *Service) AddLineItem(ctx context.Context, id string, req *AddLineItemRequest) (*AddLineItemResponse, error) {
	b, err := getBill(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.Status != StatusOpen {
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("cannot add to closed bill").Err()
	}
	if req.Currency != b.Currency {
		return nil, errs.B().Code(errs.FailedPrecondition).
			Msg("line item currency must match bill currency").Err()
	}

	li, err := insertLineItem(ctx, b, req.Description, req.AmountMinor)
	if err != nil {
		return nil, err
	}

	// Signal workflow
	sig := AddLineItemSignal{
		LineItemID:  li.ID,
		AmountMinor: li.AmountMinor,
		Description: li.Description,
		Currency:    string(req.Currency),
	}
	if err := s.temporalClient.SignalWorkflow(ctx, workflowIDForBill(b.ID), "", signalAddLineItem, sig); err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("signal add-line-item").Err()
	}

	items, err := listLineItems(ctx, b.ID)
	if err != nil {
		return nil, err
	}

	return &AddLineItemResponse{
		Bill:     billToDTO(b, items),
		LineItem: lineItemToDTO(li),
	}, nil
}

// --- Close bill ---

type CloseBillResponse struct {
	Bill BillDTO `json:"bill"`
}

//encore:api public method=POST path=/bills/:id/close
func (s *Service) CloseBill(ctx context.Context, id string) (*CloseBillResponse, error) {
	b, err := getBill(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.Status != StatusOpen {
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("bill already closed").Err()
	}

	// Signal workflow to close
	if err := s.temporalClient.SignalWorkflow(ctx, workflowIDForBill(b.ID), "", signalCloseBill, CloseBillSignal{}); err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("signal close-bill").Err()
	}

	// Wait for workflow result
	run := s.temporalClient.GetWorkflow(ctx, workflowIDForBill(b.ID), "")
	var result BillResult
	if err := run.Get(ctx, &result); err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("get workflow result").Err()
	}

	// Persist final total
	updated, err := closeBill(ctx, b, result.TotalMinor)
	if err != nil {
		return nil, err
	}

	items, err := listLineItems(ctx, b.ID)
	if err != nil {
		return nil, err
	}

	return &CloseBillResponse{
		Bill: billToDTO(updated, items),
	}, nil
}

// --- Get single bill ---

type GetBillResponse struct {
	Bill BillDTO `json:"bill"`
}

//encore:api public method=GET path=/bills/:id
func (s *Service) GetBill(ctx context.Context, id string) (*GetBillResponse, error) {
	b, err := getBill(ctx, id)
	if err != nil {
		return nil, err
	}
	items, err := listLineItems(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	return &GetBillResponse{Bill: billToDTO(b, items)}, nil
}

// --- List bills ---

type ListBillsRequest struct {
    Status string `query:"status"` // ?status=open or ?status=closed
}

type ListBillsResponse struct {
	Bills []BillDTO `json:"bills"`
}

//encore:api public method=GET path=/bills
func (s *Service) ListBills(ctx context.Context, req *ListBillsRequest) (*ListBillsResponse, error) {
    var st *BillStatus

    if req.Status != "" {
        tmp := BillStatus(req.Status)
        switch tmp {
        case StatusOpen, StatusClosed:
            st = &tmp
        default:
            return nil, errs.B().Code(errs.InvalidArgument).Msg("invalid status").Err()
        }
    }

    bills, err := listBills(ctx, st)
    if err != nil {
        return nil, err
    }

    out := make([]BillDTO, 0, len(bills))
    for _, b := range bills {
        out = append(out, billToDTO(b, nil))
    }
    return &ListBillsResponse{Bills: out}, nil
}



// --- Helpers ---

func workflowIDForBill(billID string) string {
	return "bill-" + billID
}

func billToDTO(b *Bill, items []*LineItem) BillDTO {
	var closedAtStr *string
	if b.ClosedAt != nil {
		s := b.ClosedAt.UTC().Format(time.RFC3339Nano)
		closedAtStr = &s
	}
	dto := BillDTO{
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
	if items != nil {
		dto.Items = make([]LineItemDTO, 0, len(items))
		for _, li := range items {
			dto.Items = append(dto.Items, lineItemToDTO(li))
		}
	}
	return dto
}

func lineItemToDTO(li *LineItem) LineItemDTO {
	return LineItemDTO{
		ID:          li.ID,
		Description: li.Description,
		AmountMinor: li.AmountMinor,
		CreatedAt:   li.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
