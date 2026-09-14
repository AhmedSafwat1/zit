package config

import "time"

// Queue configures the RabbitMQ publisher (API) and consumer (worker).
type Queue struct {
	URL            string        `env:"URL"`
	Exchange       string        `env:"EXCHANGE" envDefault:"zit.events"`
	PublishTimeout time.Duration `env:"PUBLISH_TIMEOUT" envDefault:"500ms"`
	Confirm        bool          `env:"CONFIRM" envDefault:"true"`
	Prefetch       int           `env:"PREFETCH" envDefault:"64"`
	Concurrency    int           `env:"CONCURRENCY" envDefault:"8"`
	MaxRetries     int           `env:"MAX_RETRIES" envDefault:"3"`
	RetryDelay     time.Duration `env:"RETRY_DELAY" envDefault:"1s"`
}
