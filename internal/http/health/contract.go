//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package health

import "context"

type pinger interface {
	Ping(ctx context.Context) error
}
