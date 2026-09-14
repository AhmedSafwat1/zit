package config

import "time"

// Zitadel configures the upstream identity provider client.
type Zitadel struct {
	Issuer       string        `env:"ISSUER" envDefault:"https://zauth.4ja.org"`
	UserinfoPath string        `env:"USERINFO_PATH" envDefault:"/oidc/v1/userinfo"`
	Timeout      time.Duration `env:"TIMEOUT" envDefault:"3s"`
	Retries      int           `env:"RETRIES" envDefault:"1"`
}
