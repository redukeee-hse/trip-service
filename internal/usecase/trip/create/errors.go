package create

import "errors"

// ErrIdempotencyConflict — Idempotency-Key уже использован с другим телом запроса.
//
// Это правило протокола повторных запросов, а не правило поездки: сама поездка
// ничего не знает об Idempotency-Key. Проверяет ключ только usecase создания,
// поэтому ошибка объявлена здесь. Транспорт (http/domainerror) может её
// импортировать: правила слоёв запрещают http ходить в repository и infra,
// но не в usecase.
var ErrIdempotencyConflict = errors.New("ключ идемпотентности уже использован с другим телом запроса")
