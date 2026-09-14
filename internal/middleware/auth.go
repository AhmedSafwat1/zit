package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/4jawaly/identity-gateway/internal/reqctx"
	"github.com/4jawaly/identity-gateway/internal/response"
	"github.com/4jawaly/identity-gateway/internal/types"
)

// Authenticator is what Auth needs from the user module: it maps a bearer
// token to the calling user, returning a catalog AppError on failure.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (types.AuthUser, error)
}

// BearerToken extracts the token from an Authorization header. It returns
// "" when the header is missing or not a Bearer scheme.
func BearerToken(header string) string {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// Auth rejects requests without a valid bearer token and stores the user.
func Auth(a Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := BearerToken(c.GetHeader("Authorization"))
		if token == "" {
			response.Fail(c, response.ErrTokenMissing)
			return
		}
		u, err := a.Authenticate(c.Request.Context(), token)
		if err != nil {
			response.Fail(c, err)
			return
		}
		c.Request = c.Request.WithContext(reqctx.WithAuthUser(c.Request.Context(), u))
		c.Next()
	}
}
