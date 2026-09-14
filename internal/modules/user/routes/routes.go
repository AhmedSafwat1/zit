// Package routes maps URLs to handlers for the user module.
package routes

import (
	"github.com/gin-gonic/gin"

	userhandler "github.com/4jawaly/identity-gateway/internal/modules/user/handlers"
)

// Register mounts the user routes.
func Register(g *gin.RouterGroup, h *userhandler.UserHandler) {
	g.POST("/users/introspect", h.Introspect)
}
