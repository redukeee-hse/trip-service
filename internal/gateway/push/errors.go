package push

import "errors"

var (
	// ErrRejected — ответ 4xx: запрос неверен, повтор не поможет.
	ErrRejected = errors.New("push service rejected the request")

	// ErrUnavailable — ответ 5xx или 429: повтор позже может помочь.
	ErrUnavailable = errors.New("push service is unavailable")
)
