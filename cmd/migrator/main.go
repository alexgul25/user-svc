package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/pressly/goose/v3"

	"github.com/alexgul25/user-svc/internal/config"
)

func main() {
	cfg, err := config.LoadUserService()
	if err != nil {
		slog.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.DbName,
	)

	db, err := goose.OpenDBWithDriver("postgres", connStr)
	if err != nil {
		slog.Error("failed to open DB", slog.Any("error", err))
		os.Exit(1)
	}
	defer db.Close()

	// Путь к папке с миграциями относительно запуска (корень проекта)
	migrationsDir := "./migrations"
	if err := goose.Up(db, migrationsDir); err != nil {
		slog.Error("failed to up migrations", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("migrations applied successfully")
}
