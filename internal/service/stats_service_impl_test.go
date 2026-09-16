package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mrhumster/stats-service/internal/domain/models"
	"github.com/mrhumster/stats-service/internal/queue/mock"
	repomock "github.com/mrhumster/stats-service/internal/repository/mock"
	"github.com/mrhumster/stats-service/internal/stream"
	streammock "github.com/mrhumster/stats-service/internal/stream/mock"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
)

var testStatusInfo = &stream.StatusInfo{
	Status:     "published",
	Visibility: "public",
	OwnerID:    uuid.New(),
	Title:      "Test Stream",
}

func newTestService(t *testing.T) (*StatsServiceImpl, *repomock.MockStatsRepository, *mock.MockActivityEventRecorder) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := repomock.NewMockStatsRepository(ctrl)
	rec := mock.NewMockActivityEventRecorder(ctrl)
	svc := NewStatsServiceImpl(repo)
	svc.WithActivityRecorder(rec)
	sclient := streammock.NewMockStatusClient(ctrl)
	sclient.EXPECT().GetStreamStatus(gomock.Any(), gomock.Any()).
		Return(testStatusInfo, nil).AnyTimes()
	svc.WithStreamStatusClient(sclient)
	return svc, repo, rec
}

func newGateTestService(t *testing.T) (*StatsServiceImpl, *repomock.MockStatsRepository, *mock.MockActivityEventRecorder, *streammock.MockStatusClient) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := repomock.NewMockStatsRepository(ctrl)
	rec := mock.NewMockActivityEventRecorder(ctrl)
	svc := NewStatsServiceImpl(repo)
	svc.WithActivityRecorder(rec)
	sclient := streammock.NewMockStatusClient(ctrl)
	svc.WithStreamStatusClient(sclient)
	return svc, repo, rec, sclient
}

func verifiedActor() Actor {
	return Actor{UserID: uuid.New(), Role: "member", EmailVerified: true}
}

func TestSetReaction_Gates(t *testing.T) {
	t.Run("unverified member rejected", func(t *testing.T) {
		svc, _, rec := newTestService(t)
		_, err := svc.SetReaction(context.Background(), Actor{UserID: uuid.New(), Role: "member", EmailVerified: false}, uuid.New(), models.ReactionLike)
		require.ErrorIs(t, err, ErrEmailNotVerified)
		_ = rec
	})

	t.Run("admin bypasses verified gate", func(t *testing.T) {
		svc, repo, rec := newTestService(t)
		streamID := uuid.New()
		repo.EXPECT().GetReaction(gomock.Any(), streamID, gomock.Any()).Return(nil, nil)
		repo.EXPECT().UpsertReaction(gomock.Any(), streamID, gomock.Any(), models.ReactionLike, gomock.Any()).Return(nil)
		repo.EXPECT().Counts(gomock.Any(), streamID).Return(int64(1), int64(0), nil)
		repo.EXPECT().GetViews(gomock.Any(), streamID).Return(int64(42), nil)
		rec.EXPECT().RecordActivityEvent(gomock.Any(), testStatusInfo.OwnerID, models.EventReactionLiked, gomock.Any(), gomock.Any()).Return(nil)

		stats, err := svc.SetReaction(context.Background(), Actor{UserID: uuid.New(), Role: "admin", EmailVerified: false}, streamID, models.ReactionLike)
		require.NoError(t, err)
		require.Equal(t, int64(1), stats.Likes)
		require.Equal(t, int64(42), stats.Views)
		require.Equal(t, models.ReactionLike, *stats.MyReaction)
	})

	t.Run("invalid kind rejected", func(t *testing.T) {
		svc, _, _ := newTestService(t)
		_, err := svc.SetReaction(context.Background(), verifiedActor(), uuid.New(), models.ReactionKind("meh"))
		require.ErrorIs(t, err, ErrInvalidReactionKind)
	})
}

func TestSetReaction_StreamGate(t *testing.T) {
	t.Run("published private rejected", func(t *testing.T) {
		svc, _, _, sclient := newGateTestService(t)
		streamID := uuid.New()
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).
			Return(&stream.StatusInfo{Status: "published", Visibility: "private", OwnerID: uuid.New()}, nil)
		_, err := svc.SetReaction(context.Background(), verifiedActor(), streamID, models.ReactionLike)
		require.ErrorIs(t, err, ErrStreamNotPublished)
	})

	t.Run("non-published rejected (admin included)", func(t *testing.T) {
		svc, _, _, sclient := newGateTestService(t)
		streamID := uuid.New()
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).
			Return(&stream.StatusInfo{Status: "draft", Visibility: "public"}, nil)
		_, err := svc.SetReaction(context.Background(), Actor{UserID: uuid.New(), Role: "admin", EmailVerified: false}, streamID, models.ReactionLike)
		require.ErrorIs(t, err, ErrStreamNotPublished)
	})

	t.Run("stream not found", func(t *testing.T) {
		svc, _, _, sclient := newGateTestService(t)
		streamID := uuid.New()
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).Return(nil, stream.ErrStreamNotFound)
		_, err := svc.SetReaction(context.Background(), verifiedActor(), streamID, models.ReactionLike)
		require.ErrorIs(t, err, ErrStreamNotFound)
	})

	t.Run("stream service down fails closed", func(t *testing.T) {
		svc, _, _, sclient := newGateTestService(t)
		streamID := uuid.New()
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).Return(nil, errors.New("dial tcp: refused"))
		_, err := svc.SetReaction(context.Background(), verifiedActor(), streamID, models.ReactionLike)
		require.ErrorIs(t, err, ErrStreamUnavailable)
	})

	t.Run("no client fails closed", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		repo := repomock.NewMockStatsRepository(ctrl)
		svc := NewStatsServiceImpl(repo)
		_, err := svc.SetReaction(context.Background(), verifiedActor(), uuid.New(), models.ReactionLike)
		require.ErrorIs(t, err, ErrStreamUnavailable)
	})
}

func TestSetReaction_Events(t *testing.T) {
	streamID := uuid.New()
	info := &stream.StatusInfo{
		Status:     "published",
		Visibility: "public",
		OwnerID:    uuid.New(),
		Title:      "Sunset",
	}

	t.Run("new like emits reaction.liked to owner", func(t *testing.T) {
		svc, repo, rec, sclient := newGateTestService(t)
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).Return(info, nil)
		repo.EXPECT().GetReaction(gomock.Any(), streamID, gomock.Any()).Return(nil, nil)
		repo.EXPECT().UpsertReaction(gomock.Any(), streamID, gomock.Any(), models.ReactionLike, gomock.Any()).Return(nil)
		repo.EXPECT().Counts(gomock.Any(), streamID).Return(int64(1), int64(0), nil)
		repo.EXPECT().GetViews(gomock.Any(), streamID).Return(int64(0), nil)
		rec.EXPECT().RecordActivityEvent(gomock.Any(), info.OwnerID, models.EventReactionLiked, gomock.Any(), gomock.Any()).Return(nil)

		stats, err := svc.SetReaction(context.Background(), verifiedActor(), streamID, models.ReactionLike)
		require.NoError(t, err)
		require.Equal(t, int64(1), stats.Likes)
	})

	t.Run("switching to dislike emits reaction.disliked", func(t *testing.T) {
		svc, repo, rec, sclient := newGateTestService(t)
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).Return(info, nil)
		repo.EXPECT().GetReaction(gomock.Any(), streamID, gomock.Any()).
			Return(&models.Reaction{StreamID: streamID, Kind: models.ReactionLike}, nil)
		repo.EXPECT().UpsertReaction(gomock.Any(), streamID, gomock.Any(), models.ReactionDislike, gomock.Any()).Return(nil)
		repo.EXPECT().Counts(gomock.Any(), streamID).Return(int64(0), int64(1), nil)
		repo.EXPECT().GetViews(gomock.Any(), streamID).Return(int64(0), nil)
		rec.EXPECT().RecordActivityEvent(gomock.Any(), info.OwnerID, models.EventReactionDisliked, gomock.Any(), gomock.Any()).Return(nil)

		stats, err := svc.SetReaction(context.Background(), verifiedActor(), streamID, models.ReactionDislike)
		require.NoError(t, err)
		require.Equal(t, int64(1), stats.Dislikes)
	})

	t.Run("repeated same reaction sends no event", func(t *testing.T) {
		svc, repo, _, sclient := newGateTestService(t)
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).Return(info, nil)
		repo.EXPECT().GetReaction(gomock.Any(), streamID, gomock.Any()).
			Return(&models.Reaction{StreamID: streamID, Kind: models.ReactionLike}, nil)
		repo.EXPECT().UpsertReaction(gomock.Any(), streamID, gomock.Any(), models.ReactionLike, gomock.Any()).Return(nil)
		repo.EXPECT().Counts(gomock.Any(), streamID).Return(int64(1), int64(0), nil)
		repo.EXPECT().GetViews(gomock.Any(), streamID).Return(int64(0), nil)

		stats, err := svc.SetReaction(context.Background(), verifiedActor(), streamID, models.ReactionLike)
		require.NoError(t, err)
		require.Equal(t, int64(1), stats.Likes)
	})

	t.Run("owner self-reaction emits no event", func(t *testing.T) {
		ownerID := uuid.New()
		ownerInfo := &stream.StatusInfo{
			Status:     "published",
			Visibility: "public",
			OwnerID:    ownerID,
			Title:      "Mine",
		}
		svc, repo, _, sclient := newGateTestService(t)
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).Return(ownerInfo, nil)
		repo.EXPECT().GetReaction(gomock.Any(), streamID, ownerID).Return(nil, nil)
		repo.EXPECT().UpsertReaction(gomock.Any(), streamID, ownerID, models.ReactionLike, gomock.Any()).Return(nil)
		repo.EXPECT().Counts(gomock.Any(), streamID).Return(int64(1), int64(0), nil)
		repo.EXPECT().GetViews(gomock.Any(), streamID).Return(int64(0), nil)

		_, err := svc.SetReaction(context.Background(), Actor{UserID: ownerID, Role: "member", EmailVerified: true}, streamID, models.ReactionLike)
		require.NoError(t, err)
	})

	t.Run("removing reaction emits no event", func(t *testing.T) {
		svc, repo, rec, sclient := newGateTestService(t)
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).Return(info, nil)
		repo.EXPECT().GetReaction(gomock.Any(), streamID, gomock.Any()).
			Return(&models.Reaction{StreamID: streamID, Kind: models.ReactionLike}, nil)
		repo.EXPECT().DeleteReaction(gomock.Any(), streamID, gomock.Any()).Return(nil)
		repo.EXPECT().Counts(gomock.Any(), streamID).Return(int64(0), int64(0), nil)
		repo.EXPECT().GetViews(gomock.Any(), streamID).Return(int64(0), nil)
		rec.EXPECT().RecordActivityEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		_, err := svc.SetReaction(context.Background(), verifiedActor(), streamID, models.ReactionNone)
		require.NoError(t, err)
	})

	t.Run("event best-effort does not fail the request", func(t *testing.T) {
		svc, repo, rec, sclient := newGateTestService(t)
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).Return(info, nil)
		repo.EXPECT().GetReaction(gomock.Any(), streamID, gomock.Any()).Return(nil, nil)
		repo.EXPECT().UpsertReaction(gomock.Any(), streamID, gomock.Any(), models.ReactionLike, gomock.Any()).Return(nil)
		repo.EXPECT().Counts(gomock.Any(), streamID).Return(int64(1), int64(0), nil)
		repo.EXPECT().GetViews(gomock.Any(), streamID).Return(int64(0), nil)
		rec.EXPECT().RecordActivityEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("redis down"))

		stats, err := svc.SetReaction(context.Background(), verifiedActor(), streamID, models.ReactionLike)
		require.NoError(t, err)
		require.Equal(t, int64(1), stats.Likes)
	})
}

func TestRegisterView_GateAndIncrement(t *testing.T) {
	t.Run("published stream increments", func(t *testing.T) {
		svc, repo, _ := newTestService(t)
		streamID := uuid.New()
		repo.EXPECT().IncrementViews(gomock.Any(), streamID, gomock.Any()).Return(nil)
		require.NoError(t, svc.RegisterView(context.Background(), streamID))
	})

	t.Run("published private rejected", func(t *testing.T) {
		svc, _, _, sclient := newGateTestService(t)
		streamID := uuid.New()
		sclient.EXPECT().GetStreamStatus(gomock.Any(), streamID).
			Return(&stream.StatusInfo{Status: "published", Visibility: "private", OwnerID: uuid.New()}, nil)
		err := svc.RegisterView(context.Background(), streamID)
		require.ErrorIs(t, err, ErrStreamNotPublished)
	})
}

func TestGetStats(t *testing.T) {
	t.Run("with reaction for authed user", func(t *testing.T) {
		svc, repo, _ := newTestService(t)
		streamID := uuid.New()
		repo.EXPECT().Counts(gomock.Any(), streamID).Return(int64(5), int64(2), nil)
		repo.EXPECT().GetViews(gomock.Any(), streamID).Return(int64(100), nil)
		repo.EXPECT().GetReaction(gomock.Any(), streamID, gomock.Any()).
			Return(&models.Reaction{Kind: models.ReactionDislike}, nil)

		stats, err := svc.GetStats(context.Background(), streamID, &Actor{UserID: uuid.New()})
		require.NoError(t, err)
		require.Equal(t, int64(5), stats.Likes)
		require.Equal(t, int64(2), stats.Dislikes)
		require.Equal(t, int64(100), stats.Views)
		require.Equal(t, models.ReactionDislike, *stats.MyReaction)
	})

	t.Run("anonymous has no reaction", func(t *testing.T) {
		svc, repo, _ := newTestService(t)
		streamID := uuid.New()
		repo.EXPECT().Counts(gomock.Any(), streamID).Return(int64(0), int64(0), nil)
		repo.EXPECT().GetViews(gomock.Any(), streamID).Return(int64(12), nil)

		stats, err := svc.GetStats(context.Background(), streamID, nil)
		require.NoError(t, err)
		require.Nil(t, stats.MyReaction)
		require.Equal(t, int64(12), stats.Views)
	})
}