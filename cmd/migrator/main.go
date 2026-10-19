package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/alexgul25/user-svc/internal/config"
	"github.com/alexgul25/user-svc/migrations"
)

func main() {
	cfg, err := config.LoadUserService()
	if err != nil {
		slog.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	db, err := sql.Open("pgx", cfg.Database.DSN())
	if err != nil {
		slog.Error("failed to open DB", slog.Any("error", err))
		os.Exit(1)
	}
	defer db.Close()

	// Миграции встроены в бинарник: мигратор не зависит от текущей папки.
	results, err := migrations.Up(context.Background(), db)
	for _, r := range results {
		slog.Info("migration applied", slog.String("file", r.Source.Path), slog.Duration("duration", r.Duration))
	}
	if err != nil {
		slog.Error("failed to up migrations", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("migrations applied successfully")
}
