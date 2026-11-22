package trip

import (
	"github.com/jackc/pgx/v5"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

// colStatus — колонка статуса: встречается в SELECT, INSERT и UPDATE.
const colStatus = "status"

// tripColumns и scanTrip держат список колонок и порядок Scan в одном месте,
// чтобы SELECT и сканирование не разъехались при изменении схемы.
var tripColumns = []string{
	"id",
	"user_id",
	"driver_id",
	"start_latitude",
	"start_longitude",
	"end_latitude",
	"end_longitude",
	"price",
	colStatus,
	"started_at",
	"finished_at",
}

func scanTrip(row pgx.Row) (trip.Trip, error) {
	var t trip.Trip
	err := row.Scan(
		&t.ID,
		&t.UserID,
		&t.DriverID,
		&t.StartPoint.Latitude,
		&t.StartPoint.Longitude,
		&t.EndPoint.Latitude,
		&t.EndPoint.Longitude,
		&t.Price,
		&t.Status,
		&t.StartedAt,
		&t.FinishedAt,
	)
	if err != nil {
		return trip.Trip{}, err
	}

	// pgx отдаёт timestamptz в зоне процесса (time.Local). Домен и API живут
	// в UTC: create отвечает "...Z", значит и get/finish должны так же.
	t.StartedAt = t.StartedAt.UTC()
	if t.FinishedAt != nil {
		finished := t.FinishedAt.UTC()
		t.FinishedAt = &finished
	}
	return t, nil
}
