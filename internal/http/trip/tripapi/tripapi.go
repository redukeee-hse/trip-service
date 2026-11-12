// Package tripapi переводит доменную поездку в тело ответа API.
// Общий для ручек create, get и finish: все три отдают api.Trip.
package tripapi

import (
	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
)

func FromDomain(t trip.Trip) api.Trip {
	return api.Trip{
		Id:         t.ID,
		UserId:     t.UserID,
		DriverId:   t.DriverID,
		StartPoint: api.Coordinates{Latitude: t.StartPoint.Latitude, Longitude: t.StartPoint.Longitude},
		EndPoint:   api.Coordinates{Latitude: t.EndPoint.Latitude, Longitude: t.EndPoint.Longitude},
		Price:      t.Price,
		Status:     api.TripStatus(t.Status),
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
	}
}
