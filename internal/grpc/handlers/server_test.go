package handlersgrpc_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	userv1 "github.com/alexgul25/protos/gen/go/user/v1"
	"github.com/alexgul25/user-svc/internal/domain"
	"github.com/alexgul25/user-svc/internal/domain/models"
	handlersgrpc "github.com/alexgul25/user-svc/internal/grpc/handlers"
	"github.com/alexgul25/user-svc/internal/grpc/interceptors"
)

// ID в UUID содержат буквы: на них проверяется приведение регистра.
const (
	userID  = "4f1c2d3e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	otherID = "9b8a7c6d-5e4f-4a3b-9c2d-1e0f2a3b4c5d"
)

// errStorage имитирует сбой хранилища. Детали в тексте не должны попасть к клиенту.
var errStorage = errors.New("storage failure: connection to 10.0.0.5:5432 refused")

// errUnexpectedCall возвращает заглушка, если хэндлер вызвал сервис, хотя не должен был.
var errUnexpectedCall = errors.New("unexpected call")

// wrap оборачивает ошибку так же, как это делает бизнес-логика: "op: err".
func wrap(err error) error { return fmt.Errorf("UserLogic: %w", err) }

// ----------------------------------------------------------------------------
// Заглушка сервиса
//
// Поведение задаётся функцией в поле. Если функция не задана, а хэндлер всё же
// вызвал метод, тест падает: так проверяется, что при ошибке валидации запрос
// не доходит до бизнес-логики.
//
// Хэндлер выполняется в горутине gRPC-сервера, а t.Fatal можно вызывать только
// из горутины теста. Поэтому заглушка сообщает об ошибке через t.Errorf.
// ----------------------------------------------------------------------------

type serviceStub struct {
	t *testing.T

	register               func(ctx context.Context, displayName, email, password string) (models.User, error)
	login                  func(ctx context.Context, email, password string) (string, error)
	getMyProfile           func(ctx context.Context, userID string) (models.User, error)
	findUsersByDisplayName func(ctx context.Context, searchQuery string) ([]models.PublicUser, error)
	subscribe              func(ctx context.Context, followerID, followeeID string) error
	unsubscribe            func(ctx context.Context, followerID, followeeID string) error
	getFollowers           func(ctx context.Context, userID string) ([]models.Follower, error)
}

func (s *serviceStub) unexpected(method string) error {
	s.t.Errorf("unexpected call to %s", method)
	return errUnexpectedCall
}

func (s *serviceStub) Register(ctx context.Context, displayName, email, password string) (models.User, error) {
	if s.register == nil {
		return models.User{}, s.unexpected("Register")
	}
	return s.register(ctx, displayName, email, password)
}

func (s *serviceStub) Login(ctx context.Context, email, password string) (string, error) {
	if s.login == nil {
		return "", s.unexpected("Login")
	}
	return s.login(ctx, email, password)
}

func (s *serviceStub) GetMyProfile(ctx context.Context, userID string) (models.User, error) {
	if s.getMyProfile == nil {
		return models.User{}, s.unexpected("GetMyProfile")
	}
	return s.getMyProfile(ctx, userID)
}

func (s *serviceStub) FindUsersByDisplayName(ctx context.Context, searchQuery string) ([]models.PublicUser, error) {
	if s.findUsersByDisplayName == nil {
		return nil, s.unexpected("FindUsersByDisplayName")
	}
	return s.findUsersByDisplayName(ctx, searchQuery)
}

func (s *serviceStub) Subscribe(ctx context.Context, followerID, followeeID string) error {
	if s.subscribe == nil {
		return s.unexpected("Subscribe")
	}
	return s.subscribe(ctx, followerID, followeeID)
}

func (s *serviceStub) Unsubscribe(ctx context.Context, followerID, followeeID string) error {
	if s.unsubscribe == nil {
		return s.unexpected("Unsubscribe")
	}
	return s.unsubscribe(ctx, followerID, followeeID)
}

func (s *serviceStub) GetFollowers(ctx context.Context, userID string) ([]models.Follower, error) {
	if s.getFollowers == nil {
		return nil, s.unexpected("GetFollowers")
	}
	return s.getFollowers(ctx, userID)
}

// ----------------------------------------------------------------------------
// Вспомогательные функции
// ----------------------------------------------------------------------------

// newClient поднимает gRPC-сервер с хэндлерами в памяти (bufconn) и возвращает клиент к нему.
// Заголовки в контекст переносит тот же интерцептор, что и в сервисе.
func newClient(t *testing.T, svc handlersgrpc.UserService, servicesWithEmailHidden []string) userv1.UserServiceClient {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnaryInterceptor(
		interceptors.NewContextEnricherInterceptor([]string{interceptors.HeaderServiceName, interceptors.HeaderUserID}),
	))
	handlersgrpc.Register(srv, svc, servicesWithEmailHidden)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return userv1.NewUserServiceClient(conn)
}

// asUser - вызов от имени пользователя: такие заголовки Gateway добавляет после проверки JWT.
func asUser(t *testing.T, id string) context.Context {
	t.Helper()
	return metadata.AppendToOutgoingContext(t.Context(), interceptors.HeaderServiceName, "gateway-svc", interceptors.HeaderUserID, id)
}

// asService - вызов от внутреннего сервиса, без пользователя.
func asService(t *testing.T, name string) context.Context {
	t.Helper()
	return metadata.AppendToOutgoingContext(t.Context(), interceptors.HeaderServiceName, name)
}

// requireStatus проверяет код и текст ответа сервера.
// Точное сравнение текста заодно гарантирует, что детали внутренних ошибок не уходят клиенту.
func requireStatus(t *testing.T, err error, wantCode codes.Code, wantMsg string) {
	t.Helper()

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error %v is not a gRPC status", err)
	}
	if st.Code() != wantCode || st.Message() != wantMsg {
		t.Fatalf("got status (%v, %q), want (%v, %q)", st.Code(), st.Message(), wantCode, wantMsg)
	}
}

// ----------------------------------------------------------------------------
// Register
// ----------------------------------------------------------------------------

func TestRegister_Success(t *testing.T) {
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 600, time.UTC)
	// Граничный случай: ровно 72 байта - максимально допустимый пароль.
	password := strings.Repeat("p", 72)

	svc := &serviceStub{t: t}
	svc.register = func(_ context.Context, displayName, email, gotPassword string) (models.User, error) {
		if displayName != "Alice" || email != "alice@example.com" || gotPassword != password {
			t.Errorf("Register called with (%q, %q, %q)", displayName, email, gotPassword)
		}
		return models.User{ID: userID, DisplayName: displayName, Email: email, CreatedAt: createdAt}, nil
	}

	resp, err := newClient(t, svc, nil).Register(t.Context(), &userv1.RegisterRequest{
		DisplayName: "Alice",
		Email:       "alice@example.com",
		Password:    password,
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if resp.GetUserId() != userID || resp.GetDisplayName() != "Alice" || resp.GetEmail() != "alice@example.com" {
		t.Errorf("unexpected response: %v", resp)
	}
	if got := resp.GetCreatedAt().AsTime(); !got.Equal(createdAt) {
		t.Errorf("created_at = %v, want %v", got, createdAt)
	}
}

func TestRegister_InvalidArgument(t *testing.T) {
	valid := &userv1.RegisterRequest{DisplayName: "Alice", Email: "alice@example.com", Password: "password"}

	tests := []struct {
		name    string
		modify  func(r *userv1.RegisterRequest)
		wantMsg string
	}{
		{"empty name", func(r *userv1.RegisterRequest) { r.DisplayName = "" }, "name is required"},
		{"NUL in name", func(r *userv1.RegisterRequest) { r.DisplayName = "Ali\x00ce" }, "name contains invalid characters"},
		{"empty email", func(r *userv1.RegisterRequest) { r.Email = "" }, "email is required"},
		{"NUL in email", func(r *userv1.RegisterRequest) { r.Email = "alice\x00@example.com" }, "email contains invalid characters"},
		{"empty password", func(r *userv1.RegisterRequest) { r.Password = "" }, "password is required"},
		{"password of 73 bytes", func(r *userv1.RegisterRequest) { r.Password = strings.Repeat("p", 73) }, "password must be at most 72 bytes"},
		{
			// Ограничение bcrypt - в байтах: 37 кириллических букв занимают 74 байта.
			name:    "limit is in bytes, not characters",
			modify:  func(r *userv1.RegisterRequest) { r.Password = strings.Repeat("я", 37) },
			wantMsg: "password must be at most 72 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &userv1.RegisterRequest{DisplayName: valid.DisplayName, Email: valid.Email, Password: valid.Password}
			tt.modify(req)

			// Функции в заглушке не заданы: обращение к сервису уронит тест.
			_, err := newClient(t, &serviceStub{t: t}, nil).Register(t.Context(), req)
			requireStatus(t, err, codes.InvalidArgument, tt.wantMsg)
		})
	}
}

func TestRegister_ServiceErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantMsg  string
	}{
		{"user already exists", wrap(domain.ErrUserExists), codes.AlreadyExists, "user already exists"},
		{"storage failure", wrap(errStorage), codes.Internal, "failed to register user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &serviceStub{t: t}
			svc.register = func(context.Context, string, string, string) (models.User, error) { return models.User{}, tt.err }

			_, err := newClient(t, svc, nil).Register(t.Context(), &userv1.RegisterRequest{
				DisplayName: "Alice", Email: "alice@example.com", Password: "password",
			})
			requireStatus(t, err, tt.wantCode, tt.wantMsg)
		})
	}
}

// ----------------------------------------------------------------------------
// Login
// ----------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	svc := &serviceStub{t: t}
	svc.login = func(_ context.Context, email, password string) (string, error) {
		if email != "alice@example.com" || password != "password" {
			t.Errorf("Login called with (%q, %q)", email, password)
		}
		return "token", nil
	}

	resp, err := newClient(t, svc, nil).Login(t.Context(), &userv1.LoginRequest{Email: "alice@example.com", Password: "password"})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if resp.GetAccessToken() != "token" {
		t.Errorf("access token = %q, want %q", resp.GetAccessToken(), "token")
	}
}

func TestLogin_InvalidArgument(t *testing.T) {
	tests := []struct {
		name    string
		req     *userv1.LoginRequest
		wantMsg string
	}{
		{"empty email", &userv1.LoginRequest{Password: "password"}, "email is required"},
		{"NUL in email", &userv1.LoginRequest{Email: "alice\x00@example.com", Password: "password"}, "email contains invalid characters"},
		{"empty password", &userv1.LoginRequest{Email: "alice@example.com"}, "password is required"},
		{
			// bcrypt сравнивает только первые 72 байта: без проверки пароль с любым "хвостом" подошёл бы.
			name:    "password of 73 bytes",
			req:     &userv1.LoginRequest{Email: "alice@example.com", Password: strings.Repeat("p", 73)},
			wantMsg: "password must be at most 72 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newClient(t, &serviceStub{t: t}, nil).Login(t.Context(), tt.req)
			requireStatus(t, err, codes.InvalidArgument, tt.wantMsg)
		})
	}
}

func TestLogin_ServiceErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantMsg  string
	}{
		{"invalid credentials", wrap(domain.ErrInvalidCredentials), codes.InvalidArgument, "invalid email or password"},
		{"storage failure", wrap(errStorage), codes.Internal, "failed to login user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &serviceStub{t: t}
			svc.login = func(context.Context, string, string) (string, error) { return "", tt.err }

			_, err := newClient(t, svc, nil).Login(t.Context(), &userv1.LoginRequest{Email: "alice@example.com", Password: "password"})
			requireStatus(t, err, tt.wantCode, tt.wantMsg)
		})
	}
}

// ----------------------------------------------------------------------------
// Аутентификация: методы, вызываемые от имени пользователя
// ----------------------------------------------------------------------------

func TestUserMethods_Unauthenticated(t *testing.T) {
	// Тела запросов пустые: заголовок x-user-id проверяется раньше аргументов,
	// поэтому неаутентифицированный вызов всегда получает Unauthenticated.
	methods := []struct {
		name string
		call func(ctx context.Context, c userv1.UserServiceClient) error
	}{
		{"GetMyProfile", func(ctx context.Context, c userv1.UserServiceClient) error {
			_, err := c.GetMyProfile(ctx, &emptypb.Empty{})
			return err
		}},
		{"FindUsersByDisplayName", func(ctx context.Context, c userv1.UserServiceClient) error {
			_, err := c.FindUsersByDisplayName(ctx, &userv1.FindUsersByDisplayNameRequest{})
			return err
		}},
		{"Subscribe", func(ctx context.Context, c userv1.UserServiceClient) error {
			_, err := c.Subscribe(ctx, &userv1.SubscribeRequest{})
			return err
		}},
		{"Unsubscribe", func(ctx context.Context, c userv1.UserServiceClient) error {
			_, err := c.Unsubscribe(ctx, &userv1.UnsubscribeRequest{})
			return err
		}},
	}

	cases := []struct {
		name    string
		headers []string // пары заголовок-значение
		wantMsg string
	}{
		{"no headers", nil, "user id is required"},
		{"empty user id", []string{interceptors.HeaderUserID, ""}, "user id is required"},
		{"not a UUID", []string{interceptors.HeaderUserID, "user-1"}, "invalid user id"},
		{"UUID in braces", []string{interceptors.HeaderUserID, "{" + userID + "}"}, "invalid user id"},
		{"UUID without hyphens", []string{interceptors.HeaderUserID, strings.ReplaceAll(userID, "-", "")}, "invalid user id"},
	}

	for _, m := range methods {
		for _, tc := range cases {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				ctx := metadata.AppendToOutgoingContext(t.Context(), tc.headers...)

				err := m.call(ctx, newClient(t, &serviceStub{t: t}, nil))
				requireStatus(t, err, codes.Unauthenticated, tc.wantMsg)
			})
		}
	}
}

// ----------------------------------------------------------------------------
// GetMyProfile
// ----------------------------------------------------------------------------

func TestGetMyProfile_Success(t *testing.T) {
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	svc := &serviceStub{t: t}
	svc.getMyProfile = func(_ context.Context, id string) (models.User, error) {
		// Gateway может передать UUID в любом регистре: до логики он доходит в нижнем.
		if id != userID {
			t.Errorf("GetMyProfile called with %q, want %q", id, userID)
		}
		return models.User{ID: id, DisplayName: "Alice", Email: "alice@example.com", CreatedAt: createdAt}, nil
	}

	resp, err := newClient(t, svc, nil).GetMyProfile(asUser(t, strings.ToUpper(userID)), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMyProfile returned error: %v", err)
	}

	if resp.GetUserId() != userID || resp.GetDisplayName() != "Alice" || resp.GetEmail() != "alice@example.com" {
		t.Errorf("unexpected response: %v", resp)
	}
	if got := resp.GetCreatedAt().AsTime(); !got.Equal(createdAt) {
		t.Errorf("created_at = %v, want %v", got, createdAt)
	}
}

func TestGetMyProfile_ServiceErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantMsg  string
	}{
		{"user not found", wrap(domain.ErrUserNotFound), codes.NotFound, "user not found"},
		{"storage failure", wrap(errStorage), codes.Internal, "failed to get user profile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &serviceStub{t: t}
			svc.getMyProfile = func(context.Context, string) (models.User, error) { return models.User{}, tt.err }

			_, err := newClient(t, svc, nil).GetMyProfile(asUser(t, userID), &emptypb.Empty{})
			requireStatus(t, err, tt.wantCode, tt.wantMsg)
		})
	}
}

// ----------------------------------------------------------------------------
// FindUsersByDisplayName
// ----------------------------------------------------------------------------

func TestFindUsersByDisplayName_Success(t *testing.T) {
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	users := []models.PublicUser{
		{ID: userID, DisplayName: "Alice", CreatedAt: createdAt},
		{ID: otherID, DisplayName: "Alina", CreatedAt: createdAt.Add(time.Hour)},
	}

	svc := &serviceStub{t: t}
	svc.findUsersByDisplayName = func(_ context.Context, query string) ([]models.PublicUser, error) {
		if query != "Ali" {
			t.Errorf("FindUsersByDisplayName called with %q, want %q", query, "Ali")
		}
		return users, nil
	}

	resp, err := newClient(t, svc, nil).FindUsersByDisplayName(asUser(t, userID), &userv1.FindUsersByDisplayNameRequest{SearchQuery: "Ali"})
	if err != nil {
		t.Fatalf("FindUsersByDisplayName returned error: %v", err)
	}

	got := resp.GetUsers()
	if len(got) != len(users) {
		t.Fatalf("got %d users, want %d", len(got), len(users))
	}
	for i, u := range users {
		if got[i].GetUserId() != u.ID || got[i].GetDisplayName() != u.DisplayName || !got[i].GetCreatedAt().AsTime().Equal(u.CreatedAt) {
			t.Errorf("user %d = %v, want %+v", i, got[i], u)
		}
	}
}

func TestFindUsersByDisplayName_NothingFound(t *testing.T) {
	svc := &serviceStub{t: t}
	svc.findUsersByDisplayName = func(context.Context, string) ([]models.PublicUser, error) { return []models.PublicUser{}, nil }

	resp, err := newClient(t, svc, nil).FindUsersByDisplayName(asUser(t, userID), &userv1.FindUsersByDisplayNameRequest{SearchQuery: "Zed"})
	if err != nil {
		t.Fatalf("FindUsersByDisplayName returned error: %v", err)
	}
	if len(resp.GetUsers()) != 0 {
		t.Errorf("expected no users, got %v", resp.GetUsers())
	}
}

func TestFindUsersByDisplayName_Errors(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		serviceErr error // nil - запрос не должен дойти до сервиса
		wantCode   codes.Code
		wantMsg    string
	}{
		{name: "empty query", query: "", wantCode: codes.InvalidArgument, wantMsg: "search query is required"},
		{name: "NUL in query", query: "Ali\x00", wantCode: codes.InvalidArgument, wantMsg: "search query contains invalid characters"},
		{name: "storage failure", query: "Ali", serviceErr: wrap(errStorage), wantCode: codes.Internal, wantMsg: "failed to find users by display name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &serviceStub{t: t}
			if tt.serviceErr != nil {
				svc.findUsersByDisplayName = func(context.Context, string) ([]models.PublicUser, error) { return nil, tt.serviceErr }
			}

			_, err := newClient(t, svc, nil).FindUsersByDisplayName(asUser(t, userID), &userv1.FindUsersByDisplayNameRequest{SearchQuery: tt.query})
			requireStatus(t, err, tt.wantCode, tt.wantMsg)
		})
	}
}

// ----------------------------------------------------------------------------
// Subscribe и Unsubscribe
//
// Проверки аргументов у двух методов одинаковые, поэтому сценарии описаны
// один раз и прогоняются для обоих.
// ----------------------------------------------------------------------------

type subscriptionMethod struct {
	name string
	call func(ctx context.Context, c userv1.UserServiceClient, followeeID string) error
	stub func(svc *serviceStub, fn func(ctx context.Context, followerID, followeeID string) error)
}

var subscriptionMethods = []subscriptionMethod{
	{
		name: "Subscribe",
		call: func(ctx context.Context, c userv1.UserServiceClient, followeeID string) error {
			_, err := c.Subscribe(ctx, &userv1.SubscribeRequest{FolloweeId: followeeID})
			return err
		},
		stub: func(svc *serviceStub, fn func(context.Context, string, string) error) { svc.subscribe = fn },
	},
	{
		name: "Unsubscribe",
		call: func(ctx context.Context, c userv1.UserServiceClient, followeeID string) error {
			_, err := c.Unsubscribe(ctx, &userv1.UnsubscribeRequest{FolloweeId: followeeID})
			return err
		},
		stub: func(svc *serviceStub, fn func(context.Context, string, string) error) { svc.unsubscribe = fn },
	},
}

func TestSubscriptions_Success(t *testing.T) {
	for _, m := range subscriptionMethods {
		t.Run(m.name, func(t *testing.T) {
			called := false
			svc := &serviceStub{t: t}
			m.stub(svc, func(_ context.Context, follower, followee string) error {
				called = true
				// Подписчик - из заголовка, на кого подписываются - из запроса. Порядок важен.
				if follower != userID || followee != otherID {
					t.Errorf("service called with (%q, %q), want (%q, %q)", follower, followee, userID, otherID)
				}
				return nil
			})

			if err := m.call(asUser(t, userID), newClient(t, svc, nil), otherID); err != nil {
				t.Fatalf("returned error: %v", err)
			}
			if !called {
				t.Error("service was not called")
			}
		})
	}
}

func TestSubscriptions_IDsAreNormalized(t *testing.T) {
	for _, m := range subscriptionMethods {
		t.Run(m.name, func(t *testing.T) {
			svc := &serviceStub{t: t}
			m.stub(svc, func(_ context.Context, follower, followee string) error {
				// Один и тот же UUID в разном регистре должен дойти до логики одинаковой строкой,
				// иначе она не распознает подписку на самого себя.
				if follower != userID || followee != userID {
					t.Errorf("service called with (%q, %q), want both %q", follower, followee, userID)
				}
				return nil
			})

			if err := m.call(asUser(t, userID), newClient(t, svc, nil), strings.ToUpper(userID)); err != nil {
				t.Fatalf("returned error: %v", err)
			}
		})
	}
}

func TestSubscriptions_InvalidFollowee(t *testing.T) {
	tests := []struct {
		name     string
		followee string
		wantMsg  string
	}{
		{"empty", "", "followee id is required"},
		{"not a UUID", "user-2", "followee id must be a UUID"},
		{"SQL in id", "' OR '1'='1", "followee id must be a UUID"},
		{"UUID with extra characters", otherID + "0", "followee id must be a UUID"},
	}

	for _, m := range subscriptionMethods {
		for _, tt := range tests {
			t.Run(m.name+"/"+tt.name, func(t *testing.T) {
				err := m.call(asUser(t, userID), newClient(t, &serviceStub{t: t}, nil), tt.followee)
				requireStatus(t, err, codes.InvalidArgument, tt.wantMsg)
			})
		}
	}
}

func TestSubscribe_ServiceErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantMsg  string
	}{
		{"already subscribed", wrap(domain.ErrAlreadySubscribed), codes.InvalidArgument, "user has already subscribed"},
		{"self subscription", wrap(domain.ErrSelfSubscription), codes.InvalidArgument, "cannot subscribe to yourself"},
		{"followee not found", wrap(domain.ErrUserNotFound), codes.NotFound, "user not found"},
		{"storage failure", wrap(errStorage), codes.Internal, "failed to subscribe user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &serviceStub{t: t}
			svc.subscribe = func(context.Context, string, string) error { return tt.err }

			_, err := newClient(t, svc, nil).Subscribe(asUser(t, userID), &userv1.SubscribeRequest{FolloweeId: otherID})
			requireStatus(t, err, tt.wantCode, tt.wantMsg)
		})
	}
}

func TestUnsubscribe_ServiceErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantMsg  string
	}{
		{"self subscription", wrap(domain.ErrSelfSubscription), codes.InvalidArgument, "cannot unsubscribe from yourself"},
		{"storage failure", wrap(errStorage), codes.Internal, "failed to unsubscribe user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &serviceStub{t: t}
			svc.unsubscribe = func(context.Context, string, string) error { return tt.err }

			_, err := newClient(t, svc, nil).Unsubscribe(asUser(t, userID), &userv1.UnsubscribeRequest{FolloweeId: otherID})
			requireStatus(t, err, tt.wantCode, tt.wantMsg)
		})
	}
}

// ----------------------------------------------------------------------------
// GetFollowers
// ----------------------------------------------------------------------------

func TestGetFollowers_EmailVisibility(t *testing.T) {
	followers := []models.Follower{
		{ID: otherID, DisplayName: "Bob", Email: "bob@example.com"},
		{ID: "c3d4e5f6-a7b8-4c9d-8e0f-1a2b3c4d5e6f", DisplayName: "Carol", Email: "carol@example.com"},
	}

	tests := []struct {
		name      string
		caller    string
		hiddenFor []string
		wantEmail bool
	}{
		{name: "caller in the list", caller: "gateway-svc", hiddenFor: []string{"gateway-svc"}, wantEmail: false},
		{name: "caller not in the list", caller: "notify-svc", hiddenFor: []string{"gateway-svc"}, wantEmail: true},
		{name: "empty list", caller: "gateway-svc", hiddenFor: nil, wantEmail: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &serviceStub{t: t}
			svc.getFollowers = func(_ context.Context, id string) ([]models.Follower, error) {
				if id != userID {
					t.Errorf("GetFollowers called with %q, want %q", id, userID)
				}
				return followers, nil
			}

			resp, err := newClient(t, svc, tt.hiddenFor).GetFollowers(asService(t, tt.caller), &userv1.GetFollowersRequest{UserId: userID})
			if err != nil {
				t.Fatalf("GetFollowers returned error: %v", err)
			}

			got := resp.GetFollowers()
			if len(got) != len(followers) {
				t.Fatalf("got %d followers, want %d", len(got), len(followers))
			}
			for i, f := range followers {
				wantEmail := ""
				if tt.wantEmail {
					wantEmail = f.Email
				}
				if got[i].GetUserId() != f.ID || got[i].GetDisplayName() != f.DisplayName || got[i].GetEmail() != wantEmail {
					t.Errorf("follower %d = %v, want id %q, name %q, email %q", i, got[i], f.ID, f.DisplayName, wantEmail)
				}
			}
		})
	}
}

func TestGetFollowers_UserIDIsNormalized(t *testing.T) {
	svc := &serviceStub{t: t}
	svc.getFollowers = func(_ context.Context, id string) ([]models.Follower, error) {
		if id != userID {
			t.Errorf("GetFollowers called with %q, want %q", id, userID)
		}
		return nil, nil
	}

	_, err := newClient(t, svc, nil).GetFollowers(asService(t, "notify-svc"), &userv1.GetFollowersRequest{UserId: strings.ToUpper(userID)})
	if err != nil {
		t.Fatalf("GetFollowers returned error: %v", err)
	}
}

func TestGetFollowers_Errors(t *testing.T) {
	tests := []struct {
		name       string
		caller     string // значение x-service-name; пустое - заголовка нет
		userID     string
		serviceErr error // nil - запрос не должен дойти до сервиса
		wantCode   codes.Code
		wantMsg    string
	}{
		{
			// Имя сервиса проверяется раньше аргументов.
			name:     "no service name",
			caller:   "",
			userID:   "",
			wantCode: codes.Unauthenticated,
			wantMsg:  "missing service name",
		},
		{
			name:     "empty user id",
			caller:   "notify-svc",
			userID:   "",
			wantCode: codes.InvalidArgument,
			wantMsg:  "user id is required",
		},
		{
			name:     "user id is not a UUID",
			caller:   "notify-svc",
			userID:   "user-1",
			wantCode: codes.InvalidArgument,
			wantMsg:  "user id must be a UUID",
		},
		{
			name:       "storage failure",
			caller:     "notify-svc",
			userID:     userID,
			serviceErr: wrap(errStorage),
			wantCode:   codes.Internal,
			wantMsg:    "failed to get user followers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &serviceStub{t: t}
			if tt.serviceErr != nil {
				svc.getFollowers = func(context.Context, string) ([]models.Follower, error) { return nil, tt.serviceErr }
			}

			ctx := t.Context()
			if tt.caller != "" {
				ctx = asService(t, tt.caller)
			}

			_, err := newClient(t, svc, nil).GetFollowers(ctx, &userv1.GetFollowersRequest{UserId: tt.userID})
			requireStatus(t, err, tt.wantCode, tt.wantMsg)
		})
	}
}
