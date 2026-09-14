package app

import (
	"github.com/gin-gonic/gin"

	"github.com/4jawaly/identity-gateway/pkg/queue"
)

// HTTPModule is a feature's API half: it mounts routes in cmd/api.
type HTTPModule interface {
	Name() string
	RegisterRoutes(g *gin.RouterGroup)
}

// ConsumerModule is a feature's worker half: it binds queue handlers in cmd/worker.
type ConsumerModule interface {
	Name() string
	RegisterConsumers(c *queue.Consumer)
}
