//go:build integration

package trip_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

const keyTTL = 24 * time.Hour

func newKey(t *testing.T) uuid.UUID {
	t.Helper()
	key := uuid.New()
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM idempotency_keys WHERE key = $1`, key); err != nil {
			t.Errorf("cleanup key %s: %v", key, err)
		}
	})
	return key
}

func TestRepository_ClaimIdempotencyKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, _ := setup(t)
	key := newKey(t)
	tripID := uuid.New()

	claimed, err := repo.ClaimIdempotencyKey(ctx, key, "body-1", tripID, keyTTL)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true, nil", claimed, err)
	}

	claimed, err = repo.ClaimIdempotencyKey(ctx, key, "body-2", uuid.New(), keyTTL)
	if err != nil || claimed {
		t.Fatalf("second claim = %v, %v; want false, nil", claimed, err)
	}

	// Повторный claim не перезаписал данные первого.
	data, gotTripID, err := repo.GetIdempotencyKey(ctx, key)
	if err != nil {
		t.Fatalf("GetIdempotencyKey: %v", err)
	}
	if data != "body-1" || gotTripID != tripID {
		t.Errorf("stored = %q, %s; want %q, %s", data, gotTripID, "body-1", tripID)
	}
}

func TestRepository_ClaimIdempotencyKey_ExpiredIsReclaimed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, _ := setup(t)
	key := newKey(t)

	// Ключ, созданный «давно» — старше TTL.
	if _, err := testPool.Exec(ctx,
		`INSERT INTO idempotency_keys (key, request_data, trip_id, created_at)
		 VALUES ($1, 'old-body', $2, now() - interval '25 hours')`, key, uuid.New()); err != nil {
		t.Fatalf("insert expired key: %v", err)
	}

	newTripID := uuid.New()
	claimed, err := repo.ClaimIdempotencyKey(ctx, key, "new-body", newTripID, keyTTL)
	if err != nil || !claimed {
		t.Fatalf("claim expired = %v, %v; want true, nil", claimed, err)
	}

	data, gotTripID, err := repo.GetIdempotencyKey(ctx, key)
	if err != nil {
		t.Fatalf("GetIdempotencyKey: %v", err)
	}
	if data != "new-body" || gotTripID != newTripID {
		t.Errorf("stored = %q, %s; want new-body, %s", data, gotTripID, newTripID)
	}
}

func TestRepository_GetIdempotencyKey_Missing(t *testing.T) {
	t.Parallel()
	repo, _ := setup(t)

	if _, _, err := repo.GetIdempotencyKey(context.Background(), uuid.New()); err == nil {
		t.Fatal("err = nil, want error for missing key")
	}
}
