package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	StatusActive    TripStatus = "active"
	StatusCompleted TripStatus = "completed"
)

var (
	ErrTripNotFound  = errors.New("поездка не найдена")
	ErrDriverBusy    = errors.New("у водителя уже есть активная поездка")
	ErrTripCompleted = errors.New("поездка уже завершена")
)

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type TripStatus string

type Trip struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint Coordinates
	EndPoint   Coordinates
	Price      int64
	Status     TripStatus
	StartedAt  time.Time
	FinishedAt *time.Time
}
