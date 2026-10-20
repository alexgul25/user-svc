package app

import (
	"context"
	"fmt"
	"log/slog"

	grpcapp "github.com/alexgul25/user-svc/internal/app/grpc"
	"github.com/alexgul25/user-svc/internal/config"
	jwtmanager "github.com/alexgul25/user-svc/internal/lib/jwt"
	userlogic "github.com/alexgul25/user-svc/internal/service/user"
	"github.com/alexgul25/user-svc/internal/storage/postgresql"
)

type StorageCloser interface {
	Close() error
}

type App struct {
	grpcServer    *grpcapp.ServerApp
	storageCloser StorageCloser
}

func New(ctx context.Context, log *slog.Logger, cfg *config.Config) (*App, error) {
	storage, err := postgresql.NewStorage(cfg.Database.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to init storage: %w", err)
	}

	if err := storage.Ping(ctx); err != nil {
		storage.Close()
		return nil, fmt.Errorf("failed to ping storage: %w", err)
	}

	userStorage := postgresql.NewUserStorage(storage.DB())

	jwtManger := jwtmanager.New([]byte(cfg.JWT.Secret), cfg.JWT.TokenTTL)

	userLogic := userlogic.New(log, userStorage, userStorage, jwtManger)

	serverApp := grpcapp.New(log, userLogic, cfg.GRPCServer.Port, cfg.GRPCServer.ServicesWithEmailHidden)

	return &App{grpcServer: serverApp, storageCloser: storage}, nil
}

func (a *App) CloseStorage() error {
	return a.storageCloser.Close()
}

// RunServer обслуживает запросы до остановки сервера. Ошибка означает,
// что сервер не смог запуститься или аварийно завершился (например, порт занят).
func (a *App) RunServer() error {
	return a.grpcServer.Run()
}

// GracefulStop останавливает сервер, дожидаясь начатых запросов не дольше, чем живёт ctx.
func (a *App) GracefulStop(ctx context.Context) {
	a.grpcServer.GracefulStop(ctx)
}
