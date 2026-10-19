// Package pgtest создаёт временные базы данных PostgreSQL для интеграционных тестов.
//
// Сервер задаётся переменной окружения TEST_DATABASE_URL. Каждый вызов NewDatabase
// создаёт на нём отдельную пустую базу со случайным именем, поэтому тестовые пакеты,
// которые go test запускает параллельно, не мешают друг другу. Пользователю из
// TEST_DATABASE_URL нужно право CREATEDB (у суперпользователя postgres оно есть).
package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// EnvDSN - переменная окружения со строкой подключения к тестовому серверу PostgreSQL.
const EnvDSN = "TEST_DATABASE_URL"

// NewDatabase создаёт пустую базу с именем "<prefix>_<случайный суффикс>" и возвращает
// строку подключения к ней и функцию, которая удаляет базу.
func NewDatabase(ctx context.Context, prefix string) (dsn string, drop func() error, err error) {
	baseDSN := os.Getenv(EnvDSN)
	if baseDSN == "" {
		return "", nil, fmt.Errorf("pgtest: %s is not set, start a test database with `make test-db-up`", EnvDSN)
	}

	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", nil, fmt.Errorf("pgtest: %w", err)
	}
	name := prefix + "_" + hex.EncodeToString(suffix)

	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		return "", nil, fmt.Errorf("pgtest: open %s: %w", EnvDSN, err)
	}
	defer admin.Close()

	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+quoted); err != nil {
		return "", nil, fmt.Errorf("pgtest: create database %s: %w", name, err)
	}

	dsn, err = withDatabase(baseDSN, name)
	if err != nil {
		return "", nil, err
	}

	drop = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		admin, err := sql.Open("pgx", baseDSN)
		if err != nil {
			return fmt.Errorf("pgtest: open %s: %w", EnvDSN, err)
		}
		defer admin.Close()

		// WITH (FORCE) закрывает соединения, которые тест мог не закрыть.
		if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			return fmt.Errorf("pgtest: drop database %s: %w", name, err)
		}
		return nil
	}

	return dsn, drop, nil
}

// withDatabase заменяет имя базы в строке подключения вида postgres://...
func withDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", errors.New("pgtest: " + EnvDSN + " must be a URL like postgres://user:password@host:port/db")
	}
	u.Path = "/" + name
	return u.String(), nil
}
