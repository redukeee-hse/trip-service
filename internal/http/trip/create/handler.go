package create

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/response"
	"github.com/redukeee-hse/avitoService/internal/http/trip/tripapi"
)

type Handler struct {
	usecase tripCreator
}

func New(usecase tripCreator) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.InvalidRequest(w, r, "cannot read request body")
		return
	}

	if err := checkRequiredFields(body); err != nil {
		response.InvalidRequest(w, r, err.Error())
		return
	}

	var req api.TripData
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.InvalidRequest(w, r, "invalid JSON body")
		return
	}

	t, created, err := h.usecase.Execute(r.Context(), toDomain(req), params.IdempotencyKey)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("Location", "/api/v1/trips/"+t.ID.String())
	response.JSON(w, status, tripapi.FromDomain(t))
}
