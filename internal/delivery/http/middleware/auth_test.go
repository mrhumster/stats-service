package middleware_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mrhumster/stats-service/internal/delivery/http/middleware"
	"github.com/mrhumster/stats-service/internal/service"
	"github.com/mrhumster/identity-service/pkg/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tokenServiceFor(t *testing.T, key *rsa.PublicKey) *service.TokenService {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pkix, err := x509.MarshalPKIXPublicKey(key)
		require.NoError(t, err)
		_, _ = w.Write(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix}))
	}))
	t.Cleanup(srv.Close)

	ts, err := service.NewTokenService(srv.URL)
	require.NoError(t, err)
	return ts
}

func signTokenWithRole(t *testing.T, key *rsa.PrivateKey, userID string, role string, emailVerified bool) string {
	t.Helper()
	claims := &dto.AccessClaims{
		UserID:        userID,
		Role:          role,
		EmailVerified: emailVerified,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	require.NoError(t, err)
	return signed
}

func TestAuthMiddleware(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	ts := tokenServiceFor(t, &key.PublicKey)
	userID := uuid.New()

	router := gin.New()
	router.GET("/protected", middleware.AuthMiddleware(ts), func(c *gin.Context) {
		actor := middleware.Actor(c)
		c.JSON(http.StatusOK, gin.H{
			"user_id":        actor.UserID.String(),
			"role":           actor.Role,
			"email_verified": actor.EmailVerified,
		})
	})

	t.Run("missing token -> 401", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("bad token -> 401", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer garbage")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("valid token -> 200 with claims", func(t *testing.T) {
		token := signTokenWithRole(t, key, userID.String(), "member", true)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"user_id":"`+userID.String()+`","role":"member","email_verified":true}`, w.Body.String())
	})
}

func TestOptionalAuthMiddleware(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	ts := tokenServiceFor(t, &key.PublicKey)
	userID := uuid.New()

	router := gin.New()
	router.GET("/public", middleware.OptionalAuthMiddleware(ts), func(c *gin.Context) {
		if middleware.Claims(c) == nil {
			c.JSON(http.StatusOK, gin.H{"authed": false})
			return
		}
		actor := middleware.Actor(c)
		c.JSON(http.StatusOK, gin.H{"authed": true, "user_id": actor.UserID.String()})
	})

	t.Run("no token -> anonymous", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/public", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"authed":false}`, w.Body.String())
	})

	t.Run("bad token -> still anonymous", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/public", nil)
		req.Header.Set("Authorization", "Bearer garbage")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"authed":false}`, w.Body.String())
	})

	t.Run("valid token -> authed", func(t *testing.T) {
		token := signTokenWithRole(t, key, userID.String(), "member", true)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/public", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"authed":true,"user_id":"`+userID.String()+`"}`, w.Body.String())
	})
}