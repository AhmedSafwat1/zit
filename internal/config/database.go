package config

import (
	"net/url"
	"strconv"
	"time"
)

// Database configures the Postgres pool from its parts. The worker always
// opens it; the API opens it only when Enabled() (DB_HOST set).
type Database struct {
	Host     string `env:"HOST"`
	Port     int    `env:"PORT" envDefault:"5432"`
	User     string `env:"USER" envDefault:"identity"`
	Password string `env:"PASSWORD"`
	Name     string `env:"NAME" envDefault:"identity"`
	SSLMode  string `env:"SSLMODE" envDefault:"disable"`

	MaxOpenConns    int           `env:"MAX_OPEN_CONNS" envDefault:"16"`
	MaxIdleConns    int           `env:"MAX_IDLE_CONNS" envDefault:"4"`
	ConnMaxLifetime time.Duration `env:"CONN_MAX_LIFETIME" envDefault:"30m"`
}

// Enabled reports whether a database is configured at all.
func (d Database) Enabled() bool { return d.Host != "" }

// DSN renders the connection URL. User and password are escaped.
func (d Database) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		Host:   d.Host + ":" + strconv.Itoa(d.Port),
		Path:   "/" + d.Name,
	}
	if d.Password != "" {
		u.User = url.UserPassword(d.User, d.Password)
	} else if d.User != "" {
		u.User = url.User(d.User)
	}
	q := url.Values{}
	if d.SSLMode != "" {
		q.Set("sslmode", d.SSLMode)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
