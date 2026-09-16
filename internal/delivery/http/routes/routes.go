package routes

import (
	"log"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/mrhumster/stats-service/config"
	"github.com/mrhumster/stats-service/internal/delivery/http/handler"
	"github.com/mrhumster/stats-service/internal/delivery/http/middleware"
	"github.com/mrhumster/stats-service/internal/service"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
)

// SetupRoutes builds the REST API:
//   - public: stream stats (stats list + per-view counter with a stream gate)
//   - authenticated write: reactions (like/dislike/none) with a
//     verified-email gate (admin bypass), mirrors the CreateStream rule.
//   - health and metrics
func SetupRoutes(db *gorm.DB, cfg *config.Config, svc service.StatsService, tokens *service.TokenService) *gin.Engine {
	if cfg.Server.Mode == "test" {
		gin.SetMode(gin.TestMode)
	} else if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.MetricsMiddleware())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.Server.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	h := handler.NewStatsHandler(svc)

	// Public read/views. my_reaction is attached when a valid Bearer token
	// is supplied (OptionalAuthMiddleware); the same middleware picks the
	// viewer identity (user id vs IP) for the per-viewer dedup on views.
	r.GET("/streams/:streamId/stats", middleware.OptionalAuthMiddleware(tokens), h.GetStats)
	r.POST("/streams/:streamId/views", middleware.OptionalAuthMiddleware(tokens), h.RegisterView)

	authed := r.Group("", middleware.AuthMiddleware(tokens))
	{
		authed.PUT("/streams/:streamId/reaction", h.SetReaction)
	}

	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	r.GET("/health", func(c *gin.Context) {
		if sqlDB, err := db.DB(); err == nil {
			if err := sqlDB.Ping(); err != nil {
				log.Println("stats PG error: ", err.Error())
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "down", "error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "up"})
	})

	return r
}