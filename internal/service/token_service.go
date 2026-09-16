package service

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mrhumster/identity-service/pkg/dto"
)

// TokenService validates access tokens on the write API using the
// identity-service public key (fetched once at startup), mirroring the
// comments/events token validation pattern. Read endpoints (stats list,
// views) are intentionally public.
type TokenService struct {
	accessPublicKey *rsa.PublicKey
}

func NewTokenService(publicKeyURL string) (*TokenService, error) {
	if publicKeyURL == "" {
		return nil, errors.New("token service: JWT_ACCESS_PUBLIC_KEY_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicKeyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("token service: build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token service: fetch public key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		suffix := " (status " + resp.Status + ")"
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if len(body) > 0 {
			suffix = ": " + string(body)
		}
		return nil, fmt.Errorf("token service: fetch public key failed%s", suffix)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("token service: read public key: %w", err)
	}

	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(body)
	if err != nil {
		return nil, fmt.Errorf("token service: parse public key: %w", err)
	}

	return &TokenService{accessPublicKey: publicKey}, nil
}

func (s *TokenService) ValidateAccessToken(tokenString string) (*dto.AccessClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &dto.AccessClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.accessPublicKey, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*dto.AccessClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid access token")
}