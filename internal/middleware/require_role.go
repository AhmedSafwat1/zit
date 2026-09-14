package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/4jawaly/identity-gateway/internal/reqctx"
	"github.com/4jawaly/identity-gateway/internal/response"
)

// RequireRole allows the request through when the user holds any of roles.
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := reqctx.AuthUserFrom(c.Request.Context())
		if !ok {
			response.Fail(c, response.ErrTokenMissing)
			return
		}
		for _, r := range roles {
			if u.HasRole(r) {
				c.Next()
				return
			}
		}
		response.Fail(c, response.ErrForbidden)
	}
}
