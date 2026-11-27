-- +goose Up
-- NULL означает «координат по поездке ещё не было»: в этом случае выборка
-- для опроса отсчитывает время от started_at.
ALTER TABLE trips ADD COLUMN last_position_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE trips DROP COLUMN last_position_at;
