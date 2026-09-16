package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/mrhumster/stats-service/internal/domain/models"
	"gorm.io/gorm"
)

type GormStatsRepository struct {
	db *gorm.DB
}

func NewGormStatsRepository(db *gorm.DB) *GormStatsRepository {
	return &GormStatsRepository{db: db}
}

func (r *GormStatsRepository) UpsertReaction(ctx context.Context, streamID, userID uuid.UUID, kind models.ReactionKind, now time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO reactions (stream_id, user_id, kind, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (stream_id, user_id) DO UPDATE
		SET kind = EXCLUDED.kind, updated_at = EXCLUDED.updated_at`,
		streamID, userID, kind, now, now).Error
}

func (r *GormStatsRepository) DeleteReaction(ctx context.Context, streamID, userID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Where("stream_id = ? AND user_id = ?", streamID, userID).
		Delete(&models.Reaction{}).Error
}

func (r *GormStatsRepository) GetReaction(ctx context.Context, streamID, userID uuid.UUID) (*models.Reaction, error) {
	var reaction models.Reaction
	err := r.db.WithContext(ctx).
		Where("stream_id = ? AND user_id = ?", streamID, userID).
		First(&reaction).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &reaction, nil
}

func (r *GormStatsRepository) Counts(ctx context.Context, streamID uuid.UUID) (likes, dislikes int64, err error) {
	err = r.db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(sum((kind = 'like')::int), 0)::bigint  AS likes,
			COALESCE(sum((kind = 'dislike')::int), 0)::bigint AS dislikes
		FROM reactions
		WHERE stream_id = ?`, streamID).
		Row().Scan(&likes, &dislikes)
	return likes, dislikes, err
}

func (r *GormStatsRepository) GetViews(ctx context.Context, streamID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.StreamViews{}).
		Where("stream_id = ?", streamID).
		Select("count").
		Scan(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *GormStatsRepository) IncrementViews(ctx context.Context, streamID uuid.UUID, now time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO stream_views (stream_id, count, updated_at)
		VALUES (?, 1, ?)
		ON CONFLICT (stream_id) DO UPDATE
		SET count = stream_views.count + 1, updated_at = EXCLUDED.updated_at`,
		streamID, now).Error
}