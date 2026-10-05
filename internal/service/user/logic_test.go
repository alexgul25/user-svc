package userlogic_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/alexgul25/user-svc/internal/domain"
	"github.com/alexgul25/user-svc/internal/domain/models"
	jwtmanager "github.com/alexgul25/user-svc/internal/lib/jwt"
	userlogic "github.com/alexgul25/user-svc/internal/service/user"
)

const (
	testSecret   = "test-secret"
	testTokenTTL = time.Hour
)

// errStorage имитирует непредвиденный сбой хранилища (например, недоступна БД).
var errStorage = errors.New("storage failure")

// ----------------------------------------------------------------------------
// Заглушки репозиториев
//
// Поведение каждого метода задаётся функцией в поле. Если тест не задал
// функцию, а логика всё же вызвала метод, тест падает: так проверяется,
// что логика не обращается к хранилищу, когда не должна.
// ----------------------------------------------------------------------------

type userRepoStub struct {
	t *testing.T

	createUser            func(ctx context.Context, displayName, email string, passwordHash []byte) (models.User, error)
	getUserByEmail        func(ctx context.Context, email string) (models.User, error)
	getUserByID           func(ctx context.Context, userID string) (models.User, error)
	getUsersByDisplayName func(ctx context.Context, searchQuery string) ([]models.PublicUser, error)
}

func (s *userRepoStub) CreateUser(ctx context.Context, displayName, email string, passwordHash []byte) (models.User, error) {
	if s.createUser == nil {
		s.t.Fatal("unexpected call to CreateUser")
	}
	return s.createUser(ctx, displayName, email, passwordHash)
}

func (s *userRepoStub) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	if s.getUserByEmail == nil {
		s.t.Fatal("unexpected call to GetUserByEmail")
	}
	return s.getUserByEmail(ctx, email)
}

func (s *userRepoStub) GetUserByID(ctx context.Context, userID string) (models.User, error) {
	if s.getUserByID == nil {
		s.t.Fatal("unexpected call to GetUserByID")
	}
	return s.getUserByID(ctx, userID)
}

func (s *userRepoStub) GetUsersByDisplayName(ctx context.Context, searchQuery string) ([]models.PublicUser, error) {
	if s.getUsersByDisplayName == nil {
		s.t.Fatal("unexpected call to GetUsersByDisplayName")
	}
	return s.getUsersByDisplayName(ctx, searchQuery)
}

type subRepoStub struct {
	t *testing.T

	subscribe    func(ctx context.Context, followerID, followeeID string) error
	unsubscribe  func(ctx context.Context, followerID, followeeID string) error
	getFollowers func(ctx context.Context, userID string) ([]models.Follower, error)
}

func (s *subRepoStub) Subscribe(ctx context.Context, followerID, followeeID string) error {
	if s.subscribe == nil {
		s.t.Fatal("unexpected call to Subscribe")
	}
	return s.subscribe(ctx, followerID, followeeID)
}

func (s *subRepoStub) Unsubscribe(ctx context.Context, followerID, followeeID string) error {
	if s.unsubscribe == nil {
		s.t.Fatal("unexpected call to Unsubscribe")
	}
	return s.unsubscribe(ctx, followerID, followeeID)
}

func (s *subRepoStub) GetFollowers(ctx context.Context, userID string) ([]models.Follower, error) {
	if s.getFollowers == nil {
		s.t.Fatal("unexpected call to GetFollowers")
	}
	return s.getFollowers(ctx, userID)
}

// ----------------------------------------------------------------------------
// Вспомогательные функции
// ----------------------------------------------------------------------------

// newLogic собирает логику с заглушками и логгером, который ничего не выводит.
func newLogic(usrRepo *userRepoStub, subRepo *subRepoStub) *userlogic.UserLogic {
	return userlogic.New(
		slog.New(slog.DiscardHandler),
		usrRepo,
		subRepo,
		jwtmanager.New([]byte(testSecret), testTokenTTL),
	)
}

// hashPassword возвращает bcrypt-хеш с минимальной стоимостью, чтобы тесты были быстрыми.
func hashPassword(t *testing.T, password string) []byte {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	return hash
}

// ----------------------------------------------------------------------------
// Register
// ----------------------------------------------------------------------------

func TestRegister_Success(t *testing.T) {
	const (
		displayName = "Alice"
		email       = "alice@example.com"
		password    = "correct-password"
	)
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	var savedHash []byte
	usrRepo := &userRepoStub{t: t}
	usrRepo.createUser = func(_ context.Context, gotName, gotEmail string, passwordHash []byte) (models.User, error) {
		if gotName != displayName || gotEmail != email {
			t.Errorf("CreateUser called with (%q, %q), want (%q, %q)", gotName, gotEmail, displayName, email)
		}
		savedHash = passwordHash

		return models.User{
			ID:           "user-1",
			DisplayName:  gotName,
			Email:        gotEmail,
			PasswordHash: passwordHash,
			CreatedAt:    createdAt,
		}, nil
	}

	user, err := newLogic(usrRepo, &subRepoStub{t: t}).Register(context.Background(), displayName, email, password)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	// В хранилище должен попасть хеш пароля, а не сам пароль.
	if string(savedHash) == password {
		t.Error("password was saved in plain text")
	}
	if err := bcrypt.CompareHashAndPassword(savedHash, []byte(password)); err != nil {
		t.Errorf("saved hash does not match the password: %v", err)
	}

	// Хеш пароля не должен покидать слой логики.
	if len(user.PasswordHash) != 0 {
		t.Error("returned user contains password hash")
	}

	if user.ID != "user-1" || user.DisplayName != displayName || user.Email != email || !user.CreatedAt.Equal(createdAt) {
		t.Errorf("unexpected user returned: %+v", user)
	}
}

func TestRegister_Errors(t *testing.T) {
	tests := []struct {
		name     string
		password string
		// repoErr == nil означает, что до хранилища дело дойти не должно.
		repoErr error
		wantErr error
	}{
		{
			name:     "user already exists",
			password: "password",
			repoErr:  domain.ErrUserExists,
			wantErr:  domain.ErrUserExists,
		},
		{
			name:     "storage failure",
			password: "password",
			repoErr:  errStorage,
			wantErr:  errStorage,
		},
		{
			// bcrypt не принимает пароли длиннее 72 байт.
			name:     "password is too long",
			password: strings.Repeat("a", 73),
			wantErr:  bcrypt.ErrPasswordTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usrRepo := &userRepoStub{t: t}
			if tt.repoErr != nil {
				usrRepo.createUser = func(context.Context, string, string, []byte) (models.User, error) {
					return models.User{}, tt.repoErr
				}
			}

			user, err := newLogic(usrRepo, &subRepoStub{t: t}).Register(context.Background(), "Alice", "alice@example.com", tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got error %v, want %v", err, tt.wantErr)
			}
			if user.ID != "" || user.Email != "" || len(user.PasswordHash) != 0 {
				t.Errorf("expected empty user on error, got %+v", user)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Login
// ----------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	const (
		email    = "alice@example.com"
		password = "correct-password"
		userID   = "user-1"
	)

	usrRepo := &userRepoStub{t: t}
	usrRepo.getUserByEmail = func(_ context.Context, gotEmail string) (models.User, error) {
		if gotEmail != email {
			t.Errorf("GetUserByEmail called with %q, want %q", gotEmail, email)
		}
		return models.User{ID: userID, Email: email, PasswordHash: hashPassword(t, password)}, nil
	}

	before := time.Now()
	token, err := newLogic(usrRepo, &subRepoStub{t: t}).Login(context.Background(), email, password)
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}

	// Токен должен быть подписан секретом сервиса и содержать ID пользователя.
	var claims jwtmanager.TokenClaims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) {
		return []byte(testSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		t.Fatalf("token is not valid: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("token user_id = %q, want %q", claims.UserID, userID)
	}

	// Срок действия токена: примерно "сейчас + TTL" (время в JWT хранится с точностью до секунды).
	wantExpiry := before.Add(testTokenTTL)
	if diff := claims.ExpiresAt.Time.Sub(wantExpiry); diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("token expires at %v, want about %v", claims.ExpiresAt.Time, wantExpiry)
	}
}

func TestLogin_Errors(t *testing.T) {
	const (
		email    = "alice@example.com"
		password = "correct-password"
	)

	tests := []struct {
		name string
		// Что вернёт хранилище на запрос пользователя по email.
		repoUser models.User
		repoErr  error

		password string
		wantErr  error
		// Ошибка, которой в цепочке быть не должно.
		notWantErr error
	}{
		{
			// Неизвестный email и неверный пароль должны быть неразличимы для клиента,
			// иначе по ответу можно узнать, зарегистрирован ли email.
			name:       "user not found is reported as invalid credentials",
			repoErr:    domain.ErrUserNotFound,
			password:   password,
			wantErr:    domain.ErrInvalidCredentials,
			notWantErr: domain.ErrUserNotFound,
		},
		{
			name:     "wrong password",
			repoUser: models.User{ID: "user-1", Email: email, PasswordHash: hashPassword(t, password)},
			password: "wrong-password",
			wantErr:  domain.ErrInvalidCredentials,
		},
		{
			// Сбой хранилища не должен выглядеть как неверные учётные данные.
			name:       "storage failure",
			repoErr:    errStorage,
			password:   password,
			wantErr:    errStorage,
			notWantErr: domain.ErrInvalidCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usrRepo := &userRepoStub{t: t}
			usrRepo.getUserByEmail = func(context.Context, string) (models.User, error) {
				return tt.repoUser, tt.repoErr
			}

			token, err := newLogic(usrRepo, &subRepoStub{t: t}).Login(context.Background(), email, tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got error %v, want %v", err, tt.wantErr)
			}
			if tt.notWantErr != nil && errors.Is(err, tt.notWantErr) {
				t.Errorf("error %v must not wrap %v", err, tt.notWantErr)
			}
			if token != "" {
				t.Errorf("expected empty token on error, got %q", token)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// GetMyProfile
// ----------------------------------------------------------------------------

func TestGetMyProfile(t *testing.T) {
	const userID = "user-1"

	t.Run("success", func(t *testing.T) {
		usrRepo := &userRepoStub{t: t}
		usrRepo.getUserByID = func(_ context.Context, gotID string) (models.User, error) {
			if gotID != userID {
				t.Errorf("GetUserByID called with %q, want %q", gotID, userID)
			}
			return models.User{ID: userID, DisplayName: "Alice", Email: "alice@example.com", PasswordHash: []byte("hash")}, nil
		}

		user, err := newLogic(usrRepo, &subRepoStub{t: t}).GetMyProfile(context.Background(), userID)
		if err != nil {
			t.Fatalf("GetMyProfile returned error: %v", err)
		}
		if len(user.PasswordHash) != 0 {
			t.Error("returned user contains password hash")
		}
		if user.ID != userID || user.DisplayName != "Alice" || user.Email != "alice@example.com" {
			t.Errorf("unexpected user returned: %+v", user)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		usrRepo := &userRepoStub{t: t}
		usrRepo.getUserByID = func(context.Context, string) (models.User, error) {
			return models.User{}, domain.ErrUserNotFound
		}

		_, err := newLogic(usrRepo, &subRepoStub{t: t}).GetMyProfile(context.Background(), userID)
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("got error %v, want %v", err, domain.ErrUserNotFound)
		}
	})
}

// ----------------------------------------------------------------------------
// FindUsersByDisplayName
// ----------------------------------------------------------------------------

func TestFindUsersByDisplayName(t *testing.T) {
	const query = "ali"

	t.Run("success", func(t *testing.T) {
		want := []models.PublicUser{{ID: "user-1", DisplayName: "Alice"}, {ID: "user-2", DisplayName: "Alina"}}

		usrRepo := &userRepoStub{t: t}
		usrRepo.getUsersByDisplayName = func(_ context.Context, gotQuery string) ([]models.PublicUser, error) {
			if gotQuery != query {
				t.Errorf("GetUsersByDisplayName called with %q, want %q", gotQuery, query)
			}
			return want, nil
		}

		got, err := newLogic(usrRepo, &subRepoStub{t: t}).FindUsersByDisplayName(context.Background(), query)
		if err != nil {
			t.Fatalf("FindUsersByDisplayName returned error: %v", err)
		}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("storage failure", func(t *testing.T) {
		usrRepo := &userRepoStub{t: t}
		usrRepo.getUsersByDisplayName = func(context.Context, string) ([]models.PublicUser, error) {
			return nil, errStorage
		}

		got, err := newLogic(usrRepo, &subRepoStub{t: t}).FindUsersByDisplayName(context.Background(), query)
		if !errors.Is(err, errStorage) {
			t.Fatalf("got error %v, want %v", err, errStorage)
		}
		if got != nil {
			t.Errorf("expected nil result on error, got %+v", got)
		}
	})
}

// ----------------------------------------------------------------------------
// Subscribe и Unsubscribe
//
// У двух методов одинаковые правила, поэтому сценарии описаны один раз
// и прогоняются для обоих.
// ----------------------------------------------------------------------------

func TestSubscribeAndUnsubscribe(t *testing.T) {
	const (
		followerID = "user-1"
		followeeID = "user-2"
	)

	// Какой метод логики вызываем и какой метод хранилища он должен использовать.
	methods := []struct {
		name string
		call func(ul *userlogic.UserLogic, followerID, followeeID string) error
		stub func(repo *subRepoStub, fn func(ctx context.Context, followerID, followeeID string) error)
	}{
		{
			name: "Subscribe",
			call: func(ul *userlogic.UserLogic, follower, followee string) error {
				return ul.Subscribe(context.Background(), follower, followee)
			},
			stub: func(repo *subRepoStub, fn func(context.Context, string, string) error) { repo.subscribe = fn },
		},
		{
			name: "Unsubscribe",
			call: func(ul *userlogic.UserLogic, follower, followee string) error {
				return ul.Unsubscribe(context.Background(), follower, followee)
			},
			stub: func(repo *subRepoStub, fn func(context.Context, string, string) error) { repo.unsubscribe = fn },
		},
	}

	for _, m := range methods {
		t.Run(m.name+"/success", func(t *testing.T) {
			called := false
			subRepo := &subRepoStub{t: t}
			m.stub(subRepo, func(_ context.Context, gotFollower, gotFollowee string) error {
				called = true
				// Перепутанные местами аргументы - типичная ошибка, проверяем порядок.
				if gotFollower != followerID || gotFollowee != followeeID {
					t.Errorf("repository called with (%q, %q), want (%q, %q)", gotFollower, gotFollowee, followerID, followeeID)
				}
				return nil
			})

			if err := m.call(newLogic(&userRepoStub{t: t}, subRepo), followerID, followeeID); err != nil {
				t.Fatalf("returned error: %v", err)
			}
			if !called {
				t.Error("repository was not called")
			}
		})

		t.Run(m.name+"/self subscription is rejected without calling storage", func(t *testing.T) {
			// Функция в заглушке не задана: обращение к хранилищу уронит тест.
			err := m.call(newLogic(&userRepoStub{t: t}, &subRepoStub{t: t}), followerID, followerID)
			if !errors.Is(err, domain.ErrSelfSubscription) {
				t.Fatalf("got error %v, want %v", err, domain.ErrSelfSubscription)
			}
		})

		t.Run(m.name+"/storage error is returned", func(t *testing.T) {
			subRepo := &subRepoStub{t: t}
			m.stub(subRepo, func(context.Context, string, string) error { return domain.ErrAlreadySubscribed })

			err := m.call(newLogic(&userRepoStub{t: t}, subRepo), followerID, followeeID)
			if !errors.Is(err, domain.ErrAlreadySubscribed) {
				t.Fatalf("got error %v, want %v", err, domain.ErrAlreadySubscribed)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// GetFollowers
// ----------------------------------------------------------------------------

func TestGetFollowers(t *testing.T) {
	const userID = "user-1"

	t.Run("success", func(t *testing.T) {
		want := []models.Follower{{ID: "user-2", DisplayName: "Bob", Email: "bob@example.com"}}

		subRepo := &subRepoStub{t: t}
		subRepo.getFollowers = func(_ context.Context, gotID string) ([]models.Follower, error) {
			if gotID != userID {
				t.Errorf("GetFollowers called with %q, want %q", gotID, userID)
			}
			return want, nil
		}

		got, err := newLogic(&userRepoStub{t: t}, subRepo).GetFollowers(context.Background(), userID)
		if err != nil {
			t.Fatalf("GetFollowers returned error: %v", err)
		}
		if len(got) != 1 || got[0] != want[0] {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("storage failure", func(t *testing.T) {
		subRepo := &subRepoStub{t: t}
		subRepo.getFollowers = func(context.Context, string) ([]models.Follower, error) {
			return nil, errStorage
		}

		got, err := newLogic(&userRepoStub{t: t}, subRepo).GetFollowers(context.Background(), userID)
		if !errors.Is(err, errStorage) {
			t.Fatalf("got error %v, want %v", err, errStorage)
		}
		if got != nil {
			t.Errorf("expected nil result on error, got %+v", got)
		}
	})
}
