// Package routes maps URLs to handlers for the user module.
package routes

import (
	"github.com/gin-gonic/gin"

	"github.com/4jawaly/identity-gateway/internal/middleware"
	"github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/modules/user/handlers"
)

// Register mounts the user routes. withDB controls the Postgres-backed reads.
func Register(g *gin.RouterGroup, h *handlers.UserHandler, authn middleware.Authenticator, withDB bool) {
	users := g.Group("/users")
	users.POST("/introspect", h.Introspect)
	if !withDB {
		return
	}
	authed := users.Group("", middleware.Auth(authn))
	authed.GET("/me", h.Me)
	authed.GET("", middleware.RequireRole(dto.AdminRole), h.List)
	authed.GET("/:sub", middleware.RequireRole(dto.AdminRole), h.Get)
	authed.GET("/:sub/sub-accounts", h.SubAccounts)
}
