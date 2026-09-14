// Package user is the user module: token introspection on the API side and
// the user.sync consumer on the worker side.
package user

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/4jawaly/identity-gateway/internal/config"
	"github.com/4jawaly/identity-gateway/internal/modules/user/handlers"
	"github.com/4jawaly/identity-gateway/internal/modules/user/jobs/sync_user"
	"github.com/4jawaly/identity-gateway/internal/modules/user/repositories"
	"github.com/4jawaly/identity-gateway/internal/modules/user/routes"
	"github.com/4jawaly/identity-gateway/internal/modules/user/services"
	"github.com/4jawaly/identity-gateway/internal/modules/user/usercache"
	"github.com/4jawaly/identity-gateway/pkg/cache"
	"github.com/4jawaly/identity-gateway/pkg/queue"
	"github.com/4jawaly/identity-gateway/pkg/zitadel"
)

// Deps is everything the module may use. DB, Pub and Zitadel may be nil;
// the module builds only the half those allow.
type Deps struct {
	DB      *gorm.DB
	Store   cache.Store
	Pub     queue.Publisher
	Zitadel *zitadel.Client
	Log     zerolog.Logger
	Cfg     *config.Config
}

// Module implements app.Module.
type Module struct {
	userSvc  *services.UserService
	syncSvc  *services.SyncService
	handler  *handlers.UserHandler
	consumer *sync_user.Consumer
	withDB   bool
}

// New wires the module from explicit dependencies.
func New(d Deps) *Module {
	m := &Module{withDB: d.DB != nil}
	log := d.Log.With().Str("module", "user").Logger()
	var repo *repositories.UserRepository
	if d.DB != nil {
		repo = repositories.NewUserRepository(d.DB)
	}

	if d.Zitadel != nil && d.Store != nil && d.Pub != nil {
		uc := usercache.New(d.Store)
		notifier := sync_user.NewNotifier(d.Pub, uc,
			sync_user.WithProducer(d.Cfg.App.Producer()),
			sync_user.WithDedupeTTL(d.Cfg.Cache.SyncDedupeTTL),
			sync_user.WithNotifierLogger(log),
		)
		opts := []services.ServiceOption{
			services.WithIssuer(d.Cfg.Zitadel.Issuer),
			services.WithTTLs(d.Cfg.Cache.IntrospectTTL, d.Cfg.Cache.InactiveTTL, d.Cfg.Cache.UserTTL),
			services.WithLogger(log),
		}
		if repo != nil {
			opts = append(opts, services.WithRepository(repo))
		}
		m.userSvc = services.NewUserService(d.Zitadel, uc, notifier, opts...)
		m.handler = handlers.NewUserHandler(m.userSvc)
	}
	if repo != nil {
		m.syncSvc = services.NewSyncService(repo, services.WithSyncLogger(log))
		m.consumer = sync_user.NewConsumer(m.syncSvc)
	}
	return m
}

// Name implements app.Module.
func (m *Module) Name() string { return "user" }

// RegisterRoutes mounts the HTTP routes; a no-op without the API half.
func (m *Module) RegisterRoutes(g *gin.RouterGroup) {
	if m.handler == nil {
		return
	}
	routes.Register(g, m.handler, m.userSvc, m.withDB)
}

// RegisterConsumers binds the user.sync consumer; a no-op without a DB.
func (m *Module) RegisterConsumers(c *queue.Consumer) {
	if m.consumer == nil {
		return
	}
	m.consumer.Register(c)
}
