package bill

import (
	"context"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
)

type CreateBillRequest struct {
	Currency Currency `json:"currency"`
}

type CreateBillResponse struct {
	BillID string `json:"bill_id"`
}

//encore:api public method=POST path=/bills
func (s *Service) CreateBill(ctx context.Context, req *CreateBillRequest) (*CreateBillResponse, error) {
	if !req.Currency.Valid() {
		return nil, errs.B().Code(errs.InvalidArgument).Msg("unsupported currency").Err()
	}

	// Handler generates deterministic-safe ID
	billID := uuid.New().String()

	_, err := s.temporalClient.ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:        workflowIDForBill(billID),
			TaskQueue: taskQueueName,
		},
		BillLifecycleWorkflow,
		BillWorkflowParams{BillID: billID, Currency: req.Currency},
	)
	if err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("start bill workflow").Err()
	}

	return &CreateBillResponse{BillID: billID}, nil
}

type AddLineItemRequest struct {
	Description string   `json:"description"`
	AmountMinor int64    `json:"amount_minor"`
	Currency    Currency `json:"currency"`
}

type AddLineItemResponse struct {
	LineItemID string `json:"line_item_id"`
}

//encore:api public method=POST path=/bills/:id/line-items
func (s *Service) AddLineItem(ctx context.Context, id string, req *AddLineItemRequest) (*AddLineItemResponse, error) {
	if !req.Currency.Valid() {
		return nil, errs.B().Code(errs.InvalidArgument).Msg("unsupported currency").Err()
	}

	// ✅ Pre-check status before signaling
	status, billCurrency, err := getBillStatusAndCurrency(ctx, id)
	if err != nil {
		return nil, err
	}
	if status != StatusOpen {
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("bill is closed").Err()
	}
	if billCurrency != req.Currency {
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("currency mismatch").Err()
	}

	lineItemID := uuid.New().String()

	sig := AddLineItemSignal{
		LineItemID:  lineItemID,
		Description: req.Description,
		AmountMinor: req.AmountMinor,
		Currency:    req.Currency,
	}

	if err := s.temporalClient.SignalWorkflow(ctx, workflowIDForBill(id), "", signalAddLineItem, sig); err != nil {
		// Optional fallback mapping if workflow already finished unexpectedly
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("bill is closed").Err()
	}

	return &AddLineItemResponse{LineItemID: lineItemID}, nil
}

type CloseBillResponse struct {
	AmountMinor int64         `json:"amount_minor"`
	Items       []LineItemDTO `json:"items"`
}

//encore:api public method=POST path=/bills/:id/close
func (s *Service) CloseBill(ctx context.Context, id string) (*CloseBillResponse, error) {
	// ✅ Pre-check status before signaling
	status, _, err := getBillStatusAndCurrency(ctx, id)
	if err != nil {
		return nil, err
	}
	if status != StatusOpen {
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("bill is closed").Err()
	}

	// Signal workflow to close
	if err := s.temporalClient.SignalWorkflow(ctx, workflowIDForBill(id), "", signalCloseBill, CloseBillSignal{}); err != nil {
		// If the workflow is already completed/missing, align with business semantics
		return nil, errs.B().Code(errs.FailedPrecondition).Msg("bill is closed").Err()
	}

	// Wait for workflow result
	run := s.temporalClient.GetWorkflow(ctx, workflowIDForBill(id), "")
	var result BillResult
	if err := run.Get(ctx, &result); err != nil {
		return nil, errs.B().Code(errs.Internal).Msg("get workflow result").Err()
	}

	// Indicate all line items being charged
	_, items, err := getBillWithItemsJoin(ctx, id)
	if err != nil {
		return nil, err
	}

	return &CloseBillResponse{
		AmountMinor: result.TotalMinor,
		Items:       lineItemsToDTOs(items),
	}, nil
}

// Encore GET query rule: no *string
type ListBillsRequest struct {
	Status string `query:"status"` // optional: ?status=OPEN or ?status=CLOSED
}

type ListBillsWithItemsResponse struct {
	Bills []BillWithItemsDTO `json:"bills"`
}

type BillWithItemsDTO struct {
	Bill  BillDTO       `json:"bill"`
	Items []LineItemDTO `json:"items"`
}

// ==============================
// Public API
// ==============================

//encore:api public method=GET path=/bills
func (s *Service) ListBillsWithItems(ctx context.Context, req *ListBillsRequest) (*ListBillsWithItemsResponse, error) {
	var st *BillStatus
	if req != nil && req.Status != "" {
		tmp := BillStatus(req.Status)
		switch tmp {
		case StatusOpen, StatusClosed:
			st = &tmp
		default:
			return nil, errs.B().Code(errs.InvalidArgument).Msg("invalid status").Err()
		}
	}

	bills, itemsByBill, err := listBillsWithItemsJoin(ctx, st)
	if err != nil {
		return nil, err
	}

	out := make([]BillWithItemsDTO, 0, len(bills))
	for _, b := range bills {
		out = append(out, BillWithItemsDTO{
			Bill:  billToDTO(b),
			Items: lineItemsToDTOs(itemsByBill[b.ID]),
		})
	}

	return &ListBillsWithItemsResponse{Bills: out}, nil
}

type GetBillWithItemsResponse struct {
	Bill  BillDTO       `json:"bill"`
	Items []LineItemDTO `json:"items"`
}

//encore:api public method=GET path=/bills/:id
func (s *Service) GetBillWithItems(ctx context.Context, id string) (*GetBillWithItemsResponse, error) {
	b, items, err := getBillWithItemsJoin(ctx, id)
	if err != nil {
		return nil, err
	}

	return &GetBillWithItemsResponse{
		Bill:  billToDTO(b),
		Items: lineItemsToDTOs(items),
	}, nil
}
