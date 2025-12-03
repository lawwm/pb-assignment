package bill

import (
	"context"
	"fmt"

	"encore.dev/storage/sqldb"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

var db = sqldb.NewDatabase("feesdb", sqldb.DatabaseConfig{
	Migrations: "./migrations", // relative to the bill package
})

const taskQueueName = "fees-billing"

//encore:service
type Service struct {
	temporalClient client.Client
	worker         worker.Worker
}

// initService is called by Encore to construct the service.
func initService() (*Service, error) {
	// Connect to Temporal (adjust HostPort if needed)
	c, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}

	// Start worker for our task queue
	w := worker.New(c, taskQueueName, worker.Options{})
	w.RegisterWorkflow(BillWorkflow)

	if err := w.Start(); err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to start temporal worker: %w", err)
	}

	return &Service{
		temporalClient: c,
		worker:         w,
	}, nil
}

// Shutdown is called when the service stops.
func (s *Service) Shutdown(ctx context.Context) {
	s.worker.Stop()
	s.temporalClient.Close()
}
