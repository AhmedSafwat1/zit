package middleware

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/4jawaly/identity-gateway/internal/reqctx"
)

// Logger attaches a request-scoped logger to the context and writes one
// access log line per request. Successful requests are sampled 1-in-N;
// every 4xx/5xx is logged.
func Logger(log zerolog.Logger, sampleRate uint32) gin.HandlerFunc {
	var counter atomic.Uint32
	if sampleRate == 0 {
		sampleRate = 1
	}
	return func(c *gin.Context) {
		start := time.Now()
		reqLog := log.With().
			Str("request_id", reqctx.RequestIDFrom(c.Request.Context())).
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Logger()
		c.Request = c.Request.WithContext(reqLog.WithContext(c.Request.Context()))

		c.Next()

		status := c.Writer.Status()
		if status < http.StatusBadRequest && counter.Add(1)%sampleRate != 0 {
			return
		}
		ev := reqLog.Info()
		switch {
		case status >= http.StatusInternalServerError:
			ev = reqLog.Error()
		case status >= http.StatusBadRequest:
			ev = reqLog.Warn()
		}
		if len(c.Errors) > 0 {
			ev = ev.Str("errors", c.Errors.String())
		}
		ev.Int("status", status).
			Dur("duration", time.Since(start)).
			Int("bytes", c.Writer.Size()).
			Str("ip", c.ClientIP()).
			Msg("request")
	}
}
