- **service.go** wires everything: Encore-managed Postgres for app data + a Temporal client + a worker. The worker registers your workflow and 3 activity functions.
- **store.go** defines your domain model: `Bill`, `LineItem`, `Currency`, `BillStatus`. This is your core business vocabulary.
- **activities.go** is your side-effect boundary: each activity touches the DB and is idempotent by primary key.

  1. Create bill row
  2. Insert line item (with state/currency guards)
  3. Close bill row with final total

- **workflow.go** is the durable orchestrator:

  1. Starts by creating the bill row
  2. Waits for `add-line-item` signals
  3. For each signal, runs the insert activity and updates in-workflow running total
  4. On `close-bill`, runs the close activity and returns the final state
     This lets multiple HTTP requests safely coordinate over time.

- **helpers.go** currently bundles three concerns:

  1. Temporal ID helper
  2. DTOs + mappers
  3. Join-based read queries
     That’s acceptable for a small take-home, though you could split later into `dto.go` + `store_joins.go` for cleanliness.

- **api.go** exposes the semantics:

  1. `POST /bills` starts the workflow (creating the bill row inside the workflow)
  2. `POST /bills/:id/line-items` validates state then signals the workflow
  3. `POST /bills/:id/close` signals close and returns total + items
  4. `GET /bills` and `GET /bills/:id` are read models using joins
