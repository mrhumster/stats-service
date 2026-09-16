package handler

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
	svcmock "github.com/mrhumster/stats-service/internal/service/mock"
	"github.com/mrhumster/identity-service/pkg/dto"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestRegisterViewAuthedUser mirrors auth_test.go's token helpers to prove a
// signed-in viewer is deduped by user id instead of the client IP.
func TestRegisterViewAuthedUser(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	ts := tokenServiceFromKey(t, &key.PublicKey)
	userID := uuid.New()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	svc := svcmock.NewMockStatsService(ctrl)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewStatsHandler(svc)
	r.POST("/streams/:streamId/views", middleware.OptionalAuthMiddleware(ts), h.RegisterView)

	streamID := uuid.New()
	svc.EXPECT().RegisterView(gomock.Any(), streamID, "u:"+userID.String()).Return(nil)

	claims := &dto.AccessClaims{
		UserID: userID.String(),
		Role:   "member",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/streams/"+streamID.String()+"/views", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"ok":true`)
}

func tokenServiceFromKey(t *testing.T, pub *rsa.PublicKey) *service.TokenService {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pkix, err := x509.MarshalPKIXPublicKey(pub)
		require.NoError(t, err)
		_, _ = w.Write(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix}))
	}))
	t.Cleanup(srv.Close)

	ts, err := service.NewTokenService(srv.URL)
	require.NoError(t, err)
	return ts
}