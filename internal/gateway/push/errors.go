package push

import "errors"

// Ошибки различаем по тому, кто виноват и поможет ли повтор (ЛР5 добавит
// ретраи именно для ErrUnavailable). Ошибка транспорта — третий случай:
// до ответа дело не дошло (таймаут, соединение отклонено, DNS), её
// возвращаем как есть, обёрнутой через %w — по ней работает errors.Is
// с context.DeadlineExceeded.
var (
	// ErrRejected — Push Service ответил 4xx: наш запрос неверен. Повтор
	// того же запроса даст тот же ответ, чинить надо код.
	ErrRejected = errors.New("push service rejected the request")

	// ErrUnavailable — Push Service ответил 5xx или 429: проблема на его
	// стороне или нас ограничили по частоте. Повтор позже может помочь.
	ErrUnavailable = errors.New("push service is unavailable")
)
