// Package bootstrap assembles feature modules from the container. It exists
// so app does not import modules (which import app types), breaking the cycle.
package bootstrap

import (
	"github.com/4jawaly/identity-gateway/internal/app"
	"github.com/4jawaly/identity-gateway/internal/modules/user"
)

// BuildModules returns every module wired against k. A module builds only
// the half its dependencies allow (routes need Zitadel/Cache/Pub, consumers need DB).
func BuildModules(k *app.Container) []app.Module {
	return []app.Module{
		user.New(user.Deps{
			DB: k.DB, Store: k.Store, Pub: k.Pub, Zitadel: k.Zitadel, Log: k.Log, Cfg: k.Cfg,
		}),
	}
}
