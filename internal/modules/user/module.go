// Package user is the user module: token introspection on the API side and
// the user.sync consumer on the worker side. Each half is a separate type
// built only by the binary that needs it; services are constructed by
// internal/bootstrap (the composition root) and handed in.
package user

import (
	"github.com/gin-gonic/gin"

	userhandler "github.com/4jawaly/identity-gateway/internal/modules/user/handlers"
	usersync "github.com/4jawaly/identity-gateway/internal/modules/user/jobs/sync_user"
	userroute "github.com/4jawaly/identity-gateway/internal/modules/user/routes"
	usersvc "github.com/4jawaly/identity-gateway/internal/modules/user/services"
	"github.com/4jawaly/identity-gateway/pkg/queue"
)

// Name is the module name in both binaries.
const Name = "user"

// APIModule implements app.HTTPModule.
type APIModule struct {
	handler *userhandler.UserHandler
}

// NewAPI wires the HTTP half.
func NewAPI(svc *usersvc.UserService) *APIModule {
	return &APIModule{handler: userhandler.NewUserHandler(svc)}
}

// Name implements app.HTTPModule.
func (m *APIModule) Name() string { return Name }

// RegisterRoutes implements app.HTTPModule.
func (m *APIModule) RegisterRoutes(g *gin.RouterGroup) { userroute.Register(g, m.handler) }

// WorkerModule implements app.ConsumerModule.
type WorkerModule struct {
	consumer *usersync.Consumer
}

// NewWorker wires the queue half.
func NewWorker(syncSvc *usersvc.SyncService) *WorkerModule {
	return &WorkerModule{consumer: usersync.NewConsumer(syncSvc)}
}

// Name implements app.ConsumerModule.
func (m *WorkerModule) Name() string { return Name }

// RegisterConsumers implements app.ConsumerModule.
func (m *WorkerModule) RegisterConsumers(c *queue.Consumer) { m.consumer.Register(c) }
