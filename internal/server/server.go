// Package server runs the HTTP listener with graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/4jawaly/identity-gateway/internal/config"
)

// Run serves h until SIGINT/SIGTERM, then drains within cfg.ShutdownTimeout.
// pprof is exposed on the internal PprofPort (0 disables it).
func Run(ctx context.Context, h http.Handler, cfg config.Server, log zerolog.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           h,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    1 << 16,
	}

	errCh := make(chan error, 2)
	go func() {
		log.Info().Str("addr", srv.Addr).Msg("http server listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	var internal *http.Server
	if cfg.PprofPort > 0 {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		internal = &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", cfg.PprofPort), Handler: mux, ReadHeaderTimeout: cfg.ReadTimeout}
		go func() {
			log.Info().Str("addr", internal.Addr).Msg("pprof listening")
			if err := internal.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error().Err(err).Msg("pprof server failed")
			}
		}()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Info().Dur("timeout", cfg.ShutdownTimeout).Msg("shutdown signal received, draining")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	var errs []error
	if err := srv.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("drain http: %w", err))
	}
	if internal != nil {
		if err := internal.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("drain pprof: %w", err))
		}
	}
	return errors.Join(errs...)
}
