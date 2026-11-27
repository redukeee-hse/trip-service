// Package listpositions — ручка GET /api/v1/trips/{tripId}/positions.
package listpositions

import (
	"net/http"

	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/response"
	"github.com/redukeee-hse/avitoService/internal/http/trip/tripapi"
)

type Handler struct {
	usecase positionLister
}

func New(usecase positionLister) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	positions, err := h.usecase.Execute(r.Context(), tripID)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	// make, а не var: nil-срез сериализуется в null, а контракт обещает [].
	body := api.TripPositionsResponse{Positions: make([]api.TripPosition, 0, len(positions))}
	for _, p := range positions {
		body.Positions = append(body.Positions, tripapi.PositionFromDomain(p))
	}
	response.JSON(w, http.StatusOK, body)
}
