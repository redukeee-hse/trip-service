package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/model"
)

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	instance := r.URL.Path
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	problem := api.Problem{
		Type:     "https://tripgo.example/problems/" + strings.ReplaceAll(code, "_", "-"),
		Title:    title,
		Status:   int32(status),
		Detail:   &detail,
		Instance: &instance,
		Code:     code,
	}
	if err := json.NewEncoder(w).Encode(problem); err != nil {
		log.Printf("запись problem+json: %v", err)
	}
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, model.ErrTripNotFound):
		writeProblem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "Trip with this id does not exist")
	case errors.Is(err, model.ErrDriverBusy):
		writeProblem(w, r, http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, model.ErrTripCompleted):
		writeProblem(w, r, http.StatusConflict, "trip_completed", "Trip completed", "Trip is already completed")
	default:
		log.Printf("внутренняя ошибка %s %s: %v", r.Method, r.URL.Path, err)
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "Internal server error")
	}
}

func writeInvalidRequest(w http.ResponseWriter, r *http.Request, detail string) {
	writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", detail)
}

func InvalidParamHandler(w http.ResponseWriter, r *http.Request, err error) {
	writeInvalidRequest(w, r, "Invalid path or query parameter")
}
