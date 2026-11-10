// Package domainerror переводит ошибки приложения в HTTP-ответ.
//
// Это единственное место, где ошибка превращается в статус и тело Problem
// (RFC 9457). Хендлеры не разбирают ошибки сами, а отдают их сюда через
// пакет response. Функции чистые: ничего не пишут в ResponseWriter, поэтому
// маппинг проверяется table-driven тестом без httptest.
package domainerror

import (
	"errors"
	"net/http"
	"strings"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/usecase/trip/create"
)

const typeBase = "https://tripgo.example/problems/"

// Problem возвращает статус и тело ответа для ошибки из usecase.
// Неизвестная ошибка — 500 без деталей: текст ошибки БД клиенту не уходит.
func Problem(err error) (int, api.Problem) {
	var vErr *trip.ValidationError

	switch {
	case errors.As(err, &vErr):
		return problem(http.StatusBadRequest, "invalid_request", "Invalid request", vErr.Error())
	case errors.Is(err, trip.ErrTripNotFound):
		return problem(http.StatusNotFound, "trip_not_found", "Trip not found", "Trip with this id does not exist")
	case errors.Is(err, trip.ErrDriverBusy):
		return problem(http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, trip.ErrTripCompleted):
		return problem(http.StatusConflict, "trip_completed", "Trip completed", "Trip is already completed")
	case errors.Is(err, create.ErrIdempotencyConflict):
		return problem(http.StatusConflict, "idempotency_conflict", "Idempotency conflict",
			"Idempotency-Key was already used with a different request body")
	default:
		return problem(http.StatusInternalServerError, "internal_error", "Internal error", "Internal server error")
	}
}

// InvalidRequest — 400 для ошибок формы запроса, которые находит сам
// транспорт: битый JSON, лишнее или пропущенное поле, кривой параметр пути.
func InvalidRequest(detail string) (int, api.Problem) {
	return problem(http.StatusBadRequest, "invalid_request", "Invalid request", detail)
}

func problem(status int, code, title, detail string) (int, api.Problem) {
	return status, api.Problem{
		Type:   typeBase + strings.ReplaceAll(code, "_", "-"),
		Title:  title,
		Status: int32(status), //nolint:gosec // status — HTTP-код из net/http (100–599), в int32 помещается
		Detail: &detail,
		Code:   code,
	}
}
