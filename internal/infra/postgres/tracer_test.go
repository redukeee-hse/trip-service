package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	tblTrips = "trips"
	opSelect = "SELECT"
)

// Запросы — в том виде, в каком их строит squirrel в репозитории.
func TestParseSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sql            string
		wantOperation  string
		wantCollection string
	}{
		{"INSERT INTO trips (id,user_id) VALUES ($1,$2)", "INSERT", tblTrips},
		{"INSERT INTO trip_status_history(trip_id) VALUES ($1)", "INSERT", "trip_status_history"},
		{"SELECT id, status FROM trips WHERE id = $1 FOR UPDATE", opSelect, tblTrips},
		{"select request_data from idempotency_keys where key = $1", opSelect, "idempotency_keys"},
		{`UPDATE "trips" SET status = $1 WHERE id = $2`, "UPDATE", "trips"},
		{"DELETE FROM idempotency_keys WHERE key = $1", "DELETE", "idempotency_keys"},
		{"begin", "BEGIN", ""},
		{"commit", "COMMIT", ""},
		{"-- name: GetTrip\nSELECT * FROM trips", opSelect, tblTrips},
		{"SELECT 1", opSelect, ""},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			t.Parallel()

			op, coll := parseSQL(tt.sql)
			if op != tt.wantOperation || coll != tt.wantCollection {
				t.Errorf("parseSQL = (%q, %q), want (%q, %q)", op, coll, tt.wantOperation, tt.wantCollection)
			}
		})
	}
}

func TestErrorType(t *testing.T) {
	t.Parallel()

	uniqueViolation := &pgconn.PgError{Code: "23505", ConstraintName: "trips_driver_active_uniq"}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "postgres error — SQLSTATE", err: fmt.Errorf("insert: %w", uniqueViolation), want: "23505"},
		{name: "timeout", err: fmt.Errorf("query: %w", context.DeadlineExceeded), want: "timeout"},
		{name: "canceled", err: context.Canceled, want: "canceled"},
		// Текст ошибки в лейбл не идёт: высокая кардинальность.
		{name: "other", err: errors.New("dial tcp 10.0.0.1:5432: connection refused"), want: "_OTHER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := errorType(tt.err); got != tt.want {
				t.Errorf("errorType = %q, want %q", got, tt.want)
			}
		})
	}
}
