package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/alexgul25/user-svc/internal/domain"
	"github.com/alexgul25/user-svc/internal/domain/models"
)

type UserStorage struct {
	db *sql.DB
}

func NewUserStorage(db *sql.DB) *UserStorage {
	return &UserStorage{db: db}
}

func (us *UserStorage) CreateUser(ctx context.Context, displayName, email string, passwordHash []byte) (models.User, error) {
	const op = "postgresql.UserStorage.SaveUser"

	query := `
		INSERT INTO users (display_name, email, password_hash, created_at)
		VALUES ($1, $2, $3, NOW())
		RETURNING id, created_at
	`

	user := models.User{DisplayName: displayName, Email: email, PasswordHash: passwordHash}
	row := us.db.QueryRowContext(ctx, query, displayName, email, passwordHash)
	err := row.Scan(&user.ID, &user.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return models.User{}, fmt.Errorf("%s: %w", op, domain.ErrUserExists)
		}

		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}

	return user, nil
}

func (us *UserStorage) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	const op = "postgresql.UserStorage.GetUserByEmail"

	query := `
		SELECT id, display_name, password_hash, created_at
		FROM users
		WHERE email = $1	
	`

	row := us.db.QueryRowContext(ctx, query, email)
	user := models.User{Email: email}
	err := row.Scan(&user.ID, &user.DisplayName, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, fmt.Errorf("%s: %w", op, domain.ErrUserNotFound)
		}

		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}

	return user, nil
}

func (us *UserStorage) GetUserByID(ctx context.Context, userID string) (models.User, error) {
	const op = "postgresql.UserStorage.GetUserByID"

	query := `
		SELECT display_name, email, password_hash, created_at
		FROM users
		WHERE id = $1	
	`

	row := us.db.QueryRowContext(ctx, query, userID)
	user := models.User{ID: userID}
	err := row.Scan(&user.DisplayName, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, fmt.Errorf("%s: %w", op, domain.ErrUserNotFound)
		}

		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}

	return user, nil
}

func (us *UserStorage) GetUsersByDisplayName(ctx context.Context, searchQuery string) ([]models.PublicUser, error) {
	const op = "postgresql.UserStorage.GetUsersByDisplayName"

	query := `
		SELECT id, display_name, created_at
		FROM users
		WHERE display_name ILIKE CONCAT($1, '%')
	`

	rows, err := us.db.QueryContext(ctx, query, searchQuery)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	users := []models.PublicUser{}
	for rows.Next() {
		var user models.PublicUser

		if err := rows.Scan(&user.ID, &user.DisplayName, &user.CreatedAt); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}

		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return users, nil
}

func (us *UserStorage) Subscribe(ctx context.Context, followerID, followeeID string) error {
	const op = "postgresql.UserStorage.Subscribe"

	query := `
		INSERT INTO subscriptions (follower_id, followee_id)
		VALUES ($1, $2)	
	`

	_, err := us.db.ExecContext(ctx, query, followerID, followeeID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return fmt.Errorf("%s: %w", op, domain.ErrAlreadySubscribed)
		}

		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (us *UserStorage) Unsubscribe(ctx context.Context, followerID, followeeID string) error {
	const op = "postgresql.UserStorage.Unsubscribe"

	query := `
		DELETE FROM subscriptions
		WHERE follower_id = $1 AND followee_id = $2	
	`

	_, err := us.db.ExecContext(ctx, query, followerID, followeeID)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (us *UserStorage) GetFollowers(ctx context.Context, userID string) ([]models.Follower, error) {
	const op = "postgresql.UserStorage.GetFollowers"

	query := `
		SELECT id, display_name, email
		FROM users
		WHERE id IN (
			SELECT follower_id
			FROM subscriptions
			WHERE followee_id = $1)	
	`

	rows, err := us.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	followers := []models.Follower{}
	for rows.Next() {
		var follower models.Follower

		if err := rows.Scan(&follower.ID, &follower.DisplayName, &follower.Email); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}

		followers = append(followers, follower)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return followers, nil
}
