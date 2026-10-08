package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	"github.com/redukeee-hse/avitoService/internal/business"
	"github.com/redukeee-hse/avitoService/internal/config"
	"github.com/redukeee-hse/avitoService/internal/database"
	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/handlers"
)

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("Ошибка скачивания .env файлов:", err)
	}

	ctx := context.Background()
	interruptCtx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
	defer stop()
	router := chi.NewRouter()

	cfg, err := config.LoadConfig()

	if err != nil {
		log.Fatal("Ошибка конфига:", err)
	}

	pool, err := database.NewPool(ctx, cfg.DB)
	if err != nil {
		log.Fatal("Ошибка создания пула:", err)
	}

	databaseConnectCtx, stopDatabaseCtx := context.WithTimeout(ctx, cfg.DB.ConnectTimeout)
	err = pool.Ping(databaseConnectCtx)
	stopDatabaseCtx()
	if err != nil {
		log.Fatalf("Не удалось подключиться к базе данных: %v", err)
	}

	repo := database.NewTripRepository(pool, cfg.DB.QueryTimeout)
	txManager := database.NewTxManager(pool)
	service := business.NewTripService(txManager, repo)

	handler := handlers.Handler{
		Pool:        pool,
		PingTimeout: cfg.PingTimeout,
		Service:     service,
	}

	var _ api.ServerInterface = (*handlers.Handler)(nil)

	api.HandlerWithOptions(&handler, api.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: handlers.InvalidParamHandler,
	})

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	serversErr := make(chan error, 1)
	go func() {
		log.Printf("Запускаю сервер на %s", cfg.Addr)
		serversErr <- server.ListenAndServe()
	}()

	exitCode := 0
	select {
	case <-interruptCtx.Done():
		log.Println("Пришел запрос на остановку сервера(SIGINT, SIGTERM)")
		log.Println("Начинаю закрытие сервера")
	case err = <-serversErr:
		if errors.Is(err, http.ErrServerClosed) {
			log.Println("Остановка сервера")
		} else {
			log.Printf("Ошибка сервера: %v", err)
			exitCode = 1
		}
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, cfg.ShutdownTimeout)
	defer cancel()
	err = server.Shutdown(shutdownCtx)
	if err != nil {
		log.Printf("Ошибка закрытия сервера: %v - Закрываю сервер принудительно", err)
		err := server.Close()
		if err != nil {
			log.Printf("Внутренняя ошибка: %v", err)
		}
	}
	log.Println("Сервер остановлен")

	pool.Close()
	log.Println("Пул закрыт")

	if exitCode != 0 {
		os.Exit(exitCode)
	}

}
