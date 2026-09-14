// Package config loads the process configuration from environment variables.
//
// There is no validation layer by design: typed parsing plus envDefault tags
// are the contract. A missing .env file is not an error.
package config

import (
	"fmt"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config is the root configuration, one nested struct per section.
type Config struct {
	App            App            `envPrefix:"APP_"`
	Server         Server         `envPrefix:"SERVER_"`
	Database       Database       `envPrefix:"DB_"`
	Redis          Redis          `envPrefix:"REDIS_"`
	Cache          Cache          `envPrefix:"CACHE_"`
	Queue          Queue          `envPrefix:"QUEUE_"`
	Zitadel        Zitadel        `envPrefix:"ZITADEL_"`
	CircuitBreaker CircuitBreaker // explicit HTTP_CIRCUIT_BREAKER_* names, no prefix
	Log            Log            `envPrefix:"LOG_"`
}

// Load reads an optional .env file and then parses the environment.
func Load() (*Config, error) {
	_ = godotenv.Load() // a missing .env is fine; real env always wins
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}
	return &cfg, nil
}

// MustLoad is Load for main(): there is no logger yet, so it writes to stderr
// and exits with status 1 on failure.
func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

const redacted = "***"

// Redacted returns a copy with secrets masked so the config can be logged.
func (c Config) Redacted() Config {
	if c.Database.Password != "" {
		c.Database.Password = redacted
	}
	if c.Queue.URL != "" {
		c.Queue.URL = redacted
	}
	if c.Redis.Password != "" {
		c.Redis.Password = redacted
	}
	return c
}
