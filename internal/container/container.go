// Package container — единственное место, где создаются конкретные реализации
// и связываются между собой. Здесь и только здесь встречаются pgxpool и
// конструкторы репозиториев; остальной код получает зависимости аргументами
// и знает только свои интерфейсы из contract.go.
package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redukeee-hse/avitoService/internal/config"
	"github.com/redukeee-hse/avitoService/internal/http/health"
	"github.com/redukeee-hse/avitoService/internal/http/middleware"
	"github.com/redukeee-hse/avitoService/internal/http/router"
	createhttp "github.com/redukeee-hse/avitoService/internal/http/trip/create"
	finishhttp "github.com/redukeee-hse/avitoService/internal/http/trip/finish"
	gethttp "github.com/redukeee-hse/avitoService/internal/http/trip/get"
	"github.com/redukeee-hse/avitoService/internal/infra/postgres"
	"github.com/redukeee-hse/avitoService/internal/infra/postgres/transaction"
	"github.com/redukeee-hse/avitoService/internal/infra/telemetry"
	triprepo "github.com/redukeee-hse/avitoService/internal/repository/trip"
	createuc "github.com/redukeee-hse/avitoService/internal/usecase/trip/create"
	finishuc "github.com/redukeee-hse/avitoService/internal/usecase/trip/finish"
	getuc "github.com/redukeee-hse/avitoService/internal/usecase/trip/get"
)

// Container — то, что нужно main: что запустить, чем логировать и что
// закрыть при остановке.
type Container struct {
	Server    *http.Server
	Logger    *slog.Logger
	pool      *pgxpool.Pool
	telemetry *telemetry.Providers
}

// New собирает приложение снизу вверх: infra → repository → usecase → http.
func New(ctx context.Context, cfg config.Config) (c *Container, err error) {
	// infra: телеметрия первой — её провайдеры нужны пулу и middleware.
	logger := telemetry.NewLogger(os.Stdout, cfg.LogLvl, cfg.Telemetry.ServiceName)

	providers, err := telemetry.Setup(ctx, cfg.Telemetry, logger)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}
	// Если сборка дальше упадёт, экспорт телеметрии надо остановить.
	defer func() {
		if err != nil {
			err = errors.Join(err, providers.Shutdown(context.WithoutCancel(ctx)))
		}
	}()

	pool, err := postgres.New(ctx, cfg.DB, providers.Tracer, providers.Meter)
	if err != nil {
		return nil, fmt.Errorf("postgres pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.DB.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	txManager := transaction.New(pool)

	tripMetrics, err := telemetry.NewTripMetrics(providers.Meter)
	if err != nil {
		pool.Close()
		return nil, err
	}

	// repository: репозиторий берёт соединение у того же менеджера, что
	// открывает транзакции, поэтому запросы внутри tx.Do идут в транзакции.
	trips := triprepo.New(txManager, cfg.DB.QueryTimeout)

	// usecase: один *triprepo.Repository реализует все узкие интерфейсы,
	// компилятор проверяет это прямо здесь.
	now := time.Now
	createTrip := createuc.New(txManager, trips, trips, now, tripMetrics, logger)
	getTrip := getuc.New(trips)
	finishTrip := finishuc.New(txManager, trips, now, tripMetrics, logger)

	// http
	routes := router.New(router.Handlers{
		Create: createhttp.New(createTrip),
		Get:    gethttp.New(getTrip),
		Finish: finishhttp.New(finishTrip),
		Health: health.New(pool, cfg.PingTimeout),
	})

	metrics, err := middleware.Metrics(providers.Meter, routes)
	if err != nil {
		pool.Close()
		return nil, err
	}

	// Порядок из ТЗ: Recover → Tracing → Logging → Metrics → роутер.
	// Logging стоит внутри Tracing, поэтому в его записи есть trace_id.
	handler := middleware.Recover(logger)(
		middleware.Tracing(providers.Tracer, providers.Propagator, routes)(
			middleware.Logging(logger)(
				metrics(
					routes,
				),
			),
		),
	)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	return &Container{Server: server, Logger: logger, pool: pool, telemetry: providers}, nil
}

// Close освобождает ресурсы. Вызывать после Server.Shutdown: иначе
// незавершённые запросы останутся без соединений с БД. Телеметрия
// закрывается последней, чтобы дописать спаны последних запросов.
func (c *Container) Close(ctx context.Context) error {
	c.pool.Close()
	if err := c.telemetry.Shutdown(ctx); err != nil {
		return fmt.Errorf("telemetry shutdown: %w", err)
	}
	return nil
}
