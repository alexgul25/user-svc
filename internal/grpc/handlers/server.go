package handlersgrpc

import (
	"context"
	"errors"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "github.com/alexgul25/protos/gen/go/user/v1"
	"github.com/alexgul25/user-svc/internal/domain"
	"github.com/alexgul25/user-svc/internal/domain/models"
	"github.com/alexgul25/user-svc/internal/grpc/interceptors"
)

type UserService interface {
	Register(ctx context.Context, displayName, email, password string) (user models.User, err error)
	Login(ctx context.Context, email, password string) (token string, err error)
	GetMyProfile(ctx context.Context, userID string) (models.User, error)
	FindUsersByDisplayName(ctx context.Context, searchQuery string) ([]models.PublicUser, error)
	Subscribe(ctx context.Context, followerID, followeeID string) error
	Unsubscribe(ctx context.Context, followerID, followeeID string) error
	GetFollowers(ctx context.Context, userID string) ([]models.Follower, error)
}

type serverAPI struct {
	userv1.UnimplementedUserServiceServer
	userService             UserService
	servicesWithEmailHidden []string
}

func Register(gRPCServer *grpc.Server, userServer UserService, servicesWithEmailHidden []string) {
	userv1.RegisterUserServiceServer(
		gRPCServer,
		&serverAPI{
			userService:             userServer,
			servicesWithEmailHidden: servicesWithEmailHidden,
		},
	)
}

func (s *serverAPI) Register(ctx context.Context, in *userv1.RegisterRequest) (*userv1.RegisterResponse, error) {
	if in.GetDisplayName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	if in.GetEmail() == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}

	if in.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "password is required")
	}

	user, err := s.userService.Register(ctx, in.GetDisplayName(), in.GetEmail(), in.GetPassword())
	if err != nil {
		if errors.Is(err, domain.ErrUserExists) {
			return nil, status.Error(codes.AlreadyExists, "user already exists")
		}

		return nil, status.Error(codes.Internal, "failed to register user")
	}

	return &userv1.RegisterResponse{
		UserId:      user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		CreatedAt:   timestamppb.New(user.CreatedAt),
	}, nil
}

func (s *serverAPI) Login(ctx context.Context, in *userv1.LoginRequest) (*userv1.LoginResponse, error) {
	if in.GetEmail() == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}

	if in.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "password is required")
	}

	token, err := s.userService.Login(ctx, in.GetEmail(), in.GetPassword())
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			return nil, status.Error(codes.InvalidArgument, "invalid email or password")
		}

		return nil, status.Error(codes.Internal, "failed to login user")
	}

	return &userv1.LoginResponse{AccessToken: token}, nil
}

func (s *serverAPI) GetMyProfile(ctx context.Context, _ *emptypb.Empty) (*userv1.GetMyProfileResponse, error) {
	userID, ok := interceptors.GetUserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user id is required")
	}

	user, err := s.userService.GetMyProfile(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get user profile")
	}

	return &userv1.GetMyProfileResponse{
		UserId:      user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		CreatedAt:   timestamppb.New(user.CreatedAt),
	}, nil
}

func (s *serverAPI) FindUsersByDisplayName(ctx context.Context, in *userv1.FindUsersByDisplayNameRequest) (*userv1.FindUsersByDisplayNameResponse, error) {
	if in.GetSearchQuery() == "" {
		return nil, status.Error(codes.InvalidArgument, "search query is required")
	}

	_, ok := interceptors.GetUserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user id is required")
	}

	publicUsers, err := s.userService.FindUsersByDisplayName(ctx, in.GetSearchQuery())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to find users by display name")
	}

	grpcUsers := make([]*userv1.User, len(publicUsers))
	for i, u := range publicUsers {
		grpcUsers[i] = &userv1.User{UserId: u.ID, DisplayName: u.DisplayName, CreatedAt: timestamppb.New(u.CreatedAt)}
	}

	return &userv1.FindUsersByDisplayNameResponse{Users: grpcUsers}, nil
}

func (s *serverAPI) Subscribe(ctx context.Context, in *userv1.SubscribeRequest) (*emptypb.Empty, error) {
	if in.GetFolloweeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "followee id is required")
	}

	followerID, ok := interceptors.GetUserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user id is required")
	}

	if err := s.userService.Subscribe(ctx, followerID, in.GetFolloweeId()); err != nil {
		if errors.Is(err, domain.ErrAlreadySubscribed) {
			return nil, status.Error(codes.InvalidArgument, "user has already subscribed")
		}
		if errors.Is(err, domain.ErrSelfSubscription) {
			return nil, status.Error(codes.InvalidArgument, "cannot subscribe to yourself")
		}

		return nil, status.Error(codes.Internal, "failed to subscribe user")
	}

	return &emptypb.Empty{}, nil
}

func (s *serverAPI) Unsubscribe(ctx context.Context, in *userv1.UnsubscribeRequest) (*emptypb.Empty, error) {
	if in.GetFolloweeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "followee id is required")
	}

	followerID, ok := interceptors.GetUserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user id is required")
	}

	if err := s.userService.Unsubscribe(ctx, followerID, in.GetFolloweeId()); err != nil {
		if errors.Is(err, domain.ErrSelfSubscription) {
			return nil, status.Error(codes.InvalidArgument, "cannot unsubscribe from yourself")
		}

		return nil, status.Error(codes.Internal, "failed to unsubscribe user")
	}

	return &emptypb.Empty{}, nil
}

func (s *serverAPI) GetFollowers(ctx context.Context, in *userv1.GetFollowersRequest) (*userv1.GetFollowersResponse, error) {
	if in.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}

	serviceName, ok := interceptors.GetServiceNameFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing service name")
	}

	followers, err := s.userService.GetFollowers(ctx, in.GetUserId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get user followers")
	}

	return s.toGetFollowersResponse(followers, serviceName), nil
}

func (s *serverAPI) toGetFollowersResponse(followers []models.Follower, callerService string) *userv1.GetFollowersResponse {
	hideEmail := slices.Contains(s.servicesWithEmailHidden, callerService)
	grpcFollowers := make([]*userv1.Follower, len(followers))
	for i, follower := range followers {
		if hideEmail {
			follower.Email = ""
		}
		grpcFollowers[i] = &userv1.Follower{UserId: follower.ID, DisplayName: follower.DisplayName, Email: follower.Email}
	}

	return &userv1.GetFollowersResponse{Followers: grpcFollowers}
}
