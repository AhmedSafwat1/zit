// Command worker consumes RabbitMQ and writes Postgres. It never talks to
// Zitadel and never publishes.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/4jawaly/identity-gateway/internal/app"
	"github.com/4jawaly/identity-gateway/internal/bootstrap"
	"github.com/4jawaly/identity-gateway/internal/config"
	"github.com/4jawaly/identity-gateway/internal/router"
	"github.com/4jawaly/identity-gateway/internal/server"
	"github.com/4jawaly/identity-gateway/pkg/logger"
	"github.com/4jawaly/identity-gateway/pkg/queue"
)

func main() {
	cfg := config.MustLoad()
	log := logger.New(cfg.Log).With().Str("app", cfg.App.Name).Str("bin", "worker").Logger()
	if err := run(cfg, log); err != nil {
		log.Error().Err(err).Msg("exiting")
		os.Exit(1)
	}
}

func run(cfg *config.Config, log zerolog.Logger) error {
	log.Info().Interface("config", cfg.Redacted()).Msg("starting")
	if cfg.Queue.URL == "" {
		return errors.New("QUEUE_URL is empty")
	}

	k, err := app.New(
		app.WithConfig(cfg), app.WithLogger(log),
		app.WithDatabase(), app.WithRedis(),
		// deliberately no publisher and no Zitadel
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

	consumer := queue.NewConsumer(cfg.Queue.URL, cfg.Queue.Exchange,
		queue.WithPrefetch(cfg.Queue.Prefetch),
		queue.WithConcurrency(cfg.Queue.Concurrency),
		queue.WithConsumerLogger(log.With().Str("component", "consumer").Logger()),
		queue.WithMiddleware(
			queue.Retry(queue.DefaultRetryPolicy(cfg.Queue.MaxRetries, cfg.Queue.RetryDelay)),
		),
	)
	for _, m := range bootstrap.BuildModules(k) {
		m.RegisterConsumers(consumer)
		log.Info().Str("module", m.Name()).Msg("consumers registered")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return consumer.Run(gctx) })
	g.Go(func() error { return server.Run(gctx, router.New(k), cfg.Server.ForWorker(), log) })
	if err := g.Wait(); err != nil {
		return fmt.Errorf("worker: %w", err)
	}
	log.Info().Msg("stopped")
	return nil
}
