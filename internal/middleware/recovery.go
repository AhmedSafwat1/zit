package middleware

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/4jawaly/identity-gateway/internal/response"
)

// Recovery turns a panic into an INTERNAL_ERROR envelope. The stack is logged
// by response.FailPanic and echoed to the client only in debug mode.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			// A broken pipe means the client is gone; nothing to write.
			if ne, ok := r.(error); ok {
				var opErr *net.OpError
				if errors.As(ne, &opErr) {
					var se *os.SyscallError
					if errors.As(opErr, &se) && (strings.Contains(strings.ToLower(se.Error()), "broken pipe") ||
						strings.Contains(strings.ToLower(se.Error()), "connection reset by peer")) {
						c.Abort()
						return
					}
				}
			}
			if c.Writer.Written() {
				c.Abort()
				return
			}
			response.FailPanic(c, response.ErrInternal.With(fmt.Errorf("panic: %v", r), nil), string(debug.Stack()))
		}()
		c.Next()
	}
}
