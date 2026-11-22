package requesterror_test

import (
	"context"
	"errors"
	"testing"

	"github.com/redukeee-hse/avitoService/internal/http/requesterror"
)

func TestAttachAndReport(t *testing.T) {
	t.Parallel()

	ctx, reported := requesterror.Attach(context.Background())
	if err := reported(); err != nil {
		t.Fatalf("до Report ошибка должна быть nil, got %v", err)
	}

	errDB := errors.New("connection refused")
	// Хендлер получает производный контекст — ошибка всё равно должна дойти.
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	requesterror.Report(child, errDB)

	if err := reported(); !errors.Is(err, errDB) {
		t.Errorf("reported = %v, want %v", err, errDB)
	}
}

func TestReport_WithoutAttach(t *testing.T) {
	t.Parallel()

	// Хендлеры в unit-тестах вызываются без middleware: Report не должен паниковать.
	requesterror.Report(context.Background(), errors.New("boom"))
}
