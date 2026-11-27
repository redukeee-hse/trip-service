package create

import (
	"fmt"

	"github.com/redukeee-hse/avitoService/internal/http/requestbody"
)

// checkRequiredFields проверяет форму тела: все поля на месте и не null.
// Диапазоны значений здесь не проверяем — это правила поездки, их проверяет
// trip.Validate в usecase.
func checkRequiredFields(body []byte) error {
	top, err := requestbody.RequireKeys(body, "user_id", "driver_id", "start_point", "end_point", "price")
	if err != nil {
		return err
	}
	for _, point := range []string{"start_point", "end_point"} {
		if _, err := requestbody.RequireKeys(top[point], "latitude", "longitude"); err != nil {
			return fmt.Errorf("%s: %w", point, err)
		}
	}
	return nil
}
