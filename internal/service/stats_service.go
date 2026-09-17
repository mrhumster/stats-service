//go:generate mockgen -source=stats_service.go -destination=mock/stats_service_mock.go -package=mock
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/mrhumster/stats-service/internal/domain/models"
)

var (
	ErrEmailNotVerified    = errors.New("email not verified")
	ErrStreamNotFound      = errors.New("stream not found")
	ErrStreamNotPublished  = errors.New("stream is not published")
	ErrStreamUnavailable   = errors.New("stream service unavailable")
	ErrInvalidReactionKind = errors.New("invalid reaction kind")
	ErrViewDedupUnavailable = errors.New("view dedup unavailable")
)

// Actor is the authenticated caller: user identity plus JWT claims used by
// the write guards (verified email, admin bypass).
type Actor struct {
	UserID        uuid.UUID
	Role          string
	Email         string
	EmailVerified bool
}

type StatsService interface {
	// GetStats returns counters and the caller's reaction (when authed).
	GetStats(ctx context.Context, streamID uuid.UUID, actor *Actor) (*models.Stats, error)
	// SetReaction upserts the actor's like/dislike (or removes it with "none")
	// and emits a reaction.* activity event to the stream owner.
	SetReaction(ctx context.Context, actor Actor, streamID uuid.UUID, kind models.ReactionKind) (*models.Stats, error)
	// RegisterView bumps the per-stream view counter once per viewer within
	// the dedup window (viewer is userID when authed, else the client IP).
	RegisterView(ctx context.Context, streamID uuid.UUID, viewerKey string) error
}