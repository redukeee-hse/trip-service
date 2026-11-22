package create

import (
	"encoding/json"
	"errors"
	"fmt"
)

// checkRequiredFields проверяет форму тела: все поля на месте и не null.
// json.Decoder сам этого не умеет: пропущенное поле тихо станет нулём,
// и "price": 0 не отличить от забытого price. Диапазоны значений здесь
// не проверяем — это правила поездки, их проверяет trip.Validate в usecase.
func checkRequiredFields(body []byte) error {
	top, err := requireKeys(body, "user_id", "driver_id", "start_point", "end_point", "price")
	if err != nil {
		return err
	}
	for _, point := range []string{"start_point", "end_point"} {
		if _, err := requireKeys(top[point], "latitude", "longitude"); err != nil {
			return fmt.Errorf("%s: %w", point, err)
		}
	}
	return nil
}

func requireKeys(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New("expected a JSON object")
	}
	for _, key := range keys {
		value, ok := obj[key]
		if !ok || string(value) == "null" {
			return nil, fmt.Errorf("field %q is required", key)
		}
	}
	return obj, nil
}
