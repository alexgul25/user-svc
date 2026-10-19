//go:build integration

package postgresql_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/alexgul25/user-svc/internal/domain"
	"github.com/alexgul25/user-svc/internal/domain/models"
	"github.com/alexgul25/user-svc/internal/storage/postgresql"
)

// missingID - корректный UUID, которого заведомо нет в базе.
const missingID = "9b8a7c6d-5e4f-4a3b-9c2d-1e0f2a3b4c5d"

var uuidRegexp = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ----------------------------------------------------------------------------
// Вспомогательные функции
// ----------------------------------------------------------------------------

// createUser создаёт пользователя и роняет тест при ошибке.
func createUser(t *testing.T, us *postgresql.UserStorage, displayName, email string) models.User {
	t.Helper()

	user, err := us.CreateUser(t.Context(), displayName, email, []byte("hash-of-"+email))
	if err != nil {
		t.Fatalf("CreateUser(%q, %q) returned error: %v", displayName, email, err)
	}
	return user
}

// subscribe подписывает follower на followee и роняет тест при ошибке.
func subscribe(t *testing.T, us *postgresql.UserStorage, follower, followee models.User) {
	t.Helper()

	if err := us.Subscribe(t.Context(), follower.ID, followee.ID); err != nil {
		t.Fatalf("Subscribe(%s -> %s) returned error: %v", follower.DisplayName, followee.DisplayName, err)
	}
}

// names возвращает отображаемые имена пользователей в порядке выдачи.
func names(users []models.PublicUser) []string {
	result := make([]string, len(users))
	for i, u := range users {
		result[i] = u.DisplayName
	}
	return result
}

// followerIDs возвращает отсортированные ID подписчиков: порядок выдачи GetFollowers не задан.
func followerIDs(followers []models.Follower) []string {
	ids := make([]string, len(followers))
	for i, f := range followers {
		ids[i] = f.ID
	}
	slices.Sort(ids)
	return ids
}

// sortedIDs возвращает отсортированные ID пользователей.
func sortedIDs(users ...models.User) []string {
	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	slices.Sort(ids)
	return ids
}

// ----------------------------------------------------------------------------
// CreateUser
// ----------------------------------------------------------------------------

func TestCreateUser(t *testing.T) {
	us := newUserStorage(t)
	before := time.Now()

	user, err := us.CreateUser(t.Context(), "Alice", "alice@example.com", []byte("hash"))
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}

	// ID генерирует база: UUID в каноническом виде.
	if !uuidRegexp.MatchString(user.ID) {
		t.Errorf("ID = %q, want canonical UUID", user.ID)
	}
	if user.DisplayName != "Alice" || user.Email != "alice@example.com" || !bytes.Equal(user.PasswordHash, []byte("hash")) {
		t.Errorf("unexpected user: %+v", user)
	}
	// created_at ставит база: допуск на расхождение часов приложения и БД.
	if diff := user.CreatedAt.Sub(before); diff < -time.Minute || diff > time.Minute {
		t.Errorf("CreatedAt = %v, want about %v", user.CreatedAt, before)
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
	}{
		{name: "same email", email: "alice@example.com"},
		{name: "email in different case", email: "ALICE@Example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			us := newUserStorage(t)
			createUser(t, us, "Alice", "alice@example.com")

			_, err := us.CreateUser(t.Context(), "Alice 2", tt.email, []byte("hash"))
			if !errors.Is(err, domain.ErrUserExists) {
				t.Errorf("got error %v, want %v", err, domain.ErrUserExists)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// GetUserByEmail и GetUserByID
// ----------------------------------------------------------------------------

func TestGetUserByEmail(t *testing.T) {
	us := newUserStorage(t)
	created := createUser(t, us, "Alice", "Alice@Example.com")

	t.Run("found in any case", func(t *testing.T) {
		for _, email := range []string{"Alice@Example.com", "alice@example.com", "ALICE@EXAMPLE.COM"} {
			got, err := us.GetUserByEmail(t.Context(), email)
			if err != nil {
				t.Fatalf("GetUserByEmail(%q) returned error: %v", email, err)
			}
			if got.ID != created.ID || got.DisplayName != "Alice" || !bytes.Equal(got.PasswordHash, created.PasswordHash) {
				t.Errorf("GetUserByEmail(%q) = %+v, want user %s", email, got, created.ID)
			}
			if !got.CreatedAt.Equal(created.CreatedAt) {
				t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created.CreatedAt)
			}
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := us.GetUserByEmail(t.Context(), "bob@example.com")
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Errorf("got error %v, want %v", err, domain.ErrUserNotFound)
		}
	})
}

func TestGetUserByID(t *testing.T) {
	us := newUserStorage(t)
	created := createUser(t, us, "Alice", "alice@example.com")

	t.Run("found", func(t *testing.T) {
		got, err := us.GetUserByID(t.Context(), created.ID)
		if err != nil {
			t.Fatalf("GetUserByID returned error: %v", err)
		}
		if got.ID != created.ID || got.DisplayName != "Alice" || got.Email != "alice@example.com" ||
			!bytes.Equal(got.PasswordHash, created.PasswordHash) || !got.CreatedAt.Equal(created.CreatedAt) {
			t.Errorf("got %+v, want %+v", got, created)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := us.GetUserByID(t.Context(), missingID)
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Errorf("got error %v, want %v", err, domain.ErrUserNotFound)
		}
	})

	t.Run("invalid UUID is a database error", func(t *testing.T) {
		// Формат ID проверяют хэндлеры. До хранилища некорректный UUID не доходит,
		// а если дойдёт - это внутренняя ошибка, а не "пользователь не найден".
		_, err := us.GetUserByID(t.Context(), "not-a-uuid")
		if err == nil || errors.Is(err, domain.ErrUserNotFound) {
			t.Errorf("got error %v, want a database error", err)
		}
	})
}

// ----------------------------------------------------------------------------
// GetUsersByDisplayName
// ----------------------------------------------------------------------------

func TestGetUsersByDisplayName(t *testing.T) {
	us := newUserStorage(t)
	for i, name := range []string{"Alice", "alina", "Bob", "Malik", "Алиса", "АЛЕКСЕЙ", "Борис"} {
		createUser(t, us, name, fmt.Sprintf("user%d@example.com", i))
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "prefix, case-insensitive", query: "ali", want: []string{"Alice", "alina"}},
		{name: "upper-case query", query: "ALI", want: []string{"Alice", "alina"}},
		// Поиск - по началу имени: "Malik" содержит "ali", но не начинается с него.
		{name: "match only at the beginning", query: "lik", want: []string{}},
		{name: "cyrillic, case-insensitive", query: "ал", want: []string{"АЛЕКСЕЙ", "Алиса"}},
		{name: "full name", query: "Bob", want: []string{"Bob"}},
		{name: "nothing found", query: "Zed", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := us.GetUsersByDisplayName(t.Context(), tt.query)
			if err != nil {
				t.Fatalf("GetUsersByDisplayName(%q) returned error: %v", tt.query, err)
			}
			// Пустой результат - пустой срез, а не nil.
			if got == nil {
				t.Fatal("got nil, want empty slice")
			}
			if !slices.Equal(names(got), tt.want) {
				t.Errorf("GetUsersByDisplayName(%q) = %q, want %q", tt.query, names(got), tt.want)
			}
		})
	}
}

func TestGetUsersByDisplayName_ReturnsPublicFields(t *testing.T) {
	us := newUserStorage(t)
	created := createUser(t, us, "Alice", "alice@example.com")

	got, err := us.GetUsersByDisplayName(t.Context(), "Alice")
	if err != nil {
		t.Fatalf("GetUsersByDisplayName returned error: %v", err)
	}
	want := []models.PublicUser{{ID: created.ID, DisplayName: "Alice", CreatedAt: created.CreatedAt}}
	if len(got) != 1 || got[0].ID != want[0].ID || got[0].DisplayName != want[0].DisplayName || !got[0].CreatedAt.Equal(want[0].CreatedAt) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUsersByDisplayName_WildcardsAreLiteral(t *testing.T) {
	us := newUserStorage(t)
	for i, name := range []string{"100% real", "100 percent", "a_b", "axb", `back\slash`, "backslash"} {
		createUser(t, us, name, fmt.Sprintf("user%d@example.com", i))
	}

	// Символы %, _ и \ в запросе ищутся буквально, а не работают как шаблон LIKE.
	tests := []struct {
		query string
		want  []string
	}{
		{query: "%", want: []string{}},
		{query: "_", want: []string{}},
		{query: "100%", want: []string{"100% real"}},
		{query: "a_", want: []string{"a_b"}},
		{query: `back\`, want: []string{`back\slash`}},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := us.GetUsersByDisplayName(t.Context(), tt.query)
			if err != nil {
				t.Fatalf("GetUsersByDisplayName(%q) returned error: %v", tt.query, err)
			}
			if !slices.Equal(names(got), tt.want) {
				t.Errorf("GetUsersByDisplayName(%q) = %q, want %q", tt.query, names(got), tt.want)
			}
		})
	}
}

func TestGetUsersByDisplayName_LimitAndOrder(t *testing.T) {
	us := newUserStorage(t)
	total := postgresql.MaxSearchResults + 10
	// Создаём в обратном порядке: сортировку должна делать база, а не порядок вставки.
	for i := total - 1; i >= 0; i-- {
		createUser(t, us, fmt.Sprintf("user %03d", i), fmt.Sprintf("user%d@example.com", i))
	}

	got, err := us.GetUsersByDisplayName(t.Context(), "user")
	if err != nil {
		t.Fatalf("GetUsersByDisplayName returned error: %v", err)
	}
	if len(got) != postgresql.MaxSearchResults {
		t.Fatalf("got %d users, want %d", len(got), postgresql.MaxSearchResults)
	}
	for i, u := range got {
		if want := fmt.Sprintf("user %03d", i); u.DisplayName != want {
			t.Fatalf("user %d = %q, want %q (results must be sorted by name)", i, u.DisplayName, want)
		}
	}
}

// ----------------------------------------------------------------------------
// Subscribe, Unsubscribe и GetFollowers
// ----------------------------------------------------------------------------

func TestSubscribe_Errors(t *testing.T) {
	us := newUserStorage(t)
	alice := createUser(t, us, "Alice", "alice@example.com")
	bob := createUser(t, us, "Bob", "bob@example.com")
	subscribe(t, us, bob, alice)

	tests := []struct {
		name               string
		follower, followee string
		wantErr            error
	}{
		{name: "already subscribed", follower: bob.ID, followee: alice.ID, wantErr: domain.ErrAlreadySubscribed},
		{name: "followee does not exist", follower: bob.ID, followee: missingID, wantErr: domain.ErrUserNotFound},
		{name: "follower does not exist", follower: missingID, followee: alice.ID, wantErr: domain.ErrUserNotFound},
		// Логика отклоняет подписку на себя раньше, но и база её не допускает.
		{name: "self subscription", follower: alice.ID, followee: alice.ID, wantErr: domain.ErrSelfSubscription},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := us.Subscribe(t.Context(), tt.follower, tt.followee)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got error %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetFollowers(t *testing.T) {
	us := newUserStorage(t)
	alice := createUser(t, us, "Alice", "alice@example.com")
	bob := createUser(t, us, "Bob", "bob@example.com")
	carol := createUser(t, us, "Carol", "carol@example.com")

	// Bob и Carol подписаны на Alice, Alice подписана на Carol.
	subscribe(t, us, bob, alice)
	subscribe(t, us, carol, alice)
	subscribe(t, us, alice, carol)

	tests := []struct {
		name string
		user models.User
		want []string
	}{
		// Возвращаются подписчики пользователя, а не те, на кого он подписан.
		{name: "Alice", user: alice, want: sortedIDs(bob, carol)},
		{name: "Carol", user: carol, want: sortedIDs(alice)},
		{name: "Bob has no followers", user: bob, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := us.GetFollowers(t.Context(), tt.user.ID)
			if err != nil {
				t.Fatalf("GetFollowers returned error: %v", err)
			}
			if got == nil {
				t.Fatal("got nil, want empty slice")
			}
			if !slices.Equal(followerIDs(got), tt.want) {
				t.Errorf("followers = %v, want %v", followerIDs(got), tt.want)
			}
		})
	}

	t.Run("followers include name and email", func(t *testing.T) {
		got, err := us.GetFollowers(t.Context(), carol.ID)
		if err != nil {
			t.Fatalf("GetFollowers returned error: %v", err)
		}
		want := models.Follower{ID: alice.ID, DisplayName: "Alice", Email: "alice@example.com"}
		if len(got) != 1 || got[0] != want {
			t.Errorf("got %+v, want [%+v]", got, want)
		}
	})
}

func TestUnsubscribe(t *testing.T) {
	us := newUserStorage(t)
	alice := createUser(t, us, "Alice", "alice@example.com")
	bob := createUser(t, us, "Bob", "bob@example.com")
	carol := createUser(t, us, "Carol", "carol@example.com")
	subscribe(t, us, bob, alice)
	subscribe(t, us, carol, alice)

	if err := us.Unsubscribe(t.Context(), bob.ID, alice.ID); err != nil {
		t.Fatalf("Unsubscribe returned error: %v", err)
	}

	// Удаляется только подписка Bob, подписка Carol остаётся.
	got, err := us.GetFollowers(t.Context(), alice.ID)
	if err != nil {
		t.Fatalf("GetFollowers returned error: %v", err)
	}
	if want := sortedIDs(carol); !slices.Equal(followerIDs(got), want) {
		t.Errorf("followers = %v, want %v", followerIDs(got), want)
	}

	t.Run("not subscribed is not an error", func(t *testing.T) {
		// Повторная отписка и отписка от несуществующего пользователя ничего не делают.
		for _, followee := range []string{alice.ID, missingID} {
			if err := us.Unsubscribe(t.Context(), bob.ID, followee); err != nil {
				t.Errorf("Unsubscribe(%s) returned error: %v", followee, err)
			}
		}
	})
}

func TestDeletingUserRemovesSubscriptions(t *testing.T) {
	us := newUserStorage(t)
	alice := createUser(t, us, "Alice", "alice@example.com")
	bob := createUser(t, us, "Bob", "bob@example.com")
	subscribe(t, us, bob, alice)
	subscribe(t, us, alice, bob)

	// Удаления пользователя в сервисе пока нет, но схема должна к нему быть готова.
	if _, err := testDB.ExecContext(t.Context(), "DELETE FROM users WHERE id = $1", bob.ID); err != nil {
		t.Fatalf("failed to delete user: %v", err)
	}

	got, err := us.GetFollowers(t.Context(), alice.ID)
	if err != nil {
		t.Fatalf("GetFollowers returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("followers of Alice = %v, want none", got)
	}

	var left int
	if err := testDB.QueryRowContext(t.Context(), "SELECT count(*) FROM subscriptions").Scan(&left); err != nil {
		t.Fatalf("failed to count subscriptions: %v", err)
	}
	if left != 0 {
		t.Errorf("%d subscriptions left, want 0", left)
	}
}

// ----------------------------------------------------------------------------
// Отменённый контекст
// ----------------------------------------------------------------------------

func TestCanceledContext(t *testing.T) {
	us := newUserStorage(t)
	user := createUser(t, us, "Alice", "alice@example.com")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	calls := map[string]func() error{
		"CreateUser": func() error {
			_, err := us.CreateUser(ctx, "Bob", "bob@example.com", []byte("hash"))
			return err
		},
		"GetUserByEmail": func() error { _, err := us.GetUserByEmail(ctx, "alice@example.com"); return err },
		"GetUserByID":    func() error { _, err := us.GetUserByID(ctx, user.ID); return err },
		"GetUsersByDisplayName": func() error {
			_, err := us.GetUsersByDisplayName(ctx, "Ali")
			return err
		},
		"Subscribe":    func() error { return us.Subscribe(ctx, user.ID, missingID) },
		"Unsubscribe":  func() error { return us.Unsubscribe(ctx, user.ID, missingID) },
		"GetFollowers": func() error { _, err := us.GetFollowers(ctx, user.ID); return err },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			// Запрос не выполняется: отмена клиентом не должна выглядеть как "не найдено".
			if err := call(); !errors.Is(err, context.Canceled) {
				t.Errorf("got error %v, want %v", err, context.Canceled)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Storage
// ----------------------------------------------------------------------------

func TestStorage_PingAndClose(t *testing.T) {
	storage, err := postgresql.NewStorage(testDSN)
	if err != nil {
		t.Fatalf("NewStorage returned error: %v", err)
	}

	if err := storage.Ping(t.Context()); err != nil {
		t.Errorf("Ping returned error: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
	if err := storage.Ping(t.Context()); err == nil {
		t.Error("Ping after Close succeeded, want error")
	}
}
