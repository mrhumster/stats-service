//go:generate mockgen -source=activity_event.go -destination=mock/activity_event_mock.go -package=mock
package queue

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// Producer contract with events-service: tasks are enqueued into the asynq DB
// (cfg.Redis.QueueDB, default 3), queue "events", task "event:activity".
// JSON field names must match services/events-service/internal/queue.
const (
	TaskActivityEvent = "event:activity"
	TaskActivityQueue = "events"
)

type ActivityEventPayload struct {
	EventID    uuid.UUID       `json:"event_id"`
	UserID     uuid.UUID       `json:"user_id"`
	EventType  string          `json:"event_type"`
	StreamID   *uuid.UUID      `json:"stream_id,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
}

func NewActivityEventTask(p ActivityEventPayload) (*asynq.Task, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TaskActivityEvent, payload), nil
}

// ActivityEventRecorder records user-facing activity for an actor/owner.
type ActivityEventRecorder interface {
	RecordActivityEvent(ctx context.Context, userID uuid.UUID, eventType string, streamID *uuid.UUID, payload any) error
}

// AsyncActivityRecorder enqueues activity events into the events-service
// queue via its own asynq client. Enqueue is best-effort: callers log the
// error and never fail the request.
type AsyncActivityRecorder struct {
	client *asynq.Client
}

func NewAsyncActivityRecorder(redisOpt asynq.RedisClientOpt) *AsyncActivityRecorder {
	return &AsyncActivityRecorder{client: asynq.NewClient(redisOpt)}
}

func (r *AsyncActivityRecorder) Close() error {
	return r.client.Close()
}

func (r *AsyncActivityRecorder) RecordActivityEvent(ctx context.Context, userID uuid.UUID, eventType string, streamID *uuid.UUID, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	task, err := NewActivityEventTask(ActivityEventPayload{
		EventID:    uuid.New(),
		UserID:     userID,
		EventType:  eventType,
		StreamID:   streamID,
		Payload:    raw,
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	_, err = r.client.EnqueueContext(ctx, task,
		asynq.Queue(TaskActivityQueue),
		asynq.MaxRetry(3),
		asynq.Timeout(30*time.Second),
	)
	return err
}