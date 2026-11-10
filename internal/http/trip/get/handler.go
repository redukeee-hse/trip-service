package get

import (
	"net/http"

	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/response"
	"github.com/redukeee-hse/avitoService/internal/http/trip/tripapi"
)

// Handler — ручка GET /api/v1/trips/{tripId}.
type Handler struct {
	usecase tripGetter
}

func New(usecase tripGetter) *Handler {
	return &Handler{usecase: usecase}
}

// Handle вызывается из router. tripId уже разобран сгенерированным кодом:
// кривой UUID до сюда не доходит, его отклоняет ErrorHandlerFunc роутера.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	t, err := h.usecase.Execute(r.Context(), tripID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, tripapi.FromDomain(t))
}
