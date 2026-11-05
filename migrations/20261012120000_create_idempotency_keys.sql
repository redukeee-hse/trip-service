-- +goose Up
CREATE TABLE idempotency_keys (
    key          UUID PRIMARY KEY,
    request_data TEXT NOT NULL,
    trip_id      UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE idempotency_keys;
