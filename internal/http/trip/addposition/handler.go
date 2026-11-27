// Package addposition — ручка POST /api/v1/trips/{tripId}/positions.
package addposition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/requestbody"
	"github.com/redukeee-hse/avitoService/internal/http/response"
)

type Handler struct {
	usecase positionAdder
}

func New(usecase positionAdder) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.InvalidRequest(w, r, "cannot read request body")
		return
	}

	if err := checkRequiredFields(body); err != nil {
		response.InvalidRequest(w, r, err.Error())
		return
	}

	var req api.PositionData
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.InvalidRequest(w, r, "invalid JSON body")
		return
	}

	coords := trip.Coordinates{Latitude: req.Coordinates.Latitude, Longitude: req.Coordinates.Longitude}
	p, err := h.usecase.Execute(r.Context(), tripID, coords, req.RecordedAt)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, toAPI(p))
}

func checkRequiredFields(body []byte) error {
	top, err := requestbody.RequireKeys(body, "coordinates", "recorded_at")
	if err != nil {
		return err
	}
	if _, err := requestbody.RequireKeys(top["coordinates"], "latitude", "longitude"); err != nil {
		return fmt.Errorf("coordinates: %w", err)
	}
	return nil
}

func toAPI(p trip.Position) api.TripPosition {
	return api.TripPosition{
		Id:          p.ID,
		TripId:      p.TripID,
		Coordinates: api.Coordinates{Latitude: p.Coordinates.Latitude, Longitude: p.Coordinates.Longitude},
		RecordedAt:  p.RecordedAt,
	}
}
