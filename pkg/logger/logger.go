// Package logger builds the process-wide zerolog logger.
package logger

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/4jawaly/identity-gateway/internal/config"
)

// New builds a logger from config. Unknown levels fall back to info.
func New(cfg config.Log) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	lvl, err := zerolog.ParseLevel(strings.ToLower(cfg.Level))
	if err != nil || lvl == zerolog.NoLevel {
		lvl = zerolog.InfoLevel
	}
	var out = zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	if strings.EqualFold(cfg.Format, "json") {
		return zerolog.New(os.Stderr).Level(lvl).With().Timestamp().Logger()
	}
	return zerolog.New(out).Level(lvl).With().Timestamp().Logger()
}
