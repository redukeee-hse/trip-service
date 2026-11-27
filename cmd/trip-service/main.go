package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/redukeee-hse/avitoService/internal/config"
	"github.com/redukeee-hse/avitoService/internal/container"
)

// main читает конфиг, собирает контейнер, запускает сервер и ждёт сигнала.
// Сам ничего не создаёт: все зависимости — в internal/container.
func main() {
	if err := run(); err != nil {
		// Контейнер мог не собраться (битый конфиг, нет БД), и его логгера
		// ещё нет. Для последней записи хватает JSON в stderr.
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("service stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// run вынесен из main, чтобы defer отрабатывали до os.Exit.
func run() error {
	// .env нужен только локально; в контейнере переменные приходят из окружения.
	envErr := godotenv.Load()

	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	c, err := container.New(ctx, cfg)
	if err != nil {
		return err
	}
	log := c.Logger

	if envErr != nil {
		log.Debug(".env not loaded", slog.String("error", envErr.Error()))
	}

	c.Positions.Start()

	serverErr := make(chan error, 1)
	go func() {
		log.Info("server started", slog.String("addr", cfg.Addr))
		serverErr <- c.Server.ListenAndServe()
	}()

	var runErr error
	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	}

	return errors.Join(runErr, shutdown(c, cfg.ShutdownTimeout))
}

// shutdown останавливает сервис в пределах одного общего бюджета
// SHUTDOWN_TIMEOUT: HTTP-сервер → сканер и воркеры → ресурсы.
//
// Контекст создаётся здесь, в момент остановки, а не при старте: иначе
// таймер шёл бы с запуска сервиса и к сигналу уже истёк. И от контекста
// сигнала он не наследуется — тот к этому моменту уже отменён.
func shutdown(c *container.Container, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var errs []error
	if err := c.Server.Shutdown(ctx); err != nil {
		c.Logger.Warn("graceful shutdown timed out, closing forcibly", slog.String("error", err.Error()))
		errs = append(errs, err, c.Server.Close())
	}
	if err := c.Positions.Stop(ctx); err != nil {
		c.Logger.Warn("position workers did not finish in time, stopped forcibly", slog.String("error", err.Error()))
		errs = append(errs, err)
	}
	if err := c.Close(ctx); err != nil {
		errs = append(errs, err)
	}
	c.Logger.Info("server stopped")
	return errors.Join(errs...)
}
