// Package router builds the gin engine with the shared middleware chain and
// the operational endpoints. Feature routes are registered by modules.
package router

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginswagger "github.com/swaggo/gin-swagger"

	"github.com/4jawaly/identity-gateway/internal/app"
	"github.com/4jawaly/identity-gateway/internal/middleware"
	"github.com/4jawaly/identity-gateway/internal/response"
)

// New builds the engine. Chain: request_id -> logger -> recovery -> body limit -> timeout.
func New(k *app.Container) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.RemoveExtraSlash = true

	cfg := k.Cfg
	response.SetDebug(cfg.App.Debug)
	r.Use(
		middleware.RequestID(),
		middleware.Logger(k.Log, cfg.Log.SampleRate),
		middleware.Recovery(),
		middleware.BodyLimit(cfg.Server.MaxBodyBytes),
		middleware.Timeout(cfg.Server.WriteTimeout),
	)
	r.NoRoute(func(c *gin.Context) { response.Fail(c, response.ErrNotFound) })
	r.NoMethod(func(c *gin.Context) {
		response.Fail(c, response.ErrNotFound.With(nil, map[string]any{"reason": "method_not_allowed"}))
	})

	r.GET("/health", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "ok", "app": cfg.App.Name, "version": cfg.App.Version})
	})
	r.GET("/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := k.Ready(ctx); err != nil {
			response.Fail(c, response.ErrZitadelUnavailable.With(err, map[string]any{"reason": "dependency_unavailable"}))
			return
		}
		response.OK(c, gin.H{"status": "ready"})
	})

	if !cfg.App.IsProduction() {
		r.GET("/swagger/*any", ginswagger.WrapHandler(swaggerfiles.Handler))
	}
	return r
}

// APIGroup returns the versioned group with the optional rate limiter applied.
func APIGroup(r *gin.Engine, k *app.Container) *gin.RouterGroup {
	g := r.Group("/api/v1")
	g.Use(middleware.RateLimit(k.Cfg.Server.RateLimitRPS, k.Cfg.Server.RateLimitBurst))
	return g
}

// Handler adapts the engine for tests that want a plain http.Handler.
func Handler(r *gin.Engine) http.Handler { return r }
