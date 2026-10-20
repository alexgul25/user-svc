package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexgul25/user-svc/internal/app"
	"github.com/alexgul25/user-svc/internal/config"
	"github.com/alexgul25/user-svc/internal/lib/logger"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	appCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.LoadUserService()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log := logger.New(cfg.Env)

	application, err := app.New(appCtx, log, cfg)
	if err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}
	defer func() {
		if err := application.CloseStorage(); err != nil {
			log.Error("failed to close storage", slog.Any("error", err))
		}
	}()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- application.RunServer()
	}()

	select {
	case err := <-serverErr:
		// Сервер не запустился (например, порт занят) или аварийно остановился.
		return fmt.Errorf("grpc server: %w", err)
	case <-appCtx.Done():
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.GRPCServer.GracefulTimeout)
	defer cancel()
	application.GracefulStop(stopCtx)

	return nil
}
