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
	ServicesWithEmailHiddenRaw string        `envconfig:"SERVICES_WITH_EMAIL_HIDDEN"`
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
		return nil, fmt.Errorf("%s: env variable ENV not set", op)
	}
	if cfg.Database.User == "" {
		return nil, fmt.Errorf("%s: env variable DB_USER not set", op)
	}
	if cfg.Database.Password == "" {
		return nil, fmt.Errorf("%s: env variable DB_PASSWORD not set", op)
	}
	if cfg.Database.DbName == "" {
		return nil, fmt.Errorf("%s: env variable DB_NAME not set", op)
	}
	if cfg.Database.Host == "" {
		return nil, fmt.Errorf("%s: env variable DB_HOST not set", op)
	}
	if cfg.Database.Port == 0 {
		return nil, fmt.Errorf("%s: env variable DB_PORT not set", op)
	}
	if cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("%s: env variable JWT_SECRET not set", op)
	}

	cfg.GRPCServer.ServicesWithEmailHidden = strings.Split(cfg.GRPCServer.ServicesWithEmailHiddenRaw, ",")

	return cfg, nil
}
