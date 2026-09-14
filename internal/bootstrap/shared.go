package bootstrap

import (
	"github.com/4jawaly/identity-gateway/internal/app"
	"github.com/4jawaly/identity-gateway/internal/middleware"
	usersync "github.com/4jawaly/identity-gateway/internal/modules/user/jobs/sync_user"
	userrepo "github.com/4jawaly/identity-gateway/internal/modules/user/repositories"
	usersvc "github.com/4jawaly/identity-gateway/internal/modules/user/services"
	"github.com/4jawaly/identity-gateway/internal/modules/user/usercache"
)

// APIShared holds the instances HTTP modules share in cmd/api. Each is built
// exactly once. Consumers of these take the interface accessors, never the
// concrete fields: an accessor returns a true nil interface when the
// instance is absent, so a module's `dep == nil` check is honest.
type APIShared struct {
	UserCache *usercache.Cache
	UserSvc   *usersvc.UserService
}

// Authenticator exposes token authentication to other modules' routes.
func (s *APIShared) Authenticator() middleware.Authenticator { return s.UserSvc }

// newAPIShared builds the API half from the container (Store, Pub, Zitadel
// required). The API never opens Postgres.
func newAPIShared(k *app.Container) *APIShared {
	s := &APIShared{}
	log := k.Log.With().Str("module", "user").Logger()

	s.UserCache = usercache.New(k.Store)
	notifier := usersync.NewNotifier(k.Pub, s.UserCache,
		usersync.WithProducer(k.Cfg.App.Producer()),
		usersync.WithDedupeTTL(k.Cfg.Cache.SyncDedupeTTL),
		usersync.WithNotifierLogger(log),
	)
	s.UserSvc = usersvc.NewUserService(k.Zitadel, s.UserCache, notifier,
		usersvc.WithIssuer(k.Cfg.Zitadel.Issuer),
		usersvc.WithTTLs(k.Cfg.Cache.IntrospectTTL, k.Cfg.Cache.InactiveTTL),
		usersvc.WithLogger(log),
	)
	return s
}

// ConsumerShared holds the instances consumer modules share in cmd/worker.
type ConsumerShared struct {
	UserRepo *userrepo.UserRepository
	SyncSvc  *usersvc.SyncService
}

// UserRepository exposes the user repository to other consumer modules.
func (s *ConsumerShared) UserRepository() usersvc.UserRepository { return s.UserRepo }

// newConsumerShared builds the worker half from the container (DB required).
func newConsumerShared(k *app.Container) *ConsumerShared {
	log := k.Log.With().Str("module", "user").Logger()
	repo := userrepo.NewUserRepository(k.DB)
	return &ConsumerShared{
		UserRepo: repo,
		SyncSvc:  usersvc.NewSyncService(repo, usersvc.WithSyncLogger(log)),
	}
}
