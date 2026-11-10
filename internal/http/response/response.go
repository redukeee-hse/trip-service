// Package response пишет ответы в http.ResponseWriter: JSON и problem+json.
// Что именно отдать на ошибку, решает domainerror; здесь только запись.
package response

import (
	"encoding/json"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/domainerror"
	"github.com/redukeee-hse/avitoService/internal/http/requesterror"
)

// JSON пишет v с заданным статусом.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Статус уже отправлен, исправить ответ нельзя. Обрыв соединения увидит
	// middleware логирования, поэтому ошибку записи здесь не обрабатываем.
	_ = json.NewEncoder(w).Encode(v)
}

// Error переводит ошибку usecase в Problem и пишет её.
//
// Для 5xx клиент получает только internal_error, а сама ошибка уходит
// в лог (через middleware логирования, один раз) и в спан запроса: спан
// помечается codes.Error, получает error.type и событие с текстом ошибки.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	status, body := domainerror.Problem(err)
	if status >= http.StatusInternalServerError {
		requesterror.Report(r.Context(), err)
		span := trace.SpanFromContext(r.Context())
		span.RecordError(err)
		span.SetStatus(codes.Error, body.Code)
		span.SetAttributes(attribute.String("error.type", body.Code))
	}
	writeProblem(w, r, status, body)
}

// InvalidRequest пишет 400 invalid_request с пояснением для клиента.
func InvalidRequest(w http.ResponseWriter, r *http.Request, detail string) {
	status, body := domainerror.InvalidRequest(detail)
	writeProblem(w, r, status, body)
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, body api.Problem) {
	instance := r.URL.Path
	body.Instance = &instance

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
