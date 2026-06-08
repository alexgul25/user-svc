package handlersgrpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonv1 "github.com/alexgul25/protos/gen/go/common/v1"
	userv1 "github.com/alexgul25/protos/gen/go/user/v1"
	"github.com/alexgul25/user-svc/internal/domain/models"
	userlogic "github.com/alexgul25/user-svc/internal/service/user"
	"github.com/alexgul25/user-svc/internal/storage"
)

type UserService interface {
	Register(ctx context.Context, displayName, email, password string) (user models.User, err error)
	Login(ctx context.Context, email, password string) (token string, err error)
	GetMyProfile(ctx context.Context, userID string) (models.User, error)
	Subscribe(ctx context.Context, followerID, followeeID string) error
	Unsubscribe(ctx context.Context, followerID, followeeID string) error
	GetFollowers(ctx context.Context, userID string) ([]models.Follower, error)
}

type serverAPI struct {
	userv1.UnimplementedUserServiceServer
	userService UserService
}

func Register(gRPCServer *grpc.Server, userServer UserService) {
	userv1.RegisterUserServiceServer(gRPCServer, &serverAPI{userService: userServer})
}

func (s *serverAPI) Register(ctx context.Context, in *userv1.RegisterRequest) (*userv1.RegisterResponse, error) {
	if in.DisplayName == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	if in.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}

	if in.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "password is required")
	}

	user, err := s.userService.Register(ctx, in.GetDisplayName(), in.GetEmail(), in.GetPassword())
	if err != nil {
		if errors.Is(err, storage.ErrUserExists) {
			return nil, status.Error(codes.AlreadyExists, "user already exists")
		}

		return nil, status.Error(codes.Internal, "failed to register user")
	}

	return &userv1.RegisterResponse{
		UserId:      user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		CreatedAt:   user.CreatedAt.Format(time.RFC3339),
	}, nil
}

func (s *serverAPI) Login(ctx context.Context, in *userv1.LoginRequest) (*userv1.LoginResponse, error) {
	if in.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}

	if in.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "password is required")
	}

	token, err := s.userService.Login(ctx, in.GetEmail(), in.GetPassword())
	if err != nil {
		if errors.Is(err, userlogic.ErrInvalidCredentials) {
			return nil, status.Error(codes.InvalidArgument, "invalid email or password")
		}

		return nil, status.Error(codes.Internal, "failed to login user")
	}

	return &userv1.LoginResponse{AccessToken: token}, nil
}

func (s *serverAPI) GetMyProfile(ctx context.Context, in *userv1.GetMyProfileRequest) (*userv1.ProfileResponse, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "metadata is required")
	}

	values := md.Get("x-user-id")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "user id is required")
	}
	userID := values[0]

	user, err := s.userService.GetMyProfile(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get user profile")
	}

	return &userv1.ProfileResponse{
		UserId:      user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		CreatedAt:   user.CreatedAt.Format(time.RFC3339),
	}, nil
}

func (s *serverAPI) Subscribe(ctx context.Context, in *userv1.SubscribeRequest) (*commonv1.Empty, error) {
	if in.FolloweeId == "" {
		return nil, status.Error(codes.InvalidArgument, "followee id is required")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "metadata is required")
	}

	values := md.Get("x-user-id")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "follower id is required")
	}
	followerID := values[0]

	if err := s.userService.Subscribe(ctx, followerID, in.GetFolloweeId()); err != nil {
		if errors.Is(err, storage.ErrAlreadySubscribed) {
			return nil, status.Error(codes.InvalidArgument, "user has already subscribed")
		}
		if errors.Is(err, userlogic.ErrSelfSubscription) {
			return nil, status.Error(codes.InvalidArgument, "cannot subscribe to yourself")
		}

		return nil, status.Error(codes.Internal, "failed to subscribe user")
	}

	return &commonv1.Empty{}, nil
}

func (s *serverAPI) Unsubscribe(ctx context.Context, in *userv1.UnsubscribeRequest) (*commonv1.Empty, error) {
	if in.FolloweeId == "" {
		return nil, status.Error(codes.InvalidArgument, "followee id is required")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "metadata is required")
	}

	values := md.Get("x-user-id")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "follower id is required")
	}
	followerID := values[0]

	if err := s.userService.Unsubscribe(ctx, followerID, in.GetFolloweeId()); err != nil {
		if errors.Is(err, userlogic.ErrSelfSubscription) {
			return nil, status.Error(codes.InvalidArgument, "cannot unsubscribe from yourself")
		}

		return nil, status.Error(codes.Internal, "failed to subscribe user")
	}

	return &commonv1.Empty{}, nil
}

func (s *serverAPI) GetFollowers(ctx context.Context, in *userv1.GetFollowersRequest) (*userv1.GetFollowersResponse, error) {
	if in.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}

	followers, err := s.userService.GetFollowers(ctx, in.GetUserId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get user followers")
	}

	grpcFollowers := make([]*userv1.Follower, 0, len(followers))
	for _, follower := range followers {
		grpcFollowers = append(
			grpcFollowers,
			&userv1.Follower{UserId: follower.ID, DisplayName: follower.DisplayName, Email: follower.Email},
		)
	}

	return &userv1.GetFollowersResponse{Followers: grpcFollowers}, nil
}
