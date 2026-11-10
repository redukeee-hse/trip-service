package create

import (
	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
)

// toDomain переводит тело запроса в доменную поездку. Серверные поля
// (ID, Status, StartedAt) не заполняем: их задаёт usecase.
func toDomain(req api.TripData) trip.Trip {
	return trip.Trip{
		UserID:     req.UserId,
		DriverID:   req.DriverId,
		StartPoint: trip.Coordinates{Latitude: req.StartPoint.Latitude, Longitude: req.StartPoint.Longitude},
		EndPoint:   trip.Coordinates{Latitude: req.EndPoint.Latitude, Longitude: req.EndPoint.Longitude},
		Price:      req.Price,
	}
}
