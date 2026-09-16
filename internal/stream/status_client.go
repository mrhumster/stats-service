//go:generate mockgen -source=status_client.go -destination=mock/status_client_mock.go -package=mock
package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

var (
	ErrStreamNotFound    = errors.New("stream not found")
	ErrStreamUnavailable = errors.New("stream service unavailable")
)

// StatusInfo is the response of the public stream-service endpoint
// GET /stream/:id/status. OwnerID and Title are used for activity events.
type StatusInfo struct {
	Status     string    `json:"status"`
	Visibility string    `json:"visibility"`
	OwnerID    uuid.UUID `json:"owner_id"`
	Title      string    `json:"title"`
}

// Commentable mirrors the comments-service gate: only published streams with
// non-private visibility accept reactions and view counting.
func (s StatusInfo) Commentable() bool {
	return s.Status == "published" && s.Visibility != "private"
}

// StatusClient resolves the stream state for reaction/view gates. Fail-closed:
// transport errors surface ErrStreamUnavailable and the caller rejects.
type StatusClient interface {
	GetStreamStatus(ctx context.Context, streamID uuid.UUID) (*StatusInfo, error)
}

type HTTPStatusClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPStatusClient(baseURL string) *HTTPStatusClient {
	return &HTTPStatusClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *HTTPStatusClient) GetStreamStatus(ctx context.Context, streamID uuid.UUID) (*StatusInfo, error) {
	// N.B.: resolve path via fmt instead of path.Join so an absent trailing
	// slash in baseURL does not change the address.
	url := fmt.Sprintf("%s/stream/%s/status", c.baseURL, streamID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build status request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStreamUnavailable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// fallthrough to decode below
	case http.StatusNotFound:
		return nil, ErrStreamNotFound
	default:
		return nil, fmt.Errorf("%w: unexpected status %d", ErrStreamUnavailable, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrStreamUnavailable, err)
	}

	var info StatusInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("%w: decode status: %v", ErrStreamUnavailable, err)
	}
	return &info, nil
}