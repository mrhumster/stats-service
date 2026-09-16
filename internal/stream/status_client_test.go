package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHTTPStatusClient_GetStreamStatus(t *testing.T) {
	testID := uuid.MustParse("00000000-0000-0000-0000-000000000042")

	t.Run("published public with owner", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/stream/"+testID.String()+"/status", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"published","visibility":"public","owner_id":"` + testID.String() + `","title":"Sunset"}`))
		}))
		defer srv.Close()

		client := NewHTTPStatusClient(srv.URL)
		info, err := client.GetStreamStatus(context.Background(), testID)
		require.NoError(t, err)
		require.True(t, info.Commentable())
		require.Equal(t, testID, info.OwnerID)
		require.Equal(t, "Sunset", info.Title)
	})

	t.Run("published private rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"published","visibility":"private"}`))
		}))
		defer srv.Close()

		client := NewHTTPStatusClient(srv.URL)
		info, err := client.GetStreamStatus(context.Background(), testID)
		require.NoError(t, err)
		require.False(t, info.Commentable())
	})

	t.Run("not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"stream not found"}`))
		}))
		defer srv.Close()

		client := NewHTTPStatusClient(srv.URL)
		_, err := client.GetStreamStatus(context.Background(), testID)
		require.ErrorIs(t, err, ErrStreamNotFound)
	})

	t.Run("unexpected status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		client := NewHTTPStatusClient(srv.URL)
		_, err := client.GetStreamStatus(context.Background(), testID)
		require.Error(t, err)
	})
}