//go:build integration

package app_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	userv1 "github.com/alexgul25/protos/gen/go/user/v1"
	"github.com/alexgul25/user-svc/internal/app"
	"github.com/alexgul25/user-svc/internal/config"
	jwtmanager "github.com/alexgul25/user-svc/internal/lib/jwt"
	"github.com/alexgul25/user-svc/internal/storage/postgresql"
	"github.com/alexgul25/user-svc/internal/testutil/pgtest"
	"github.com/alexgul25/user-svc/migrations"
)

const jwtSecret = "test-secret"

// Временная база, созданная для тестов пакета: настройки подключения и соединение.
var (
	testDBConfig config.DatabaseConfig
	testDB       *sql.DB
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run отделён от TestMain, чтобы отложенные вызовы выполнились до os.Exit.
func run(m *testing.M) int {
	ctx := context.Background()

	dsn, drop, err := pgtest.NewDatabase(ctx, "user_svc_app")
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

	if _, err := migrations.Up(ctx, storage.DB()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if testDBConfig, err = databaseConfig(dsn); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	testDB = storage.DB()

	return m.Run()
}

// databaseConfig раскладывает строку подключения на поля конфига, как их задают переменные окружения.
func databaseConfig(dsn string) (config.DatabaseConfig, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return config.DatabaseConfig{}, err
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return config.DatabaseConfig{}, fmt.Errorf("invalid port in %s: %w", pgtest.EnvDSN, err)
	}
	password, _ := u.User.Password()

	return config.DatabaseConfig{
		User:     u.User.Username(),
		Password: password,
		DbName:   strings.TrimPrefix(u.Path, "/"),
		Host:     u.Hostname(),
		Port:     port,
	}, nil
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

// scenario хранит клиент и вспомогательные функции для пользовательского сценария.
type scenario struct {
	t      *testing.T
	client userv1.UserServiceClient
}

// as - вызов от имени сервиса, а если задан userID - и от имени пользователя.
func (s *scenario) as(service, userID string) context.Context {
	md := []string{"x-service-name", service}
	if userID != "" {
		md = append(md, "x-user-id", userID)
	}
	return metadata.AppendToOutgoingContext(s.t.Context(), md...)
}

// register регистрирует пользователя и роняет тест при ошибке.
func (s *scenario) register(name, email string) *userv1.RegisterResponse {
	s.t.Helper()

	resp, err := s.client.Register(s.as("gateway-svc", ""), &userv1.RegisterRequest{DisplayName: name, Email: email, Password: "password"})
	if err != nil {
		s.t.Fatalf("Register(%q) returned error: %v", email, err)
	}
	return resp
}

// requireCode проверяет код ошибки.
func (s *scenario) requireCode(step string, err error, want codes.Code) {
	s.t.Helper()

	if got := status.Code(err); got != want {
		s.t.Fatalf("%s: got code %v (%v), want %v", step, got, err, want)
	}
}

// followers возвращает пары "имя:email" подписчиков в алфавитном порядке.
func (s *scenario) followers(caller, userID string) []string {
	s.t.Helper()

	resp, err := s.client.GetFollowers(s.as(caller, ""), &userv1.GetFollowersRequest{UserId: userID})
	if err != nil {
		s.t.Fatalf("GetFollowers as %s returned error: %v", caller, err)
	}

	var result []string
	for _, f := range resp.GetFollowers() {
		result = append(result, f.GetDisplayName()+":"+f.GetEmail())
	}
	slices.Sort(result)
	return result
}

// TestApp_UserScenario собирает приложение так же, как main: настоящая бизнес-логика,
// настоящая база и gRPC-сервер на TCP-порту. Сценарий проходит через все слои сразу.
func TestApp_UserScenario(t *testing.T) {
	// Пустая база: тест можно запускать повторно (go test -count=N).
	if _, err := testDB.ExecContext(t.Context(), "TRUNCATE users CASCADE"); err != nil {
		t.Fatalf("failed to truncate tables: %v", err)
	}

	cfg := &config.Config{
		Env:      "local",
		Database: testDBConfig,
		GRPCServer: config.GRPCServerConfig{
			Port:                    freePort(t),
			ServicesWithEmailHidden: []string{"gateway-svc"},
		},
		JWT: config.JWTConfig{Secret: jwtSecret, TokenTTL: time.Hour},
	}

	application, err := app.New(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	serverErr := make(chan error, 1)
	go func() { serverErr <- application.RunServer() }()

	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", cfg.GRPCServer.Port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer conn.Close()

	// Ждём, пока сервер начнёт слушать порт.
	waitCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	health, err := healthpb.NewHealthClient(conn).Check(waitCtx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
	if err != nil || health.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health = (%v, %v), want SERVING", health.GetStatus(), err)
	}

	s := &scenario{t: t, client: userv1.NewUserServiceClient(conn)}

	// Регистрация. Тот же email в другом регистре - тот же пользователь.
	alice := s.register("Alice", "alice@example.com")
	bob := s.register("Bob", "bob@example.com")
	carol := s.register("Carol", "carol@example.com")
	_, err = s.client.Register(s.as("gateway-svc", ""), &userv1.RegisterRequest{DisplayName: "A", Email: "ALICE@example.com", Password: "password"})
	s.requireCode("Register duplicate", err, codes.AlreadyExists)

	// Вход: токен подписан секретом сервиса и содержит ID пользователя.
	login, err := s.client.Login(s.as("gateway-svc", ""), &userv1.LoginRequest{Email: "Alice@Example.com", Password: "password"})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	var claims jwtmanager.TokenClaims
	if _, err := jwt.ParseWithClaims(login.GetAccessToken(), &claims, func(*jwt.Token) (any, error) {
		return []byte(jwtSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()})); err != nil {
		t.Fatalf("token is not valid: %v", err)
	}
	if claims.UserID != alice.GetUserId() {
		t.Errorf("token user_id = %q, want %q", claims.UserID, alice.GetUserId())
	}
	_, err = s.client.Login(s.as("gateway-svc", ""), &userv1.LoginRequest{Email: "alice@example.com", Password: "wrong"})
	s.requireCode("Login with wrong password", err, codes.InvalidArgument)

	// Профиль.
	profile, err := s.client.GetMyProfile(s.as("gateway-svc", alice.GetUserId()), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMyProfile returned error: %v", err)
	}
	if profile.GetEmail() != "alice@example.com" || profile.GetDisplayName() != "Alice" {
		t.Errorf("unexpected profile: %v", profile)
	}

	// Поиск по началу имени.
	found, err := s.client.FindUsersByDisplayName(s.as("gateway-svc", bob.GetUserId()), &userv1.FindUsersByDisplayNameRequest{SearchQuery: "al"})
	if err != nil {
		t.Fatalf("FindUsersByDisplayName returned error: %v", err)
	}
	if len(found.GetUsers()) != 1 || found.GetUsers()[0].GetUserId() != alice.GetUserId() {
		t.Errorf("search result = %v, want only Alice", found.GetUsers())
	}

	// Подписки и ошибки подписки.
	for _, follower := range []*userv1.RegisterResponse{bob, carol} {
		_, err := s.client.Subscribe(s.as("gateway-svc", follower.GetUserId()), &userv1.SubscribeRequest{FolloweeId: alice.GetUserId()})
		if err != nil {
			t.Fatalf("Subscribe %s -> Alice returned error: %v", follower.GetDisplayName(), err)
		}
	}
	_, err = s.client.Subscribe(s.as("gateway-svc", bob.GetUserId()), &userv1.SubscribeRequest{FolloweeId: alice.GetUserId()})
	s.requireCode("Subscribe twice", err, codes.InvalidArgument)
	_, err = s.client.Subscribe(s.as("gateway-svc", alice.GetUserId()), &userv1.SubscribeRequest{FolloweeId: strings.ToUpper(alice.GetUserId())})
	s.requireCode("Subscribe to yourself", err, codes.InvalidArgument)
	_, err = s.client.Subscribe(s.as("gateway-svc", alice.GetUserId()), &userv1.SubscribeRequest{FolloweeId: "9b8a7c6d-5e4f-4a3b-9c2d-1e0f2a3b4c5d"})
	s.requireCode("Subscribe to missing user", err, codes.NotFound)

	// Подписчики: Gateway получает их без email, внутренний сервис - с email.
	if got, want := s.followers("gateway-svc", alice.GetUserId()), []string{"Bob:", "Carol:"}; !slices.Equal(got, want) {
		t.Errorf("followers for gateway-svc = %q, want %q", got, want)
	}
	if got, want := s.followers("notify-svc", alice.GetUserId()), []string{"Bob:bob@example.com", "Carol:carol@example.com"}; !slices.Equal(got, want) {
		t.Errorf("followers for notify-svc = %q, want %q", got, want)
	}

	// Отписка.
	if _, err := s.client.Unsubscribe(s.as("gateway-svc", bob.GetUserId()), &userv1.UnsubscribeRequest{FolloweeId: alice.GetUserId()}); err != nil {
		t.Fatalf("Unsubscribe returned error: %v", err)
	}
	if got, want := s.followers("notify-svc", alice.GetUserId()), []string{"Carol:carol@example.com"}; !slices.Equal(got, want) {
		t.Errorf("followers after unsubscribe = %q, want %q", got, want)
	}

	// Остановка так же, как в main: сервер останавливается, RunServer возвращает nil.
	stopCtx, cancelStop := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelStop()
	application.GracefulStop(stopCtx)

	select {
	case err := <-serverErr:
		if err != nil {
			t.Errorf("RunServer returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunServer did not return after GracefulStop")
	}
	if err := application.CloseStorage(); err != nil {
		t.Errorf("CloseStorage returned error: %v", err)
	}
}
