package interceptors

import (
	"context"
	"log/slog"
	"slices"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// all headers: "x-api-key", "x-service-name", "x-user-id"
var (
	loggedHeaders     = []string{"x-service-name"}
	forContextHeaders = []string{"x-service-name", "x-user-id"}
)

var methodAccess = map[string][]string{
	"/user.v1.UserService/Register":     {"gateway-svc"},
	"/user.v1.UserService/Login":        {"gateway-svc"},
	"/user.v1.UserService/GetMyProfile": {"gateway-svc"},
	"/user.v1.UserService/Subscribe":    {"gateway-svc"},
	"/user.v1.UserService/Unsubscribe":  {"gateway-svc"},
	"/user.v1.UserService/GetFollowers": {"gateway-svc", "notification-svc"},
}

func NewRecoveryInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	recoveryOpts := []recovery.Option{
		recovery.WithRecoveryHandler(func(p any) (err error) {
			log.Error("recovered from panic", slog.Any("panic", p))

			return status.Error(codes.Internal, "internal error")
		}),
	}

	return recovery.UnaryServerInterceptor(recoveryOpts...)
}

func interceptorLogger(log *slog.Logger) logging.Logger {
	return logging.LoggerFunc(func(ctx context.Context, level logging.Level, msg string, fields ...any) {
		var lvl slog.Level
		switch level {
		case logging.LevelInfo:
			lvl = slog.LevelInfo
		case logging.LevelDebug:
			lvl = slog.LevelDebug
		case logging.LevelWarn:
			lvl = slog.LevelWarn
		case logging.LevelError:
			lvl = slog.LevelError
		default:
			lvl = slog.LevelInfo
		}

		log.Log(ctx, lvl, msg, fields...)
	})
}

func NewLoggingInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	loggingOpts := []logging.Option{
		logging.WithFieldsFromContext(func(ctx context.Context) logging.Fields {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil
			}

			fields := logging.Fields{}
			for _, header := range loggedHeaders {
				if values := md.Get(header); len(values) != 0 {
					fields = append(fields, header, values[0])
				}
			}

			return fields
		}),
	}

	return logging.UnaryServerInterceptor(interceptorLogger(log), loggingOpts...)
}

func NewValidationInterceptor(apiKey string) grpc.UnaryServerInterceptor {
	return grpc.UnaryServerInterceptor(
		func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, status.Error(codes.Unauthenticated, "metadata is required")
			}

			values := md.Get("x-api-key")
			if len(values) == 0 {
				return nil, status.Error(codes.Unauthenticated, "missing required header: x-api-key")
			}
			receivedKey := values[0]
			if receivedKey != apiKey {
				return nil, status.Error(codes.Unauthenticated, "wrong api key")
			}

			values = md.Get("x-service-name")
			if len(values) == 0 {
				return nil, status.Error(codes.Unauthenticated, "missing required header: x-service-name")
			}
			receivedName := values[0]
			allowedNames, ok := methodAccess[info.FullMethod]
			if !ok {
				return nil, status.Error(codes.PermissionDenied, "unknown method")
			}

			if !slices.Contains(allowedNames, receivedName) {
				return nil, status.Error(codes.PermissionDenied, "your service not allowed to call this method")
			}

			return handler(ctx, req)
		},
	)
}

type ctxKey string

func GetServiceNameFromContext(ctx context.Context) (string, bool) {
	res, ok := ctx.Value(ctxKey("x-service-name")).(string)
	return res, ok
}

func GetUserIDFromContext(ctx context.Context) (string, bool) {
	res, ok := ctx.Value(ctxKey("x-user-id")).(string)
	return res, ok
}

func NewContextEnricherInterceptor() grpc.UnaryServerInterceptor {
	return grpc.UnaryServerInterceptor(
		func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if ok {
				for _, header := range forContextHeaders {
					if values := md.Get(header); len(values) > 0 && values[0] != "" {
						ctx = context.WithValue(ctx, ctxKey(header), values[0])
					}
				}
			}

			return handler(ctx, req)
		},
	)
}
