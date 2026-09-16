//go:generate mockgen -source=stats_repository.go -destination=mock/stats_repository_mock.go -package=mock
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mrhumster/stats-service/internal/domain/models"
)

type StatsRepository interface {
	UpsertReaction(ctx context.Context, streamID, userID uuid.UUID, kind models.ReactionKind, now time.Time) error
	DeleteReaction(ctx context.Context, streamID, userID uuid.UUID) error
	GetReaction(ctx context.Context, streamID, userID uuid.UUID) (*models.Reaction, error)
	Counts(ctx context.Context, streamID uuid.UUID) (likes, dislikes int64, err error)
	GetViews(ctx context.Context, streamID uuid.UUID) (int64, error)
	IncrementViews(ctx context.Context, streamID uuid.UUID, now time.Time) error
}