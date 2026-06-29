package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexgul25/user-svc/internal/app"
	"github.com/alexgul25/user-svc/internal/config"
	"github.com/alexgul25/user-svc/internal/lib/logger"
)

func main() {
	appCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.LoadUserService()
	if err != nil {
		slog.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	log := logger.New(cfg.Env)

	application, err := app.New(
		log,
		cfg.GRPCServer.Port,
		cfg.Database.User, cfg.Database.Password, cfg.Database.Host, cfg.Database.DbName, cfg.Database.Port,
		cfg.JWT.Secret, cfg.JWT.TokenTTL,
		cfg.GRPCServer.ServicesWithEmailHidden,
	)
	if err != nil {
		slog.Error("failed to create app", slog.Any("error", err))
		os.Exit(1)
	}
	defer application.CloseStorage()

	go func() {
		application.RunServer()
	}()

	<-appCtx.Done()

	application.GracefulStop()
}
