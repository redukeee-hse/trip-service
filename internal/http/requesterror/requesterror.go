// Package requesterror передаёт внутреннюю ошибку запроса из места, где её
// обработали (ответ 500), в middleware логирования.
//
// Зачем: по conventions ошибка логируется один раз, а клиенту её детали не
// уходят. Хендлер отвечает 500 без деталей, а текст ошибки попадает в ту же
// единственную запись "request handled", которую пишет middleware. Отдельный
// пакет нужен, чтобы middleware и response не импортировали друг друга.
package requesterror

import "context"

type slotKey struct{}

// slot — ячейка в контексте запроса. Указатель: middleware кладёт его до
// вызова хендлера, а хендлер записывает ошибку уже в свою копию контекста.
type slot struct {
	err error
}

// Attach кладёт в контекст пустую ячейку под ошибку и возвращает функцию,
// которая после обработки запроса отдаёт записанную ошибку (или nil).
func Attach(ctx context.Context) (context.Context, func() error) {
	s := &slot{}
	return context.WithValue(ctx, slotKey{}, s), func() error { return s.err }
}

// Report сохраняет ошибку для записи в лог. Без Attach ничего не делает,
// поэтому хендлеры в тестах можно вызывать без middleware.
func Report(ctx context.Context, err error) {
	if s, ok := ctx.Value(slotKey{}).(*slot); ok {
		s.err = err
	}
}
