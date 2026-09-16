package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/mrhumster/stats-service/internal/domain/models"
	"github.com/mrhumster/stats-service/internal/queue"
	"github.com/mrhumster/stats-service/internal/repository"
	"github.com/mrhumster/stats-service/internal/stream"
	"github.com/mrhumster/stats-service/internal/viewer"
)

type StatsServiceImpl struct {
	repo         repository.StatsRepository
	recorder     queue.ActivityEventRecorder
	streamStatus stream.StatusClient
	viewDeduper  viewer.Deduper
}

func NewStatsServiceImpl(repo repository.StatsRepository) *StatsServiceImpl {
	return &StatsServiceImpl{repo: repo}
}

func (s *StatsServiceImpl) WithActivityRecorder(r queue.ActivityEventRecorder) {
	s.recorder = r
}

func (s *StatsServiceImpl) WithStreamStatusClient(c stream.StatusClient) {
	s.streamStatus = c
}

func (s *StatsServiceImpl) WithViewDeduper(d viewer.Deduper) {
	s.viewDeduper = d
}

func (s *StatsServiceImpl) GetStats(ctx context.Context, streamID uuid.UUID, actor *Actor) (*models.Stats, error) {
	likes, dislikes, err := s.repo.Counts(ctx, streamID)
	if err != nil {
		return nil, err
	}
	views, err := s.repo.GetViews(ctx, streamID)
	if err != nil {
		return nil, err
	}

	stats := &models.Stats{Views: views, Likes: likes, Dislikes: dislikes}

	if actor != nil {
		reaction, err := s.repo.GetReaction(ctx, streamID, actor.UserID)
		if err != nil {
			return nil, err
		}
		if reaction != nil {
			kind := reaction.Kind
			stats.MyReaction = &kind
		}
	}
	return stats, nil
}

func (s *StatsServiceImpl) SetReaction(ctx context.Context, actor Actor, streamID uuid.UUID, kind models.ReactionKind) (*models.Stats, error) {
	if actor.Role != "admin" && !actor.EmailVerified {
		return nil, ErrEmailNotVerified
	}
	if !kind.Valid() {
		return nil, ErrInvalidReactionKind
	}

	info, err := s.checkStreamCommentable(ctx, streamID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	// Determine the actor's previous reaction so we only emit a notification
	// when the reaction actually changes (and never on removal).
	var previous models.ReactionKind
	existing, err := s.repo.GetReaction(ctx, streamID, actor.UserID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		previous = existing.Kind
	}

	if kind == models.ReactionNone {
		if existing != nil {
			if err := s.repo.DeleteReaction(ctx, streamID, actor.UserID); err != nil {
				return nil, err
			}
		}
	} else {
		if err := s.repo.UpsertReaction(ctx, streamID, actor.UserID, kind, now); err != nil {
			return nil, err
		}
		if info != nil && kind != previous && !actorIsOwner(actor, info) {
			s.recordEvent(ctx, actor, info, streamID, kind)
		}
	}

	likes, dislikes, err := s.repo.Counts(ctx, streamID)
	if err != nil {
		return nil, err
	}
	views, err := s.repo.GetViews(ctx, streamID)
	if err != nil {
		return nil, err
	}

	stats := &models.Stats{Views: views, Likes: likes, Dislikes: dislikes}
	if kind != models.ReactionNone {
		my := kind
		stats.MyReaction = &my
	}
	return stats, nil
}

func (s *StatsServiceImpl) RegisterView(ctx context.Context, streamID uuid.UUID, viewerKey string) error {
	if _, err := s.checkStreamCommentable(ctx, streamID); err != nil {
		return err
	}

	// Dedup by viewer: once per window the counter is bumped, otherwise the
	// request is silently accepted. An unavailable deduper fails open (view
	// still counts, dedup degraded) so the counter never breaks.
	if s.viewDeduper != nil {
		allowed, err := s.viewDeduper.Allow(ctx, streamID, viewerKey)
		if err != nil {
			slog.Warn("view dedup unavailable, failing open", "stream_id", streamID, "error", err)
		} else if !allowed {
			return nil
		}
	}

	return s.repo.IncrementViews(ctx, streamID, time.Now().UTC())
}

// checkStreamCommentable fails closed: no client configured, lookup errors or
// a non-published/private stream all reject reactions and views.
func (s *StatsServiceImpl) checkStreamCommentable(ctx context.Context, streamID uuid.UUID) (*stream.StatusInfo, error) {
	if s.streamStatus == nil {
		return nil, ErrStreamUnavailable
	}
	info, err := s.streamStatus.GetStreamStatus(ctx, streamID)
	if err != nil {
		if errors.Is(err, stream.ErrStreamNotFound) {
			return nil, ErrStreamNotFound
		}
		slog.Error("check stream status", "error", err)
		return nil, ErrStreamUnavailable
	}
	if !info.Commentable() {
		return nil, ErrStreamNotPublished
	}
	return info, nil
}

func actorIsOwner(actor Actor, info *stream.StatusInfo) bool {
	return info.OwnerID == actor.UserID
}

func (s *StatsServiceImpl) recordEvent(ctx context.Context, actor Actor, info *stream.StatusInfo, streamID uuid.UUID, kind models.ReactionKind) {
	if s.recorder == nil {
		return
	}
	eventType := models.EventReactionLiked
	if kind == models.ReactionDislike {
		eventType = models.EventReactionDisliked
	}
	payload := models.ReactionEventPayload{
		ActorEmail: actor.Email,
		Kind:       string(kind),
		Title:      info.Title,
	}
	sid := streamID
	if err := s.recorder.RecordActivityEvent(ctx, info.OwnerID, eventType, &sid, payload); err != nil {
		slog.Warn("record reaction activity event", "error", err)
	}
}