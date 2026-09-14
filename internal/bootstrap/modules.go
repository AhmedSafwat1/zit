// Package bootstrap is the composition root: it builds every dependency
// once (shared.go) and wires modules in dependency order (one file per
// module). Each binary asks only for its own half, so the API never builds
// consumers and the worker never builds HTTP services. It is the only
// package that imports module internals (services, repositories, usercache,
// jobs); app stays free of modules, which breaks the import cycle.
package bootstrap

import (
	"github.com/4jawaly/identity-gateway/internal/app"
)

// APIModules returns the HTTP modules for cmd/api, in dependency order.
// The container must hold Store, Pub and Zitadel. The API never opens Postgres.
//
// Adding a module: build its services in newAPIShared if other modules need
// them, take cross-module dependencies through the APIShared accessors, e.g.
//
//	billing := billing.NewAPI(billingservices.New(...), s.Authenticator())
//
// then append it here.
func APIModules(k *app.Container) []app.HTTPModule {
	s := newAPIShared(k)
	return []app.HTTPModule{
		userAPI(s),
	}
}

// WorkerModules returns the consumer modules for cmd/worker, in dependency
// order. The container must hold DB.
func WorkerModules(k *app.Container) []app.ConsumerModule {
	s := newConsumerShared(k)
	return []app.ConsumerModule{
		userConsumer(s),
	}
}
