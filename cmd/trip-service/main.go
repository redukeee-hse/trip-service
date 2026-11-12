package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

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
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := c.Close(closeCtx); err != nil {
			log.Warn("resources closed with error", slog.String("error", err.Error()))
		}
	}()

	if envErr != nil {
		log.Debug(".env not loaded", slog.String("error", envErr.Error()))
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("server started", slog.String("addr", cfg.Addr))
		serverErr <- c.Server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	// Контекст сигнала уже отменён — для Shutdown нужен свой, с таймаутом.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := c.Server.Shutdown(shutdownCtx); err != nil {
		log.Warn("graceful shutdown timed out, closing forcibly", slog.String("error", err.Error()))
		return errors.Join(err, c.Server.Close())
	}
	log.Info("server stopped")
	return nil
}
