package app

import (
	"fmt"
	"log/slog"
	"time"

	grpcapp "github.com/alexgul25/user-svc/internal/app/grpc"
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

func New(
	log *slog.Logger,
	grpcPort int,
	dbUser, dbPassword, dbHost, dbName string, dbPort int,
	jwtSecret string,
	tokenTTL time.Duration,
	servicesWithEmailHidden []string,
) (*App, error) {
	storage, err := postgresql.NewStorage(dbUser, dbPassword, dbHost, dbName, dbPort)
	if err != nil {
		return nil, fmt.Errorf("failed to init storage: %w", err)
	}

	userStorage := postgresql.NewUserStorage(storage.DB())

	jwtManger := jwtmanager.New([]byte(jwtSecret), tokenTTL)

	userLogic := userlogic.New(log, userStorage, userStorage, jwtManger)

	serverApp := grpcapp.New(log, userLogic, grpcPort, servicesWithEmailHidden)

	return &App{grpcServer: serverApp, storageCloser: storage}, nil
}

func (a *App) CloseStorage() error {
	return a.storageCloser.Close()
}

func (a *App) RunServer() {
	a.grpcServer.MustRun()
}

func (a *App) GracefulStop() {
	a.grpcServer.GracefulStop()
}
