-- +goose Up
CREATE TABLE trip_positions (
    id          BIGSERIAL PRIMARY KEY,
    trip_id     UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    latitude    DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude   DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    recorded_at TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Таблица только что создана и пуста, поэтому индекс строится обычным
-- CREATE INDEX в той же транзакции, CONCURRENTLY здесь не нужен.
CREATE INDEX trip_positions_trip_recorded_idx
    ON trip_positions (trip_id, recorded_at, id);

-- +goose Down
-- Индекс удаляется вместе с таблицей.
DROP TABLE trip_positions;
