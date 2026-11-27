// Package tripapi переводит доменную поездку и её точки в тело ответа API.
// Общий для ручек: create, get и finish отдают api.Trip, ручки координат — api.TripPosition.
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

// PositionFromDomain — точка маршрута в теле ответа. Общий для ручек
// сохранения точки и получения маршрута.
func PositionFromDomain(p trip.Position) api.TripPosition {
	return api.TripPosition{
		Id:          p.ID,
		TripId:      p.TripID,
		Coordinates: api.Coordinates{Latitude: p.Coordinates.Latitude, Longitude: p.Coordinates.Longitude},
		RecordedAt:  p.RecordedAt,
	}
}
