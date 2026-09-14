package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/4jawaly/identity-gateway/internal/response"
)

// RateLimit is a per-client-IP token bucket. Idle buckets are evicted so
// memory stays bounded. rps <= 0 disables it.
func RateLimit(rps float64, burst int) gin.HandlerFunc {
	if rps <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	if burst <= 0 {
		burst = int(rps)
		if burst < 1 {
			burst = 1
		}
	}
	type entry struct {
		lim  *rate.Limiter
		seen time.Time
	}
	var (
		mu      sync.Mutex
		buckets = map[string]*entry{}
		lastGC  = time.Now()
	)
	const idle = 3 * time.Minute
	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()
		mu.Lock()
		e, ok := buckets[ip]
		if !ok {
			e = &entry{lim: rate.NewLimiter(rate.Limit(rps), burst)}
			buckets[ip] = e
		}
		e.seen = now
		if now.Sub(lastGC) > idle {
			for k, v := range buckets {
				if now.Sub(v.seen) > idle {
					delete(buckets, k)
				}
			}
			lastGC = now
		}
		allowed := e.lim.Allow()
		mu.Unlock()
		if !allowed {
			response.Fail(c, response.ErrRateLimited)
			return
		}
		c.Next()
	}
}
