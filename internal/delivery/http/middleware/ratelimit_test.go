package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRateLimitPerMin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("allows up to limit then rejects", func(t *testing.T) {
		r := gin.New()
		r.POST("/views", RateLimitPerMin(3), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		for i := 0; i < 3; i++ {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/views", nil)
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "request %d should pass", i+1)
		}

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/views", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("different clients have independent budgets", func(t *testing.T) {
		r := gin.New()
		r.POST("/views", RateLimitPerMin(1), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		for _, ip := range []string{"1.1.1.1:1234", "2.2.2.2:5678"} {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/views", nil)
			req.RemoteAddr = ip
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("rejects the first client after its budget only", func(t *testing.T) {
		r := gin.New()
		r.POST("/views", RateLimitPerMin(1), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/views", nil)
		req.RemoteAddr = "1.1.1.1:1234"
		r.ServeHTTP(w, req)
		w = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/views", nil)
		req.RemoteAddr = "1.1.1.1:1234"
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusTooManyRequests, w.Code)

		w = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/views", nil)
		req.RemoteAddr = "2.2.2.2:5678"
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	})
}