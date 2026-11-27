-- +goose Up
-- NULL — координат ещё не было.
ALTER TABLE trips ADD COLUMN last_position_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE trips DROP COLUMN last_position_at;
