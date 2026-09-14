package config

import "time"

// Redis configures the shared Redis client.
type Redis struct {
	Addr         string        `env:"ADDR" envDefault:"localhost:6379"`
	Password     string        `env:"PASSWORD"`
	DB           int           `env:"DB" envDefault:"0"`
	PoolSize     int           `env:"POOL_SIZE" envDefault:"64"`
	MinIdleConns int           `env:"MIN_IDLE_CONNS" envDefault:"8"`
	ReadTimeout  time.Duration `env:"READ_TIMEOUT" envDefault:"300ms"`
	DialTimeout  time.Duration `env:"DIAL_TIMEOUT" envDefault:"1s"`
}
