//go:build integration

package postgresql_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/alexgul25/user-svc/internal/storage/postgresql"
	"github.com/alexgul25/user-svc/internal/testutil/pgtest"
	"github.com/alexgul25/user-svc/migrations"
)

// Отдельная база, созданная для тестов этого пакета: строка подключения и соединение.
var (
	testDSN string
	testDB  *sql.DB
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run отделён от TestMain, чтобы отложенные вызовы выполнились до os.Exit.
func run(m *testing.M) int {
	ctx := context.Background()

	dsn, drop, err := pgtest.NewDatabase(ctx, "user_svc_storage")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		if err := drop(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	storage, err := postgresql.NewStorage(dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer storage.Close()

	// Схему создают те же миграции, что применяются в проде.
	if _, err := migrations.Up(ctx, storage.DB()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	testDSN, testDB = dsn, storage.DB()
	return m.Run()
}

// newUserStorage очищает таблицы и возвращает хранилище: каждый тест начинает с пустой базы.
// Тесты пакета работают с одной базой, поэтому не используют t.Parallel.
func newUserStorage(t *testing.T) *postgresql.UserStorage {
	t.Helper()

	if _, err := testDB.ExecContext(t.Context(), "TRUNCATE users CASCADE"); err != nil {
		t.Fatalf("failed to truncate tables: %v", err)
	}
	return postgresql.NewUserStorage(testDB)
}
