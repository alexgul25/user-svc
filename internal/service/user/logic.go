package userlogic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"golang.org/x/crypto/bcrypt"

	"github.com/alexgul25/user-svc/internal/domain"
	"github.com/alexgul25/user-svc/internal/domain/models"
	jwtmanager "github.com/alexgul25/user-svc/internal/lib/jwt"
)

type UserRepository interface {
	CreateUser(ctx context.Context, displayName string, email string, passwordHash []byte) (user models.User, err error)
	GetUserByEmail(ctx context.Context, email string) (models.User, error)
	GetUserByID(ctx context.Context, userID string) (models.User, error)
	GetUsersByDisplayName(ctx context.Context, searchQuery string) (users []models.PublicUser, err error)
}

type SubRepository interface {
	Subscribe(ctx context.Context, followerID string, followeeID string) error
	Unsubscribe(ctx context.Context, followerID string, followeeID string) error
	GetFollowers(ctx context.Context, userID string) ([]models.Follower, error)
}

type UserLogic struct {
	log       *slog.Logger
	usrRepo   UserRepository
	subRepo   SubRepository
	jwtManger *jwtmanager.Manager
}

func New(
	log *slog.Logger,
	usrRepo UserRepository,
	subRepo SubRepository,
	jwtManger *jwtmanager.Manager,
) *UserLogic {
	return &UserLogic{
		log:       log,
		usrRepo:   usrRepo,
		subRepo:   subRepo,
		jwtManger: jwtManger,
	}
}

func (ul *UserLogic) Register(ctx context.Context, displayName, email, password string) (models.User, error) {
	const op = "UserLogic.Register"

	log := ul.log.With(
		slog.String("op", op),
		slog.String("email", email),
	)

	log.Info("attempting to register user")

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Error("failed to generate password hash", slog.Any("error", err))

		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}

	user, err := ul.usrRepo.CreateUser(ctx, displayName, email, passwordHash)
	if err != nil {
		log.Error("failed to save user", slog.Any("error", err))

		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}
	user.PasswordHash = []byte{}

	log.Info("user registered successfully")

	return user, nil
}

func (ul *UserLogic) Login(ctx context.Context, email, password string) (string, error) {
	const op = "UserLogic.Login"

	log := ul.log.With(
		slog.String("op", op),
		slog.String("email", email),
	)

	log.Info("attempting to login user")

	user, err := ul.usrRepo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			log.Warn("user not found", slog.Any("error", err))

			return "", fmt.Errorf("%s: %w", op, domain.ErrInvalidCredentials)
		}

		log.Error("failed to get user", slog.Any("error", err))

		return "", fmt.Errorf("%s: %w", op, err)
	}

	if err := bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)); err != nil {
		log.Info("invalid credentials", slog.Any("error", err))

		return "", fmt.Errorf("%s: %w", op, domain.ErrInvalidCredentials)
	}

	log.Info("user logged successfully")

	token, err := ul.jwtManger.NewToken(user.ID)
	if err != nil {
		log.Error("failed to generate token", slog.Any("error", err))

		return "", fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (ul *UserLogic) GetMyProfile(ctx context.Context, userID string) (models.User, error) {
	const op = "UserLogic.GetMyProfile"

	log := ul.log.With(
		slog.String("op", op),
		slog.String("user_id", userID),
	)

	log.Info("attempting to get user profile")

	user, err := ul.usrRepo.GetUserByID(ctx, userID)
	if err != nil {
		log.Error("failed to get user", slog.Any("error", err))

		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}
	user.PasswordHash = []byte{}

	log.Info("got user successfully")

	return user, nil
}

func (ul *UserLogic) FindUsersByDisplayName(ctx context.Context, searchQuery string) ([]models.PublicUser, error) {
	const op = "UserLogic.FindUsersByDisplayName"

	log := ul.log.With(
		slog.String("op", op),
		slog.String("search_query", searchQuery),
	)

	log.Info("attempting to find users by display name")

	users, err := ul.usrRepo.GetUsersByDisplayName(ctx, searchQuery)
	if err != nil {
		log.Error("failed to find users by display name", slog.Any("error", err))

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	log.Info("got users by display name successfully")

	return users, nil
}

func (ul *UserLogic) Subscribe(ctx context.Context, followerID, followeeID string) error {
	const op = "UserLogic.Subscribe"

	log := ul.log.With(
		slog.String("op", op),
		slog.String("follower", followerID),
		slog.String("followee", followeeID),
	)

	log.Info("attempting to subscribe user")

	if followeeID == followerID {
		log.Warn("self subscription attempt")
		return fmt.Errorf("%s: %w", op, domain.ErrSelfSubscription)
	}

	if err := ul.subRepo.Subscribe(ctx, followerID, followeeID); err != nil {
		log.Error("failed to subscribe user", slog.Any("error", err))

		return fmt.Errorf("%s: %w", op, err)
	}

	log.Info("user subscribed successfully")

	return nil
}

func (ul *UserLogic) Unsubscribe(ctx context.Context, followerID, followeeID string) error {
	const op = "UserLogic.Unsubscribe"

	log := ul.log.With(
		slog.String("op", op),
		slog.String("follower", followerID),
		slog.String("followee", followeeID),
	)

	log.Info("attempting to unsubscribe user")

	if followeeID == followerID {
		log.Warn("self subscription attempt")
		return fmt.Errorf("%s: %w", op, domain.ErrSelfSubscription)
	}

	if err := ul.subRepo.Unsubscribe(ctx, followerID, followeeID); err != nil {
		log.Error("failed to unsubscribe user", slog.Any("error", err))

		return fmt.Errorf("%s: %w", op, err)
	}

	log.Info("user unsubscribed successfully")

	return nil
}

func (ul *UserLogic) GetFollowers(ctx context.Context, userID string) ([]models.Follower, error) {
	const op = "UserLogic.GetFollowers"

	log := ul.log.With(
		slog.String("op", op),
		slog.String("user_id", userID),
	)

	log.Info("attempting to get user followers")

	followers, err := ul.subRepo.GetFollowers(ctx, userID)
	if err != nil {
		log.Error("failed to get followers", slog.Any("error", err))

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	log.Info("got user followers successfully")

	return followers, nil
}
