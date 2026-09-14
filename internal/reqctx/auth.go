// Package reqctx carries per-request values on context.Context: the request
// id and the authenticated user. It has no HTTP dependency, so services can
// read from it as well as middleware and handlers.
package reqctx

import (
	"context"

	"github.com/4jawaly/identity-gateway/internal/types"
)

type authUserKey struct{}

// WithAuthUser stores the authenticated user in ctx.
func WithAuthUser(ctx context.Context, u types.AuthUser) context.Context {
	return context.WithValue(ctx, authUserKey{}, u)
}

// AuthUserFrom returns the authenticated user stored by the auth middleware.
func AuthUserFrom(ctx context.Context) (types.AuthUser, bool) {
	u, ok := ctx.Value(authUserKey{}).(types.AuthUser)
	return u, ok
}
