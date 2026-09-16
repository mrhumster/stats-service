package models

import (
	"time"

	"github.com/google/uuid"
)

type ReactionKind string

const (
	ReactionLike    ReactionKind = "like"
	ReactionDislike ReactionKind = "dislike"
	ReactionNone    ReactionKind = "none"
)

func (k ReactionKind) Valid() bool {
	return k == ReactionLike || k == ReactionDislike || k == ReactionNone
}

// Reaction is a single like/dislike of one user on one stream
// (upserted key (stream_id, user_id)).
type Reaction struct {
	StreamID  uuid.UUID    `json:"stream_id"`
	UserID    uuid.UUID    `json:"user_id"`
	Kind      ReactionKind `json:"kind"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func (Reaction) TableName() string { return "reactions" }

// StreamViews is the per-stream anonymous view counter.
type StreamViews struct {
	StreamID  uuid.UUID `json:"stream_id"`
	Count     int64     `json:"count"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (StreamViews) TableName() string { return "stream_views" }

// Stats aggregates the public counters of a stream. MyReaction is only set
// when the caller is authenticated.
type Stats struct {
	Views       int64        `json:"views"`
	Likes       int64        `json:"likes"`
	Dislikes    int64        `json:"dislikes"`
	MyReaction *ReactionKind `json:"my_reaction,omitempty"`
}

const (
	EventReactionLiked    = "reaction.liked"
	EventReactionDisliked = "reaction.disliked"
)

// ReactionEventPayload is the extra payload for activity events emitted by
// stats-service. ActorEmail is the reacting user, Kind the new reaction.
type ReactionEventPayload struct {
	ActorEmail string `json:"actor_email"`
	Kind       string `json:"kind"`
	Title      string `json:"title,omitempty"`
}