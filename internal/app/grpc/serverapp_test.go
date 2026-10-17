package grpcapp_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	userv1 "github.com/alexgul25/protos/gen/go/user/v1"
	grpcapp "github.com/alexgul25/user-svc/internal/app/grpc"
	"github.com/alexgul25/user-svc/internal/domain/models"
	handlersgrpc "github.com/alexgul25/user-svc/internal/grpc/handlers"
)

const userID = "4f1c2d3e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"

// ----------------------------------------------------------------------------
// Заглушка сервиса
// ----------------------------------------------------------------------------

// serviceStub переопределяет только методы, нужные тестам этого пакета.
// Вызов остальных паникует на nil-интерфейсе: здесь они не вызываются.
type serviceStub struct {
	handlersgrpc.UserService

	login        func(ctx context.Context, email, password string) (string, error)
	getMyProfile func(ctx context.Context, userID string) (models.User, error)
	getFollowers func(ctx context.Context, userID string) ([]models.Follower, error)
}

func (s *serviceStub) Login(ctx context.Context, email, password string) (string, error) {
	return s.login(ctx, email, password)
}

func (s *serviceStub) GetMyProfile(ctx context.Context, userID string) (models.User, error) {
	return s.getMyProfile(ctx, userID)
}

func (s *serviceStub) GetFollowers(ctx context.Context, userID string) ([]models.Follower, error) {
	return s.getFollowers(ctx, userID)
}

// ----------------------------------------------------------------------------
// Вспомогательные функции
// ----------------------------------------------------------------------------

// syncBuffer - буфер для логов, безопасный для записи из горутин сервера.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newLogger возвращает JSON-логгер, пишущий в буфер.
func newLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// startServer собирает сервер так же, как сервис (все интерцепторы и health-check),
// запускает его в памяти (bufconn) и возвращает соединение с ним.
// После теста сервер останавливается через GracefulStop.
func startServer(t *testing.T, log *slog.Logger, svc handlersgrpc.UserService, servicesWithEmailHidden []string) *grpc.ClientConn {
	t.Helper()

	app := grpcapp.New(log, svc, 0, servicesWithEmailHidden)
	lis := bufconn.Listen(1 << 20)

	served := make(chan error, 1)
	go func() { served <- app.Serve(lis) }()
	t.Cleanup(func() {
		app.GracefulStop()
		if err := <-served; err != nil {
			t.Errorf("Serve returned error: %v", err)
		}
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return conn
}

// freePort находит свободный TCP-порт.
func freePort(t *testing.T) int {
	t.Helper()

	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}

// busyPort занимает порт на время теста.
func busyPort(t *testing.T) int {
	t.Helper()

	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", ":0")
	if err != nil {
		t.Fatalf("failed to occupy port: %v", err)
	}
	t.Cleanup(func() { l.Close() })

	return l.Addr().(*net.TCPAddr).Port
}

// ----------------------------------------------------------------------------
// Цепочка интерцепторов
// ----------------------------------------------------------------------------

func TestServer_HeadersReachHandlers(t *testing.T) {
	svc := &serviceStub{
		getMyProfile: func(_ context.Context, id string) (models.User, error) {
			return models.User{ID: id, DisplayName: "Alice"}, nil
		},
		getFollowers: func(context.Context, string) ([]models.Follower, error) {
			return []models.Follower{{ID: userID, DisplayName: "Bob", Email: "bob@example.com"}}, nil
		},
	}
	log, _ := newLogger()
	conn := startServer(t, log, svc, []string{"gateway-svc"})
	client := userv1.NewUserServiceClient(conn)

	t.Run("x-user-id", func(t *testing.T) {
		ctx := metadata.AppendToOutgoingContext(t.Context(), "x-service-name", "gateway-svc", "x-user-id", userID)

		resp, err := client.GetMyProfile(ctx, &emptypb.Empty{})
		if err != nil {
			t.Fatalf("GetMyProfile returned error: %v", err)
		}
		if resp.GetUserId() != userID {
			t.Errorf("user_id = %q, want %q", resp.GetUserId(), userID)
		}
	})

	// Список сервисов, от которых скрыт email, берётся из конфига и передаётся через grpcapp.New.
	for _, tt := range []struct {
		caller    string
		wantEmail string
	}{
		{caller: "gateway-svc", wantEmail: ""},
		{caller: "notify-svc", wantEmail: "bob@example.com"},
	} {
		t.Run("x-service-name="+tt.caller, func(t *testing.T) {
			ctx := metadata.AppendToOutgoingContext(t.Context(), "x-service-name", tt.caller)

			resp, err := client.GetFollowers(ctx, &userv1.GetFollowersRequest{UserId: userID})
			if err != nil {
				t.Fatalf("GetFollowers returned error: %v", err)
			}
			if got := resp.GetFollowers()[0].GetEmail(); got != tt.wantEmail {
				t.Errorf("email = %q, want %q", got, tt.wantEmail)
			}
		})
	}
}

func TestServer_RecoversFromPanic(t *testing.T) {
	var calls int
	var mu sync.Mutex
	svc := &serviceStub{
		login: func(context.Context, string, string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if calls == 1 {
				panic("unexpected nil pointer in business logic")
			}
			return "token", nil
		},
	}
	log, logs := newLogger()
	conn := startServer(t, log, svc, nil)
	client := userv1.NewUserServiceClient(conn)
	req := &userv1.LoginRequest{Email: "alice@example.com", Password: "password"}

	// Паника в обработчике превращается в Internal без подробностей.
	_, err := client.Login(t.Context(), req)
	if st := status.Convert(err); st.Code() != codes.Internal || st.Message() != "internal error" {
		t.Fatalf("got status (%v, %q), want (Internal, %q)", st.Code(), st.Message(), "internal error")
	}
	if !strings.Contains(logs.String(), "recovered from panic") {
		t.Errorf("panic was not logged, log: %s", logs.String())
	}

	// Сервер продолжает обслуживать запросы.
	resp, err := client.Login(t.Context(), req)
	if err != nil {
		t.Fatalf("second call returned error: %v", err)
	}
	if resp.GetAccessToken() != "token" {
		t.Errorf("access token = %q, want %q", resp.GetAccessToken(), "token")
	}
}

func TestServer_LogsCallsButNotHealthChecks(t *testing.T) {
	svc := &serviceStub{
		login: func(context.Context, string, string) (string, error) { return "token", nil },
	}
	log, logs := newLogger()
	conn := startServer(t, log, svc, nil)

	ctx := metadata.AppendToOutgoingContext(t.Context(), "x-service-name", "gateway-svc")
	if _, err := userv1.NewUserServiceClient(conn).Login(ctx, &userv1.LoginRequest{Email: "a@b.c", Password: "p"}); err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if _, err := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("health check returned error: %v", err)
	}

	out := logs.String()
	if !strings.Contains(out, `"grpc.method":"Login"`) || !strings.Contains(out, `"x-service-name":"gateway-svc"`) {
		t.Errorf("Login call was not logged with service name, log: %s", out)
	}
	if strings.Contains(out, "grpc.health.v1.Health") {
		t.Errorf("health check must not be logged, log: %s", out)
	}
}

// ----------------------------------------------------------------------------
// Health-check и остановка
// ----------------------------------------------------------------------------

func TestServer_HealthCheck(t *testing.T) {
	log, _ := newLogger()
	conn := startServer(t, log, &serviceStub{}, nil)

	resp, err := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check returned error: %v", err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("status = %v, want SERVING", resp.GetStatus())
	}
}

func TestServer_GracefulStopReportsNotServing(t *testing.T) {
	log, _ := newLogger()
	app := grpcapp.New(log, &serviceStub{}, 0, nil)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = app.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer conn.Close()

	// Watch - поток, в который сервер присылает каждое изменение статуса.
	watchCtx, cancelWatch := context.WithCancel(t.Context())
	defer cancelWatch()
	stream, err := healthpb.NewHealthClient(conn).Watch(watchCtx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("failed to watch health: %v", err)
	}
	if resp, err := stream.Recv(); err != nil || resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("initial status = (%v, %v), want SERVING", resp.GetStatus(), err)
	}

	stopped := make(chan struct{})
	go func() {
		app.GracefulStop()
		close(stopped)
	}()

	// При остановке сервер сразу сообщает, что больше не принимает запросы.
	if resp, err := stream.Recv(); err != nil || resp.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status after stop = (%v, %v), want NOT_SERVING", resp.GetStatus(), err)
	}

	// GracefulStop ждёт завершения всех открытых вызовов, включая поток Watch.
	// Пока клиент его не закроет, остановка не завершится.
	select {
	case <-stopped:
		t.Fatal("GracefulStop returned while a watch stream was still open")
	case <-time.After(100 * time.Millisecond):
	}

	cancelWatch()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("GracefulStop did not return after the watch stream was closed")
	}
}

func TestServe_AfterStop(t *testing.T) {
	log, _ := newLogger()
	app := grpcapp.New(log, &serviceStub{}, 0, nil)
	app.GracefulStop()

	err := app.Serve(bufconn.Listen(1 << 20))
	if !errors.Is(err, grpc.ErrServerStopped) {
		t.Errorf("got error %v, want %v", err, grpc.ErrServerStopped)
	}
}

// ----------------------------------------------------------------------------
// Run и MustRun: запуск на настоящем TCP-порту
// ----------------------------------------------------------------------------

func TestRun_ServesOnConfiguredPort(t *testing.T) {
	port := freePort(t)
	log, logs := newLogger()
	app := grpcapp.New(log, &serviceStub{}, port, nil)

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer conn.Close()

	// WaitForReady: клиент ждёт, пока сервер начнёт слушать порт.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("health check returned error: %v", err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("status = %v, want SERVING", resp.GetStatus())
	}
	if !strings.Contains(logs.String(), fmt.Sprintf(":%d", port)) {
		t.Errorf("started address is not logged, log: %s", logs.String())
	}

	app.GracefulStop()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error after GracefulStop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after GracefulStop")
	}
}

func TestRun_PortIsBusy(t *testing.T) {
	log, _ := newLogger()
	app := grpcapp.New(log, &serviceStub{}, busyPort(t), nil)

	if err := app.Run(); err == nil {
		t.Fatal("expected error for busy port, got nil")
	}
}

func TestMustRun_PanicsOnError(t *testing.T) {
	log, _ := newLogger()
	app := grpcapp.New(log, &serviceStub{}, busyPort(t), nil)

	defer func() {
		if recover() == nil {
			t.Error("MustRun did not panic")
		}
	}()
	app.MustRun()
}
