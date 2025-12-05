package bill

import (
	"context"
	"fmt"

	"encore.dev/storage/sqldb"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

var db = sqldb.NewDatabase("feesdb", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})

const taskQueueName = "fees-billing"

//encore:service
type Service struct {
	temporalClient client.Client
	worker         worker.Worker
}

func initService() (*Service, error) {
	c, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
	})
	if err != nil {
		return nil, fmt.Errorf("temporal client: %w", err)
	}

	w := worker.New(c, taskQueueName, worker.Options{})

	// Register workflow + activities
	w.RegisterWorkflow(BillLifecycleWorkflow)

	// register activity functions
	w.RegisterActivity(CreateBillRowActivity)
	w.RegisterActivity(AddLineItemActivity)
	w.RegisterActivity(CloseBillActivity)

	if err := w.Start(); err != nil {
		c.Close()
		return nil, fmt.Errorf("worker start: %w", err)
	}

	return &Service{temporalClient: c, worker: w}, nil
}

func (s *Service) Shutdown(ctx context.Context) {
	s.worker.Stop()
	s.temporalClient.Close()
}
