package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mrhumster/stats-service/internal/service"
	"github.com/mrhumster/identity-service/pkg/dto"
)

const KeyUserID = "user_id"

// AuthMiddleware authenticates Bearer access tokens on the write API and
// places the user ID and claims into the gin context.
func AuthMiddleware(tokens *service.TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(raw, "Bearer ")
		if !ok || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "auth token required"})
			return
		}

		claims, err := tokens.ValidateAccessToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			return
		}

		c.Set(KeyUserID, userID)
		c.Set("claims", claims)
		c.Next()
	}
}

// OptionalAuthMiddleware authenticates the Bearer token when present, but
// treats requests without (or with a broken) token as anonymous. Used on the
// public stats endpoint to attach my_reaction for signed-in viewers.
func OptionalAuthMiddleware(tokens *service.TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(raw, "Bearer ")
		if !ok || token == "" {
			c.Next()
			return
		}

		claims, err := tokens.ValidateAccessToken(token)
		if err != nil {
			c.Next()
			return
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			c.Next()
			return
		}

		c.Set(KeyUserID, userID)
		c.Set("claims", claims)
		c.Next()
	}
}

func UserID(c *gin.Context) uuid.UUID {
	v, _ := c.Get(KeyUserID)
	id, _ := v.(uuid.UUID)
	return id
}

func Claims(c *gin.Context) *dto.AccessClaims {
	v, _ := c.Get("claims")
	cl, _ := v.(*dto.AccessClaims)
	return cl
}

// Actor builds the service.Actor from JWT claims for write guards
// (verified email + role + user id), mirroring the stream CreateStream gate.
func Actor(c *gin.Context) service.Actor {
	cl := Claims(c)
	if cl == nil {
		return service.Actor{}
	}
	userID, _ := uuid.Parse(cl.UserID)
	return service.Actor{
		UserID:        userID,
		Role:          cl.Role,
		Email:         cl.Email,
		EmailVerified: cl.EmailVerified,
	}
}