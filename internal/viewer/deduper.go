//go:generate mockgen -source=deduper.go -destination=mock/viewer_deduper_mock.go -package=mock

package viewer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	// ViewTTL is the dedup window: one view per viewer per stream per day.
	ViewTTL = 24 * time.Hour

	viewKeyPrefix = "view:"
)

// Deduper answers "has this viewer already been counted in the current
// window?" for a stream. It lets the public view endpoint drop trivial
// inflation (scripted replays) while keeping guests counted by IP and
// signed-in viewers by account.
type Deduper interface {
	// Allow reports whether the viewer may register a view. A Redis error is
	// returned as-is; callers fail open so an unavailable cache never breaks
	// the view counter.
	Allow(ctx context.Context, streamID uuid.UUID, viewerKey string) (bool, error)
	Close() error
}

// RedisDeduper dedupes with SETNX keyed on stream+viewer with a 24h TTL.
type RedisDeduper struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewRedisDeduper(addr, password string, db int) *RedisDeduper {
	return &RedisDeduper{
		rdb: redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db}),
		ttl: ViewTTL,
	}
}

func (d *RedisDeduper) Allow(ctx context.Context, streamID uuid.UUID, viewerKey string) (bool, error) {
	key := viewKeyPrefix + streamID.String() + ":" + viewerKey
	ok, err := d.rdb.SetNX(ctx, key, 1, d.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("view dedup SETNX: %w", err)
	}
	return ok, nil
}

func (d *RedisDeduper) Close() error {
	return d.rdb.Close()
}