package app

import (
	"github.com/gin-gonic/gin"

	"github.com/4jawaly/identity-gateway/pkg/queue"
)

// Module is a self-contained feature: it registers HTTP routes in the API
// binary and queue consumers in the worker binary. Either may be a no-op.
type Module interface {
	Name() string
	RegisterRoutes(g *gin.RouterGroup)
	RegisterConsumers(c *queue.Consumer)
}
