package app_test

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/alexgul25/user-svc/internal/app"
	"github.com/alexgul25/user-svc/internal/config"
)

// Этот тест не требует базы данных и запускается вместе с unit-тестами.

func TestNew_DatabaseUnavailable(t *testing.T) {
	cfg := &config.Config{
		Env: "local",
		// Порт 1 на localhost закрыт: соединение отклоняется сразу.
		Database: config.DatabaseConfig{User: "user", Password: "pass", DbName: "db", Host: "127.0.0.1", Port: 1},
		JWT:      config.JWTConfig{Secret: "secret", TokenTTL: time.Hour},
	}

	// Сервис не должен запускаться без базы: ошибка видна сразу при старте,
	// а не на первом запросе пользователя.
	application, err := app.New(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)
	if err == nil {
		t.Fatal("New succeeded without database, want error")
	}
	if !strings.Contains(err.Error(), "failed to ping storage") {
		t.Errorf("error %q does not explain that the database is unavailable", err)
	}
	if application != nil {
		t.Errorf("expected nil app on error, got %+v", application)
	}
}
