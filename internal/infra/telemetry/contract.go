//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package telemetry

import "context"

// activeTripsCounter — то, что нужно gauge tripgo.trips.active от хранилища.
// Объявлен здесь, у потребителя; реализует его repository/trip, связывает
// container.
type activeTripsCounter interface {
	CountActive(ctx context.Context) (int64, error)
}
