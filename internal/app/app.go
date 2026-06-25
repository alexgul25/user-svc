package app

import (
	"fmt"
	"log/slog"
	"time"

	grpcapp "github.com/alexgul25/user-svc/internal/app/grpc"
	"github.com/alexgul25/user-svc/internal/lib/jwt"
	userlogic "github.com/alexgul25/user-svc/internal/service/user"
	"github.com/alexgul25/user-svc/internal/storage/postgresql"
)

type StorageCloser interface {
	Close() error
}

type App struct {
	GRPCServer *grpcapp.ServerApp
	StorageCloser
}

func New(
	log *slog.Logger,
	grpcPort int,
	dbUser, dbPassword, dbHost, dbName string, dbPort int,
	jwtSecret string,
	tokenTTL time.Duration,
	apiKey string,
) (*App, error) {
	storage, err := postgresql.NewStorage(dbUser, dbPassword, dbHost, dbName, dbPort)
	if err != nil {
		return nil, fmt.Errorf("failed to init storage: %w", err)
	}

	userStorage := postgresql.NewUserStorage(storage.DB())

	jwtManger := jwt.NewJWTManager([]byte(jwtSecret), tokenTTL)

	userLogic := userlogic.NewUserLogic(log, userStorage, userStorage, jwtManger)

	serverApp := grpcapp.New(log, userLogic, grpcPort, apiKey)

	return &App{GRPCServer: serverApp, StorageCloser: storage}, nil
}
