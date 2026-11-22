package finish_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/usecase/trip/finish"
)

// logRecords разбирает JSON-вывод slog: одна строка — одна запись.
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

// Бизнес-событие «поездка завершена» — требование ТЗ (п. 6.1). Мутационное
// тестирование показало, что вызов логгера можно удалить и ни один тест этого
// не заметит: остальные тесты передают slog.DiscardHandler. Этот тест пишет
// лог в буфер и проверяет запись.
func TestUsecase_Execute_LogsTripCompleted(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tx := NewMocktxManager(ctrl)
	repo := NewMocktripRepository(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	id := uuid.New()
	from := trip.StatusActive
	runInTx(tx)
	repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(activeTrip(id), nil)
	repo.EXPECT().Finish(gomock.Any(), id, wantFinishedAt).Return(nil)
	repo.EXPECT().AddStatusHistory(gomock.Any(), id, &from, trip.StatusCompleted).Return(nil)
	metrics.EXPECT().TripCompleted(gomock.Any())

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	if _, err := finish.New(tx, repo, fixedClock, metrics, log).Execute(context.Background(), id); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	records := logRecords(t, &buf)
	if len(records) != 1 {
		t.Fatalf("records = %d, want ровно одна запись о завершении: %s", len(records), buf.String())
	}
	rec := records[0]
	if rec["msg"] != "trip completed" || rec["level"] != "INFO" {
		t.Errorf("msg/level = %v/%v, want trip completed/INFO", rec["msg"], rec["level"])
	}
	if rec["trip_id"] != id.String() {
		t.Errorf("trip_id = %v, want %s", rec["trip_id"], id)
	}
}

// Если транзакция не прошла, поездка не завершена — и событие не пишется.
func TestUsecase_Execute_NoLogOnError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tx := NewMocktxManager(ctrl)
	repo := NewMocktripRepository(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	id := uuid.New()
	completed := activeTrip(id)
	completed.Status = trip.StatusCompleted
	runInTx(tx)
	repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(completed, nil)

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	_, err := finish.New(tx, repo, fixedClock, metrics, log).Execute(context.Background(), id)
	if !errors.Is(err, trip.ErrTripCompleted) {
		t.Fatalf("err = %v, want ErrTripCompleted", err)
	}
	if buf.Len() != 0 {
		t.Errorf("при ошибке бизнес-событие не пишется, got %s", buf.String())
	}
}
