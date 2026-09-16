package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests served by the stats reader.",
	}, []string{"method", "endpoint", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "endpoint", "status"})
)

func init() {
	prometheus.MustRegister(httpRequests, httpDuration)
}

// MetricsMiddleware records request count and latency per
// method / route template / status code (RED-style).
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		httpRequests.WithLabelValues(c.Request.Method, endpoint, status).Inc()
		httpDuration.WithLabelValues(c.Request.Method, endpoint, status).Observe(time.Since(start).Seconds())
	}
}