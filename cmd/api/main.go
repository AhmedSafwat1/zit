// Command api is the HTTP gateway. It never opens Postgres on the request
// path; database-backed reads are enabled only when DB_DSN is set.
//
//	@title						Identity Gateway API
//	@version					1.0
//	@description				Authentication gateway in front of Zitadel.
//	@BasePath					/api/v1
//
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Type "Bearer " followed by the access token.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	_ "github.com/4jawaly/identity-gateway/docs"
	"github.com/4jawaly/identity-gateway/internal/app"
	"github.com/4jawaly/identity-gateway/internal/bootstrap"
	"github.com/4jawaly/identity-gateway/internal/config"
	"github.com/4jawaly/identity-gateway/internal/router"
	"github.com/4jawaly/identity-gateway/internal/server"
	"github.com/4jawaly/identity-gateway/pkg/circuitbreaker"
	"github.com/4jawaly/identity-gateway/pkg/httpclient"
	"github.com/4jawaly/identity-gateway/pkg/logger"
)

func main() {
	checkConfig := flag.Bool("check-config", false, "print the redacted config and exit")
	flag.Parse()

	cfg := config.MustLoad()
	if *checkConfig {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(cfg.Redacted())
		return
	}
	log := logger.New(cfg.Log).With().Str("app", cfg.App.Name).Str("bin", "api").Logger()
	if err := run(cfg, log); err != nil {
		log.Error().Err(err).Msg("exiting")
		os.Exit(1)
	}
}

func run(cfg *config.Config, log zerolog.Logger) error {
	log.Info().Interface("config", cfg.Redacted()).Msg("starting")

	zitHTTP, err := circuitbreaker.New(
		circuitbreaker.WithHTTPClient(httpclient.New(
			httpclient.WithTimeout(cfg.Zitadel.Timeout),
			httpclient.WithMaxIdleConnsPerHost(256),
		)),
		circuitbreaker.WithSettings(cfg.CircuitBreaker.Zitadel()),
		circuitbreaker.WithLogger(log),
	)
	if err != nil {
		return fmt.Errorf("zitadel http client: %w", err)
	}

	k, err := app.New(
		app.WithConfig(cfg), app.WithLogger(log),
		app.WithRedis(), app.WithPublisher(), app.WithZitadel(zitHTTP),
		// deliberately no WithDatabase: the API never touches Postgres
	)
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := k.Close(ctx); err != nil {
			log.Error().Err(err).Msg("close")
		}
	}()

	r := router.New(k)
	api := router.APIGroup(r, k)
	for _, m := range bootstrap.APIModules(k) {
		m.RegisterRoutes(api)
		log.Info().Str("module", m.Name()).Msg("routes registered")
	}

	if err := server.Run(context.Background(), r, cfg.Server, log); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	log.Info().Msg("stopped")
	return nil
}
