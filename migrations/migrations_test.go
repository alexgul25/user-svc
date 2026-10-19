//go:build integration

package migrations_test

import (
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/alexgul25/user-svc/internal/testutil/pgtest"
	"github.com/alexgul25/user-svc/migrations"
)

// latestVersion - номер последней миграции. При добавлении миграции его нужно увеличить.
const latestVersion = 4

// ----------------------------------------------------------------------------
// Вспомогательные функции
// ----------------------------------------------------------------------------

// newDB создаёт для теста отдельную пустую базу и возвращает соединение с ней.
func newDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn, drop, err := pgtest.NewDatabase(t.Context(), "user_svc_migrations")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := drop(); err != nil {
			t.Error(err)
		}
	})

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

// newProvider возвращает goose-провайдер со встроенными миграциями.
func newProvider(t *testing.T, db *sql.DB) *goose.Provider {
	t.Helper()

	provider, err := migrations.NewProvider(db)
	if err != nil {
		t.Fatalf("NewProvider returned error: %v", err)
	}
	return provider
}

// exec выполняет SQL и роняет тест при ошибке.
func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()

	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("failed to execute %q: %v", query, err)
	}
}

// insertUser добавляет пользователя напрямую через SQL и возвращает его ID.
func insertUser(t *testing.T, db *sql.DB, email string) string {
	t.Helper()

	var id string
	err := db.QueryRowContext(t.Context(),
		"INSERT INTO users (display_name, email, password_hash) VALUES ('User', $1, 'hash') RETURNING id", email,
	).Scan(&id)
	if err != nil {
		t.Fatalf("failed to insert user %q: %v", email, err)
	}
	return id
}

// tables возвращает таблицы схемы public.
func tables(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(),
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name")
	if err != nil {
		t.Fatalf("failed to list tables: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to list tables: %v", err)
	}
	return names
}

// pgCode возвращает код ошибки PostgreSQL или пустую строку.
func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// ----------------------------------------------------------------------------
// Тесты
// ----------------------------------------------------------------------------

func TestUp_AppliesAllMigrations(t *testing.T) {
	db := newDB(t)

	results, err := migrations.Up(t.Context(), db)
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}
	if len(results) != latestVersion {
		t.Errorf("applied %d migrations, want %d", len(results), latestVersion)
	}

	// Повторный запуск ничего не делает: мигратор можно безопасно запускать при каждом деплое.
	results, err = migrations.Up(t.Context(), db)
	if err != nil {
		t.Fatalf("second Up returned error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("second Up applied %d migrations, want 0", len(results))
	}

	version, err := newProvider(t, db).GetDBVersion(t.Context())
	if err != nil {
		t.Fatalf("GetDBVersion returned error: %v", err)
	}
	if version != latestVersion {
		t.Errorf("database version = %d, want %d", version, latestVersion)
	}
}

func TestDownAndUpAgain(t *testing.T) {
	db := newDB(t)
	provider := newProvider(t, db)

	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	// Каждая миграция откатывается: после отката всех остаётся только служебная таблица goose.
	if _, err := provider.DownTo(t.Context(), 0); err != nil {
		t.Fatalf("DownTo(0) returned error: %v", err)
	}
	if got := tables(t, db); !slices.Equal(got, []string{"goose_db_version"}) {
		t.Errorf("tables after rollback = %q, want only goose_db_version", got)
	}

	// После отката миграции снова применяются: Down-части не оставляют мусора.
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatalf("Up after rollback returned error: %v", err)
	}
	if got := tables(t, db); !slices.Equal(got, []string{"goose_db_version", "subscriptions", "users"}) {
		t.Errorf("tables after second Up = %q", got)
	}
}

func TestIndexes(t *testing.T) {
	db := newDB(t)
	if _, err := migrations.Up(t.Context(), db); err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	rows, err := db.QueryContext(t.Context(),
		"SELECT indexname FROM pg_indexes WHERE schemaname = 'public' AND tablename IN ('users', 'subscriptions') ORDER BY indexname")
	if err != nil {
		t.Fatalf("failed to list indexes: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan index name: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to list indexes: %v", err)
	}

	want := []string{
		"idx_subscriptions_followee", // поиск подписчиков пользователя
		"subscriptions_pkey",
		"users_email_lower_key", // уникальность email без учёта регистра
		"users_pkey",
	}
	if !slices.Equal(got, want) {
		t.Errorf("indexes = %q, want %q", got, want)
	}
}

func TestMigration003_CaseInsensitiveEmailDuplicates(t *testing.T) {
	db := newDB(t)
	provider := newProvider(t, db)
	if _, err := provider.UpTo(t.Context(), 2); err != nil {
		t.Fatalf("UpTo(2) returned error: %v", err)
	}

	// До миграции 003 можно было зарегистрировать один адрес в разном регистре.
	insertUser(t, db, "alice@example.com")
	duplicate := insertUser(t, db, "Alice@Example.com")

	// С такими данными уникальный индекс не создаётся, и миграция не применяется.
	_, err := provider.Up(t.Context())
	if code := pgCode(err); code != pgerrcode.UniqueViolation {
		t.Fatalf("got error %v (code %q), want unique violation", err, code)
	}
	// Миграция выполняется в транзакции: база остаётся в прежнем состоянии.
	if version, err := provider.GetDBVersion(t.Context()); err != nil || version != 2 {
		t.Fatalf("database version = %d (%v), want 2", version, err)
	}

	// После ручного разбора дубликатов миграции применяются.
	exec(t, db, "DELETE FROM users WHERE id = $1", duplicate)
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatalf("Up after cleanup returned error: %v", err)
	}

	// Теперь база сама не даёт создать дубликат в другом регистре.
	_, err = db.ExecContext(t.Context(), "INSERT INTO users (display_name, email, password_hash) VALUES ('A', 'ALICE@EXAMPLE.COM', 'h')")
	if code := pgCode(err); code != pgerrcode.UniqueViolation {
		t.Errorf("got error %v (code %q), want unique violation", err, code)
	}
}

func TestMigration004_RemovesAndForbidsSelfSubscriptions(t *testing.T) {
	db := newDB(t)
	provider := newProvider(t, db)
	if _, err := provider.UpTo(t.Context(), 3); err != nil {
		t.Fatalf("UpTo(3) returned error: %v", err)
	}

	alice := insertUser(t, db, "alice@example.com")
	bob := insertUser(t, db, "bob@example.com")
	exec(t, db, "INSERT INTO subscriptions (follower_id, followee_id) VALUES ($1, $1), ($2, $1)", alice, bob)

	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	// Ошибочная самоподписка удалена, обычная подписка осталась.
	var self, total int
	err := db.QueryRowContext(t.Context(),
		"SELECT count(*) FILTER (WHERE follower_id = followee_id), count(*) FROM subscriptions").Scan(&self, &total)
	if err != nil {
		t.Fatalf("failed to count subscriptions: %v", err)
	}
	if self != 0 || total != 1 {
		t.Errorf("self subscriptions = %d, total = %d; want 0 and 1", self, total)
	}

	// Новая самоподписка запрещена ограничением.
	_, err = db.ExecContext(t.Context(), "INSERT INTO subscriptions (follower_id, followee_id) VALUES ($1, $1)", bob)
	if code := pgCode(err); code != pgerrcode.CheckViolation {
		t.Errorf("got error %v (code %q), want check violation", err, code)
	}
}
