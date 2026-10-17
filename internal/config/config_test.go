package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/alexgul25/user-svc/internal/config"
)

// requiredEnv - обязательные переменные с корректными значениями.
// Порядок важен для читаемых имён подтестов, поэтому это срез, а не map.
var requiredEnv = []struct{ key, value string }{
	{"ENV", "local"},
	{"DB_USER", "user"},
	{"DB_PASSWORD", "secret"},
	{"DB_NAME", "users"},
	{"DB_HOST", "localhost"},
	{"DB_PORT", "5432"},
	{"JWT_SECRET", "jwt-secret"},
}

// optionalEnv - необязательные переменные, у которых есть значения по умолчанию.
var optionalEnv = []string{
	"GRPCSERVER_PORT",
	"GRACEFUL_TIMEOUT",
	"SERVICES_WITH_EMAIL_HIDDEN",
	"JWT_ACCESS_TTL",
}

// ----------------------------------------------------------------------------
// Вспомогательные функции
// ----------------------------------------------------------------------------

// isolate готовит чистое окружение для теста:
//   - убирает все переменные конфига. Пустое значение не подходит: envconfig и godotenv
//     считают такую переменную заданной. t.Setenv запоминает исходное значение
//     и вернёт его после теста, а os.Unsetenv удаляет переменную на время теста;
//   - переходит во временную папку, чтобы godotenv не нашёл посторонний .env.
//
// t.Setenv и t.Chdir несовместимы с t.Parallel: тесты пакета идут последовательно.
func isolate(t *testing.T) {
	t.Helper()

	for _, e := range requiredEnv {
		t.Setenv(e.key, "")
		os.Unsetenv(e.key)
	}
	for _, key := range optionalEnv {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	t.Chdir(t.TempDir())
}

// setRequiredEnv задаёт все обязательные переменные корректными значениями.
func setRequiredEnv(t *testing.T) {
	t.Helper()

	for _, e := range requiredEnv {
		t.Setenv(e.key, e.value)
	}
}

// writeDotEnv создаёт .env в текущей (временной) папке.
func writeDotEnv(t *testing.T, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(".", ".env"), []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}
}

// mustLoad загружает конфиг и роняет тест при ошибке.
func mustLoad(t *testing.T) *config.Config {
	t.Helper()

	cfg, err := config.LoadUserService()
	if err != nil {
		t.Fatalf("LoadUserService returned error: %v", err)
	}
	return cfg
}

// ----------------------------------------------------------------------------
// LoadUserService
// ----------------------------------------------------------------------------

func TestLoadUserService_Defaults(t *testing.T) {
	isolate(t)
	setRequiredEnv(t)

	cfg := mustLoad(t)

	if cfg.Env != "local" {
		t.Errorf("Env = %q, want %q", cfg.Env, "local")
	}

	wantDB := config.DatabaseConfig{User: "user", Password: "secret", DbName: "users", Host: "localhost", Port: 5432}
	if cfg.Database != wantDB {
		t.Errorf("Database = %+v, want %+v", cfg.Database, wantDB)
	}

	if cfg.JWT.Secret != "jwt-secret" {
		t.Errorf("JWT.Secret = %q, want %q", cfg.JWT.Secret, "jwt-secret")
	}

	// Значения по умолчанию из тегов default.
	if cfg.GRPCServer.Port != 50051 {
		t.Errorf("GRPCServer.Port = %d, want 50051", cfg.GRPCServer.Port)
	}
	if cfg.GRPCServer.GracefulTimeout != 10*time.Second {
		t.Errorf("GRPCServer.GracefulTimeout = %v, want 10s", cfg.GRPCServer.GracefulTimeout)
	}
	if cfg.JWT.TokenTTL != 24*time.Hour {
		t.Errorf("JWT.TokenTTL = %v, want 24h", cfg.JWT.TokenTTL)
	}
	if len(cfg.GRPCServer.ServicesWithEmailHidden) != 0 {
		t.Errorf("ServicesWithEmailHidden = %q, want empty", cfg.GRPCServer.ServicesWithEmailHidden)
	}
}

func TestLoadUserService_OptionalOverrides(t *testing.T) {
	isolate(t)
	setRequiredEnv(t)
	t.Setenv("GRPCSERVER_PORT", "6000")
	t.Setenv("GRACEFUL_TIMEOUT", "3s")
	t.Setenv("JWT_ACCESS_TTL", "48h")

	cfg := mustLoad(t)

	if cfg.GRPCServer.Port != 6000 {
		t.Errorf("GRPCServer.Port = %d, want 6000", cfg.GRPCServer.Port)
	}
	if cfg.GRPCServer.GracefulTimeout != 3*time.Second {
		t.Errorf("GRPCServer.GracefulTimeout = %v, want 3s", cfg.GRPCServer.GracefulTimeout)
	}
	if cfg.JWT.TokenTTL != 48*time.Hour {
		t.Errorf("JWT.TokenTTL = %v, want 48h", cfg.JWT.TokenTTL)
	}
}

func TestLoadUserService_MissingRequired(t *testing.T) {
	for _, missing := range requiredEnv {
		t.Run(missing.key, func(t *testing.T) {
			isolate(t)
			setRequiredEnv(t)
			os.Unsetenv(missing.key)

			cfg, err := config.LoadUserService()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			// Ошибка должна называть переменную, иначе по логу не понять, что исправлять.
			if !strings.Contains(err.Error(), missing.key) {
				t.Errorf("error %q does not mention %s", err, missing.key)
			}
			if cfg != nil {
				t.Errorf("expected nil config on error, got %+v", cfg)
			}
		})
	}
}

func TestLoadUserService_InvalidValues(t *testing.T) {
	tests := []struct{ key, value string }{
		{"DB_PORT", "not-a-number"},
		{"GRPCSERVER_PORT", "not-a-number"},
		{"GRACEFUL_TIMEOUT", "soon"},
		{"JWT_ACCESS_TTL", "1day"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			isolate(t)
			setRequiredEnv(t)
			t.Setenv(tt.key, tt.value)

			cfg, err := config.LoadUserService()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Errorf("error %q does not mention %s", err, tt.key)
			}
			if cfg != nil {
				t.Errorf("expected nil config on error, got %+v", cfg)
			}
		})
	}
}

func TestLoadUserService_ServicesWithEmailHidden(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "one service", raw: "gateway-svc", want: []string{"gateway-svc"}},
		{name: "several services", raw: "gateway-svc,notify-svc", want: []string{"gateway-svc", "notify-svc"}},
		{
			// Пробел после запятой не должен ломать сравнение имён:
			// иначе email не будет скрыт для " notify-svc".
			name: "spaces around names",
			raw:  "gateway-svc, notify-svc",
			want: []string{"gateway-svc", "notify-svc"},
		},
		{name: "empty items", raw: " gateway-svc ,, notify-svc , ", want: []string{"gateway-svc", "notify-svc"}},
		{name: "only separators", raw: " , ,", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			setRequiredEnv(t)
			t.Setenv("SERVICES_WITH_EMAIL_HIDDEN", tt.raw)

			got := mustLoad(t).GRPCServer.ServicesWithEmailHidden
			if !slices.Equal(got, tt.want) {
				t.Errorf("ServicesWithEmailHidden = %q, want %q", got, tt.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Файл .env
// ----------------------------------------------------------------------------

func TestLoadUserService_DotEnv(t *testing.T) {
	isolate(t)
	writeDotEnv(t, strings.Join([]string{
		"ENV=dev",
		"DB_USER=file-user",
		"DB_PASSWORD=file-secret",
		"DB_NAME=file-db",
		"DB_HOST=file-host",
		"DB_PORT=6432",
		"JWT_SECRET=file-jwt",
		"SERVICES_WITH_EMAIL_HIDDEN=gateway-svc",
	}, "\n"))

	cfg := mustLoad(t)

	wantDB := config.DatabaseConfig{User: "file-user", Password: "file-secret", DbName: "file-db", Host: "file-host", Port: 6432}
	if cfg.Env != "dev" || cfg.Database != wantDB || cfg.JWT.Secret != "file-jwt" {
		t.Errorf("config is not loaded from .env: %+v", cfg)
	}
	if !slices.Equal(cfg.GRPCServer.ServicesWithEmailHidden, []string{"gateway-svc"}) {
		t.Errorf("ServicesWithEmailHidden = %q, want [gateway-svc]", cfg.GRPCServer.ServicesWithEmailHidden)
	}
}

func TestLoadUserService_EnvOverridesDotEnv(t *testing.T) {
	isolate(t)
	setRequiredEnv(t)
	t.Setenv("DB_HOST", "from-env")
	writeDotEnv(t, "DB_HOST=from-file\n")

	if got := mustLoad(t).Database.Host; got != "from-env" {
		t.Errorf("Database.Host = %q, want value from environment %q", got, "from-env")
	}
}

func TestLoadUserService_InvalidDotEnv(t *testing.T) {
	isolate(t)
	setRequiredEnv(t)
	// Незакрытая кавычка: godotenv не может разобрать файл.
	writeDotEnv(t, "DB_HOST=\"unterminated\n")

	cfg, err := config.LoadUserService()
	if err == nil {
		t.Fatal("expected error for broken .env, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
	}
}

// ----------------------------------------------------------------------------
// DSN
// ----------------------------------------------------------------------------

func TestDatabaseConfig_DSN(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.DatabaseConfig
	}{
		{
			name: "simple values",
			cfg:  config.DatabaseConfig{User: "user", Password: "secret", DbName: "users", Host: "localhost", Port: 5432},
		},
		{
			// Спецсимволы URL в пароле должны экранироваться, а не ломать строку подключения.
			name: "special characters in password",
			cfg:  config.DatabaseConfig{User: "user", Password: "p@ss:w/rd?#%", DbName: "users", Host: "localhost", Port: 5432},
		},
		{
			name: "special characters in user and database",
			cfg:  config.DatabaseConfig{User: "us er@corp", Password: "secret", DbName: "my db", Host: "db.example.com", Port: 6432},
		},
		{
			name: "IPv6 host",
			cfg:  config.DatabaseConfig{User: "user", Password: "secret", DbName: "users", Host: "::1", Port: 5432},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := tt.cfg.DSN()

			// Строку разбирает тот же драйвер, что и сервис: значения должны вернуться без искажений.
			parsed, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("pgconn.ParseConfig(%q) returned error: %v", dsn, err)
			}

			if parsed.User != tt.cfg.User {
				t.Errorf("user = %q, want %q", parsed.User, tt.cfg.User)
			}
			if parsed.Password != tt.cfg.Password {
				t.Errorf("password = %q, want %q", parsed.Password, tt.cfg.Password)
			}
			if parsed.Database != tt.cfg.DbName {
				t.Errorf("database = %q, want %q", parsed.Database, tt.cfg.DbName)
			}
			if parsed.Host != tt.cfg.Host {
				t.Errorf("host = %q, want %q", parsed.Host, tt.cfg.Host)
			}
			if int(parsed.Port) != tt.cfg.Port {
				t.Errorf("port = %d, want %d", parsed.Port, tt.cfg.Port)
			}
		})
	}
}

func TestDatabaseConfig_DSN_Format(t *testing.T) {
	cfg := config.DatabaseConfig{User: "user", Password: "secret", DbName: "users", Host: "localhost", Port: 5432}

	const want = "postgres://user:secret@localhost:5432/users"
	if got := cfg.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}
