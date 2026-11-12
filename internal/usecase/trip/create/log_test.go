package create_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/usecase/trip/create"
)

// usecaseWithLog — тот же usecase, но логгер пишет в буфер, а не в никуда.
func (m mocks) usecaseWithLog(buf *bytes.Buffer) *create.Usecase {
	return create.New(m.tx, m.trips, m.keys, fixedClock, m.metrics, slog.New(slog.NewJSONHandler(buf, nil)))
}

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("строка лога не JSON: %q", line)
		}
		out = append(out, rec)
	}
	return out
}

// Бизнес-событие «поездка создана» — требование ТЗ (п. 6.1). Мутант с удалённым
// вызовом логгера выживал: остальные тесты передают slog.DiscardHandler.
func TestExecute_LogsTripCreated(t *testing.T) {
	t.Parallel()

	m := newMocks(t)
	m.runInTx()
	m.trips.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	m.trips.EXPECT().AddStatusHistory(gomock.Any(), gomock.Any(), gomock.Nil(), trip.StatusActive).Return(nil)
	m.expectCreatedMetric()

	var buf bytes.Buffer
	got, _, err := m.usecaseWithLog(&buf).Execute(context.Background(), input(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	records := logRecords(t, &buf)
	if len(records) != 1 {
		t.Fatalf("records = %d, want ровно одна запись о создании: %s", len(records), buf.String())
	}
	rec := records[0]
	if rec["msg"] != "trip created" || rec["level"] != "INFO" {
		t.Errorf("msg/level = %v/%v, want trip created/INFO", rec["msg"], rec["level"])
	}
	if rec["trip_id"] != got.ID.String() {
		t.Errorf("trip_id = %v, want id созданной поездки %s", rec["trip_id"], got.ID)
	}
	// Идентификаторы людей в лог не пишем (conventions, раздел 5).
	for _, pd := range []string{input().UserID.String(), input().DriverID.String()} {
		if strings.Contains(buf.String(), pd) {
			t.Errorf("в лог попал идентификатор пользователя или водителя: %s", buf.String())
		}
	}
}

// Повтор по Idempotency-Key возвращает старую поездку — это не новое событие.
func TestExecute_ReplayDoesNotLog(t *testing.T) {
	t.Parallel()

	m := newMocks(t)
	key := uuid.New()
	existing := input()
	existing.ID = uuid.New()
	existing.Status = trip.StatusActive

	var data string
	m.runInTx()
	m.keys.EXPECT().ClaimIdempotencyKey(gomock.Any(), key, gomock.Any(), gomock.Any(), wantTTL).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, d string, _ uuid.UUID, _ time.Duration) (bool, error) {
			data = d
			return false, nil
		})
	m.keys.EXPECT().GetIdempotencyKey(gomock.Any(), key).
		DoAndReturn(func(context.Context, uuid.UUID) (string, uuid.UUID, error) { return data, existing.ID, nil })
	m.trips.EXPECT().GetByID(gomock.Any(), existing.ID).Return(existing, nil)

	var buf bytes.Buffer
	if _, _, err := m.usecaseWithLog(&buf).Execute(context.Background(), input(), &key); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("повтор не должен писать trip created, got %s", buf.String())
	}
}
