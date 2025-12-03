CREATE TABLE bills (
    id          TEXT PRIMARY KEY,
    status      TEXT NOT NULL CHECK (status IN ('OPEN', 'CLOSED')),
    currency    TEXT NOT NULL CHECK (currency IN ('USD', 'GEL')),
    total_minor BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at   TIMESTAMPTZ
);

CREATE TABLE bill_line_items (
    id           TEXT PRIMARY KEY,
    bill_id      TEXT NOT NULL REFERENCES bills(id) ON DELETE CASCADE,
    description  TEXT NOT NULL,
    amount_minor BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
