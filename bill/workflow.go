package bill

import "go.temporal.io/sdk/workflow"

const (
	signalAddLineItem = "add-line-item"
	signalCloseBill   = "close-bill"
)

type BillWorkflowParams struct {
	BillID   string
	Currency string
}

type AddLineItemSignal struct {
	LineItemID  string
	AmountMinor int64
	Description string
	Currency    string
}

type CloseBillSignal struct{}

type LineItemState struct {
	ID          string
	AmountMinor int64
	Description string
}

type BillResult struct {
	BillID     string
	Currency   string
	TotalMinor int64
	Items      []LineItemState
}

// BillWorkflow: accrues line items over time, returns total when bill is closed.
func BillWorkflow(ctx workflow.Context, params BillWorkflowParams) (*BillResult, error) {
	state := &BillResult{
		BillID:     params.BillID,
		Currency:   params.Currency,
		TotalMinor: 0,
		Items:      make([]LineItemState, 0),
	}

	addCh := workflow.GetSignalChannel(ctx, signalAddLineItem)
	closeCh := workflow.GetSignalChannel(ctx, signalCloseBill)

	for {
		shouldClose := false
		sel := workflow.NewSelector(ctx)

		// Accrue a new line item
		sel.AddReceive(addCh, func(c workflow.ReceiveChannel, more bool) {
			var sig AddLineItemSignal
			c.Receive(ctx, &sig)

			// Safety: ignore wrong currency
			if sig.Currency != state.Currency {
				return
			}

			state.Items = append(state.Items, LineItemState{
				ID:          sig.LineItemID,
				AmountMinor: sig.AmountMinor,
				Description: sig.Description,
			})
			state.TotalMinor += sig.AmountMinor
		})

		// Close bill
		sel.AddReceive(closeCh, func(c workflow.ReceiveChannel, more bool) {
			var sig CloseBillSignal
			c.Receive(ctx, &sig)
			shouldClose = true
		})

		sel.Select(ctx)
		if shouldClose {
			break
		}
	}

	return state, nil
}
