package config

// Log configures zerolog.
type Log struct {
	Level  string `env:"LEVEL" envDefault:"info"`
	Format string `env:"FORMAT" envDefault:"json"` // json | console
	// SampleRate keeps 1 in N successful (2xx/3xx) access log lines. 4xx/5xx are always logged.
	SampleRate uint32 `env:"SAMPLE_RATE" envDefault:"100"`
}
