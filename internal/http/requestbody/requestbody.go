// Package requestbody проверяет обязательные поля JSON до декодирования:
// json.Decoder не отличает пропущенное поле от нулевого значения.
package requestbody

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RequireKeys проверяет, что в объекте есть все keys и они не null.
func RequireKeys(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
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
