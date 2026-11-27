// Package requestbody проверяет форму JSON-тела запроса до декодирования.
//
// json.Decoder сам не отличает пропущенное поле от нулевого значения:
// забытый "price" станет 0, забытый "recorded_at" — нулевым временем.
// RequireKeys ловит это до Decode, чтобы ответить 400, а не сохранить мусор.
package requestbody

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RequireKeys проверяет, что raw — JSON-объект и в нём есть все keys,
// причём не null. Возвращает объект по ключам для проверки вложенных полей.
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
