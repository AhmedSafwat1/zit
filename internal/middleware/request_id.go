// Package middleware holds the gin middleware chain. Nothing here knows
// about modules; auth delegates to an Authenticator declared in this package.
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/4jawaly/identity-gateway/internal/reqctx"
)

// HeaderRequestID is the inbound/outbound request id header.
const HeaderRequestID = "X-Request-ID"

const maxRequestIDLen = 128

// RequestID reuses an inbound X-Request-ID (bounded length) or mints a ULID.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if id == "" || len(id) > maxRequestIDLen {
			id = ulid.Make().String()
		}
		c.Header(HeaderRequestID, id)
		c.Request = c.Request.WithContext(reqctx.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}
