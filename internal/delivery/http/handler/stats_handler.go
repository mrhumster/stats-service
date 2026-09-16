package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mrhumster/stats-service/internal/delivery/http/middleware"
	"github.com/mrhumster/stats-service/internal/domain/models"
	"github.com/mrhumster/stats-service/internal/metrics"
	"github.com/mrhumster/stats-service/internal/service"
)

type StatsHandler struct {
	svc service.StatsService
}

func NewStatsHandler(svc service.StatsService) *StatsHandler {
	return &StatsHandler{svc: svc}
}

type reactionRequest struct {
	Kind string `json:"kind"`
}

// GetStats returns the public counters of a stream. Public endpoint; when a
// valid Bearer token is present (OptionalAuthMiddleware) the caller's own
// reaction is included.
func (h *StatsHandler) GetStats(c *gin.Context) {
	streamID, ok := parseUUID(c, c.Param("streamId"))
	if !ok {
		return
	}

	var actor *service.Actor
	if middleware.Claims(c) != nil {
		a := middleware.Actor(c)
		actor = &a
	}

	stats, err := h.svc.GetStats(c.Request.Context(), streamID, actor)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// SetReaction upserts the caller's like/dislike ("none" removes it) and
// returns the updated counters. Requires a verified email (admin bypass),
// mirrors the CreateStream gate.
func (h *StatsHandler) SetReaction(c *gin.Context) {
	streamID, ok := parseUUID(c, c.Param("streamId"))
	if !ok {
		return
	}

	var req reactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	kind := models.ReactionKind(strings.ToLower(strings.TrimSpace(req.Kind)))
	if !kind.Valid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reaction kind"})
		return
	}

	stats, err := h.svc.SetReaction(c.Request.Context(), middleware.Actor(c), streamID, kind)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	metrics.Reaction(string(kind))
	c.JSON(http.StatusOK, stats)
}

// RegisterView bumps the per-stream view counter once per viewer within the
// dedup window. Public endpoint (stream gate enforced in the service layer);
// an OptionalAuthMiddleware identifies the viewer: signed-in users dedup by
// account, guests by client IP.
func (h *StatsHandler) RegisterView(c *gin.Context) {
	streamID, ok := parseUUID(c, c.Param("streamId"))
	if !ok {
		return
	}

	viewerKey := guestViewerKey(c)
	if middleware.Claims(c) != nil {
		viewerKey = "u:" + middleware.Actor(c).UserID.String()
	}

	if err := h.svc.RegisterView(c.Request.Context(), streamID, viewerKey); err != nil {
		writeServiceError(c, err)
		return
	}

	metrics.View("success")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// guestViewerKey dedupes anonymous viewers by their client IP. Behind traefik
// gin resolves the real peer from X-Forwarded-For.
func guestViewerKey(c *gin.Context) string {
	return "ip:" + c.ClientIP()
}

func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrEmailNotVerified):
		c.JSON(http.StatusForbidden, gin.H{"error": "email not verified"})
	case errors.Is(err, service.ErrInvalidReactionKind):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reaction kind"})
	case errors.Is(err, service.ErrStreamNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "stream not found"})
	case errors.Is(err, service.ErrStreamNotPublished):
		c.JSON(http.StatusForbidden, gin.H{"error": "stream is not published"})
	case errors.Is(err, service.ErrStreamUnavailable):
		slog.Error("stream service unavailable", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "internal server error"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

func parseUUID(c *gin.Context, raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid stream id"})
		return uuid.Nil, false
	}
	return id, true
}