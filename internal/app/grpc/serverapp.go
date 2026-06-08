package grpcapp

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	handlersgrpc "github.com/alexgul25/user-svc/internal/grpc/handlers"
)

type ServerApp struct {
	log        *slog.Logger
	gRPCServer *grpc.Server
	port       int
}

func New(log *slog.Logger, userService handlersgrpc.UserService, port int) *ServerApp {
	recoveryOpts := []recovery.Option{
		recovery.WithRecoveryHandler(func(p any) (err error) {
			log.Error("recovered from panic", slog.Any("panic", p))

			return status.Error(codes.Internal, "internal error")
		}),
	}

	gRPCServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		recovery.UnaryServerInterceptor(recoveryOpts...),
		logging.UnaryServerInterceptor(InterceptorLogger(log)),
	))

	handlersgrpc.Register(gRPCServer, userService)

	return &ServerApp{
		log:        log,
		gRPCServer: gRPCServer,
		port:       port,
	}
}

func InterceptorLogger(l *slog.Logger) logging.Logger {
	return logging.LoggerFunc(func(ctx context.Context, level logging.Level, msg string, fields ...any) {
		l.Log(ctx, slog.Level(level), msg, fields...)
	})
}

func (sa *ServerApp) MustRun() {
	if err := sa.Run(); err != nil {
		panic(err)
	}
}

func (sa *ServerApp) Run() error {
	const op = "ServerApp.Run"

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", sa.port))
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	sa.log.Info("grpc server started", slog.String("addr", l.Addr().String()))

	if err := sa.gRPCServer.Serve(l); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (sa *ServerApp) Stop() {
	const op = "ServerApp.Stop"

	sa.log.With(slog.String("op", op)).Info("stopping grpc server", slog.Int("port", sa.port))

	sa.gRPCServer.GracefulStop()
}
