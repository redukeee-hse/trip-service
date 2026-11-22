package create

// Внутренний тест (package create): checkRequiredFields и toDomain
// неэкспортируемые, а проверять их через хендлер — слишком далеко от сути.

import (
	"testing"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
)

const errNotObject = "expected a JSON object"

func TestCheckRequiredFields(t *testing.T) {
	t.Parallel()

	const point = `{"latitude": 55.75, "longitude": 37.61}`
	tests := []struct {
		name    string
		body    string
		wantErr string // "" — ошибки нет
	}{
		{
			name: "все поля на месте",
			body: `{"user_id":"u","driver_id":"d","start_point":` + point + `,"end_point":` + point + `,"price":0}`,
		},
		{
			name:    "нет price — 0 не должен считаться заданным",
			body:    `{"user_id":"u","driver_id":"d","start_point":` + point + `,"end_point":` + point + `}`,
			wantErr: `field "price" is required`,
		},
		{
			name:    "price: null",
			body:    `{"user_id":"u","driver_id":"d","start_point":` + point + `,"end_point":` + point + `,"price":null}`,
			wantErr: `field "price" is required`,
		},
		{
			name:    "в точке нет longitude",
			body:    `{"user_id":"u","driver_id":"d","start_point":{"latitude":1},"end_point":` + point + `,"price":1}`,
			wantErr: `start_point: field "longitude" is required`,
		},
		{
			name:    "точка не объект",
			body:    `{"user_id":"u","driver_id":"d","start_point":` + point + `,"end_point":[1,2],"price":1}`,
			wantErr: `end_point: expected a JSON object`,
		},
		{
			name:    "тело не объект",
			body:    `[]`,
			wantErr: errNotObject,
		},
		{
			name:    "тело null",
			body:    `null`,
			wantErr: errNotObject,
		},
		{
			name:    "битый JSON",
			body:    `{"user_id":`,
			wantErr: errNotObject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := checkRequiredFields([]byte(tt.body))

			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("unexpected err: %v", err)
			case tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr):
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestToDomain(t *testing.T) {
	t.Parallel()

	req := api.TripData{
		UserId:     uuid.New(),
		DriverId:   uuid.New(),
		StartPoint: api.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   api.Coordinates{Latitude: 59.94, Longitude: 30.31},
		Price:      1450,
	}

	got := toDomain(req)

	want := trip.Trip{
		UserID:     req.UserId,
		DriverID:   req.DriverId,
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 59.94, Longitude: 30.31},
		Price:      1450,
	}
	if got != want {
		t.Errorf("got %+v\nwant %+v (серверные поля должны остаться пустыми)", got, want)
	}
}
