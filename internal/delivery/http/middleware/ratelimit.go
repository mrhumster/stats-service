package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// rateLimiter is a dependency-free in-memory fixed-window limiter. It is
// coarse by design: per-process, best-effort, enough to stop the trivial
// burst flooding that the 24h view dedup alone cannot cover.
type rateLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	counters map[string]*windowCounter
}

type windowCounter struct {
	count   int
	resetAt time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:    limit,
		window:   window,
		counters: make(map[string]*windowCounter),
	}
}

func (r *rateLimiter) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	wc, ok := r.counters[key]
	if !ok || now.After(wc.resetAt) {
		// Sliding start: each new window resets the counter.
		r.counters[key] = &windowCounter{count: 1, resetAt: now.Add(r.window)}
		// Opportunistic cleanup to keep the map bounded (windows are short).
		if len(r.counters) > 10_000 {
			for k, c := range r.counters {
				if now.After(c.resetAt) {
					delete(r.counters, k)
				}
			}
		}
		return true
	}
	wc.count++
	return wc.count <= r.limit
}

// RateLimitPerMin rejects a viewer key (client IP, or user id when a Bearer
// token is already set by OptionalAuthMiddleware) once it exceeds limit
// requests within a minute. Run it AFTER OptionalAuthMiddleware so authed
// callers get their own budget.
func RateLimitPerMin(limit int) gin.HandlerFunc {
	if limit <= 0 {
		limit = 300
	}
	rl := newRateLimiter(limit, time.Minute)
	return func(c *gin.Context) {
		key := "ip:" + c.ClientIP()
		if id := UserID(c); id != uuid.Nil {
			key = "u:" + id.String()
		}
		if !rl.allow(key) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}
		c.Next()
	}
}