package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Env        string `envconfig:"ENV"`
	Database   DatabaseConfig
	GRPCServer GRPCServerConfig
	JWT        JWTConfig
}

type DatabaseConfig struct {
	User     string `envconfig:"DB_USER"`
	Password string `envconfig:"DB_PASSWORD"`
	DbName   string `envconfig:"DB_NAME"`
	Host     string `envconfig:"DB_HOST"`
	Port     int    `envconfig:"DB_PORT"`
}

type GRPCServerConfig struct {
	Port                       int           `envconfig:"GRPCSERVER_PORT" env-default:"50051"`
	GracefulTimeout            time.Duration `envconfig:"GRACEFUL_TIMEOUT" env-default:"10s"`
	servicesWithEmailHiddenRaw string        `envconfig:"SERVICES_WITH_EMAIL_HIDDEN"`
	ServicesWithEmailHidden    []string
}

type JWTConfig struct {
	Secret   string        `envconfig:"JWT_SECRET"`
	TokenTTL time.Duration `envconfig:"JWT_ACCESS_TTL" env-default:"24h"`
}

func load() (*Config, error) {
	const op = "load"

	err := godotenv.Load()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	var cfg Config
	err = envconfig.Process("", &cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &cfg, nil
}

func LoadUserService() (*Config, error) {
	const op = "LoadUserService"

	cfg, err := load()
	if err != nil {
		return nil, err
	}

	if cfg.Env == "" {
		return nil, fmt.Errorf("%s env variable not set: ENV", op)
	}
	if cfg.Database.User == "" {
		return nil, fmt.Errorf("%s env variable not set: DB_USER", op)
	}
	if cfg.Database.Password == "" {
		return nil, fmt.Errorf("%s env variable not set: DB_PASSWORD", op)
	}
	if cfg.Database.DbName == "" {
		return nil, fmt.Errorf("%s env variable not set: DB_NAME", op)
	}
	if cfg.Database.Host == "" {
		return nil, fmt.Errorf("%s env variable not set: DB_HOST", op)
	}
	if cfg.Database.Port == 0 {
		return nil, fmt.Errorf("%s env variable not set: DB_PORT", op)
	}
	if cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("%s env variable not set: JWT_SECRET", op)
	}

	cfg.GRPCServer.ServicesWithEmailHidden = strings.Split(cfg.GRPCServer.servicesWithEmailHiddenRaw, ",")

	return cfg, nil
}
