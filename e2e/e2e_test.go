//go:build integration

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	userv1 "github.com/alexgul25/protos/gen/go/user/v1"
)

// connect подключается к сервису и ждёт, пока он начнёт отвечать.
func connect(t *testing.T, e env) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient("127.0.0.1:"+e["GRPCSERVER_PORT"], grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
	if err != nil || resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("service is not ready: status %v, error %v", resp.GetStatus(), err)
	}
	return conn
}

// requireJSONLogs проверяет, что в окружении prod каждая строка вывода - JSON-запись лога.
func requireJSONLogs(t *testing.T, output string) {
	t.Helper()

	for line := range strings.Lines(strings.TrimSpace(output)) {
		if !json.Valid([]byte(line)) {
			t.Errorf("log line is not JSON: %q", line)
		}
	}
}

// ----------------------------------------------------------------------------
// Мигратор
// ----------------------------------------------------------------------------

func TestMigrator(t *testing.T) {
	e := newEnv(t)

	code, out := runToExit(t, migratorBin, e)
	if code != 0 {
		t.Fatalf("migrator exited with code %d:\n%s", code, out)
	}
	if got := strings.Count(out, "migration applied"); got != 4 {
		t.Errorf("applied %d migrations, want 4:\n%s", got, out)
	}

	// Повторный запуск при каждом деплое ничего не меняет.
	code, out = runToExit(t, migratorBin, e)
	if code != 0 || strings.Contains(out, "migration applied") || !strings.Contains(out, "migrations applied successfully") {
		t.Errorf("second run: exit code %d, output:\n%s", code, out)
	}
}

func TestMigrator_Errors(t *testing.T) {
	e := newEnv(t)

	tests := []struct {
		name    string
		env     env
		wantOut string
	}{
		{name: "missing config", env: e.with("DB_PASSWORD", ""), wantOut: "DB_PASSWORD"},
		{name: "database unavailable", env: e.with("DB_PORT", "1"), wantOut: "failed to up migrations"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out := runToExit(t, migratorBin, tt.env)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("output does not mention %q:\n%s", tt.wantOut, out)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Сервис
// ----------------------------------------------------------------------------

func TestService_Lifecycle(t *testing.T) {
	// Сервис останавливается по SIGTERM (docker stop, Kubernetes) и по SIGINT (Ctrl+C в терминале).
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			e := newEnv(t)
			migrate(t, e)

			svc := start(t, serviceBin, e)
			client := userv1.NewUserServiceClient(connect(t, e))
			gateway := metadata.AppendToOutgoingContext(t.Context(), "x-service-name", "gateway-svc")

			// Короткий сценарий: настройки из окружения, база и JWT работают вместе.
			user, err := client.Register(gateway, &userv1.RegisterRequest{DisplayName: "Alice", Email: "alice@example.com", Password: "password"})
			if err != nil {
				t.Fatalf("Register returned error: %v", err)
			}
			if _, err := client.Login(gateway, &userv1.LoginRequest{Email: "alice@example.com", Password: "password"}); err != nil {
				t.Fatalf("Login returned error: %v", err)
			}
			profile, err := client.GetMyProfile(metadata.AppendToOutgoingContext(gateway, "x-user-id", user.GetUserId()), &emptypb.Empty{})
			if err != nil || profile.GetEmail() != "alice@example.com" {
				t.Fatalf("GetMyProfile = (%v, %v)", profile, err)
			}

			svc.signal(t, sig)
			if code := svc.wait(t, 15*time.Second); code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}

			out := svc.output.String()
			for _, msg := range []string{"grpc server started", "grpc server gracefully stopped"} {
				if !strings.Contains(out, msg) {
					t.Errorf("output does not contain %q", msg)
				}
			}
			requireJSONLogs(t, out)
		})
	}
}

func TestService_GracefulTimeout(t *testing.T) {
	e := newEnv(t).with("GRACEFUL_TIMEOUT", "1s")
	migrate(t, e)

	svc := start(t, serviceBin, e)
	conn := connect(t, e)

	// Поток Watch длится, пока его не закроет клиент: без таймаута остановка ждала бы бесконечно.
	stream, err := healthpb.NewHealthClient(conn).Watch(t.Context(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("failed to watch health: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("failed to receive health status: %v", err)
	}

	started := time.Now()
	svc.signal(t, syscall.SIGTERM)
	if code := svc.wait(t, 15*time.Second); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	// Сервис ждёт GRACEFUL_TIMEOUT и затем закрывает соединения принудительно.
	if elapsed := time.Since(started); elapsed < time.Second || elapsed > 5*time.Second {
		t.Errorf("service stopped in %v, want about GRACEFUL_TIMEOUT (1s)", elapsed)
	}
	if out := svc.output.String(); !strings.Contains(out, "graceful stop timed out") {
		t.Errorf("forced stop is not logged:\n%s", out)
	}
}

func TestService_StartupErrors(t *testing.T) {
	e := newEnv(t)
	migrate(t, e)

	// Порт, занятый другим процессом.
	var lc net.ListenConfig
	busy, err := lc.Listen(t.Context(), "tcp", ":0")
	if err != nil {
		t.Fatalf("failed to occupy port: %v", err)
	}
	defer busy.Close()
	busyPort := fmt.Sprint(busy.Addr().(*net.TCPAddr).Port)

	tests := []struct {
		name    string
		env     env
		wantOut string
	}{
		{name: "missing JWT secret", env: e.with("JWT_SECRET", ""), wantOut: "JWT_SECRET"},
		{name: "invalid port", env: e.with("GRPCSERVER_PORT", "port"), wantOut: "GRPCSERVER_PORT"},
		{name: "database unavailable", env: e.with("DB_PORT", "1"), wantOut: "failed to ping storage"},
		{name: "port is busy", env: e.with("GRPCSERVER_PORT", busyPort), wantOut: "address already in use"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Сервис сразу завершается с кодом 1 и понятной ошибкой, а не падает с паникой
			// и не висит без возможности обслуживать запросы.
			code, out := runToExit(t, serviceBin, tt.env)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("output does not mention %q:\n%s", tt.wantOut, out)
			}
			if strings.Contains(out, "panic") {
				t.Errorf("service panicked:\n%s", out)
			}
		})
	}
}
