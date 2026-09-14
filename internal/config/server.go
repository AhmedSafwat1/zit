package config

import "time"

// Server configures the public HTTP listener and the internal pprof listener.
type Server struct {
	Port      int `env:"PORT" envDefault:"8080"`
	PprofPort int `env:"PPROF_PORT" envDefault:"6060"`
	// The worker runs its own health/pprof listeners on separate ports so
	// both binaries can share a host.
	WorkerPort      int           `env:"WORKER_PORT" envDefault:"8082"`
	WorkerPprofPort int           `env:"WORKER_PPROF_PORT" envDefault:"6061"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT" envDefault:"5s"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT" envDefault:"10s"`
	IdleTimeout     time.Duration `env:"IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s"`
	MaxBodyBytes    int64         `env:"MAX_BODY_BYTES" envDefault:"1048576"`
	RateLimitRPS    float64       `env:"RATE_LIMIT_RPS" envDefault:"0"`
	RateLimitBurst  int           `env:"RATE_LIMIT_BURST" envDefault:"0"`
}

// ForWorker returns the listener settings the worker binary uses.
func (s Server) ForWorker() Server {
	s.Port, s.PprofPort = s.WorkerPort, s.WorkerPprofPort
	return s
}
