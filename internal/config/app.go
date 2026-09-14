package config

// App identifies the running process.
type App struct {
	Name    string `env:"NAME" envDefault:"identity-gateway"`
	Env     string `env:"ENV" envDefault:"development"`
	Version string `env:"VERSION" envDefault:"dev"`
	// Debug echoes internal error causes (and panic stacks) into error
	// responses. Development only.
	Debug bool `env:"DEBUG" envDefault:"false"`
}

// IsProduction reports whether APP_ENV is "production".
func (a App) IsProduction() bool { return a.Env == "production" }

// Producer is the value written into every queue envelope.
func (a App) Producer() string { return a.Name + "@" + a.Version }
