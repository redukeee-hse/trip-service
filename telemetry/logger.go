package telemetry

import (
	"io"
	"log/slog"
)

func NewLogger(w io.Writer, level slog.Level, service string) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(handler).With(slog.String("service", service))
}
