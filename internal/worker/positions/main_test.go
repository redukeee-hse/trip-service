package positions_test

import (
	"testing"

	"go.uber.org/goleak"
)

// Ни один тест пакета не должен оставлять за собой горутин: воркеры,
// сканер и вызовы в Push Service обязаны завершаться (ЛР3 п. 7.6).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
