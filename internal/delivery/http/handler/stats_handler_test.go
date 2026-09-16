package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mrhumster/stats-service/internal/domain/models"
	"github.com/mrhumster/stats-service/internal/service"
	svcmock "github.com/mrhumster/stats-service/internal/service/mock"
	"go.uber.org/mock/gomock"
)

func newRouter(svc *svcmock.MockStatsService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewStatsHandler(svc)
	r.GET("/streams/:streamId/stats", h.GetStats)
	r.POST("/streams/:streamId/views", h.RegisterView)
	r.PUT("/streams/:streamId/reaction", h.SetReaction)
	return r
}

func do(method, path string, svc *svcmock.MockStatsService) *httptest.ResponseRecorder {
	r := newRouter(svc)
	req := httptest.NewRequest(method, path, strings.NewReader(`{"kind":"like"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGetStatsPublic(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := svcmock.NewMockStatsService(ctrl)
	defer ctrl.Finish()

	streamID := uuid.New()
	svc.EXPECT().GetStats(gomock.Any(), streamID, gomock.Any()).Return(&models.Stats{
		Views:    100,
		Likes:    5,
		Dislikes: 2,
	}, nil)

	w := do("GET", "/streams/"+streamID.String()+"/stats", svc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"views":100`) || !strings.Contains(w.Body.String(), `"likes":5`) {
		t.Errorf("stats body missing")
	}
}

func TestGetStatsInvalidStreamID(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := svcmock.NewMockStatsService(ctrl)
	defer ctrl.Finish()

	w := do("GET", "/streams/not-a-uuid/stats", svc)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSetReactionUnverified(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := svcmock.NewMockStatsService(ctrl)
	defer ctrl.Finish()

	svc.EXPECT().SetReaction(gomock.Any(), gomock.Any(), gomock.Any(), models.ReactionLike).Return(nil, service.ErrEmailNotVerified)

	w := do("PUT", "/streams/"+uuid.New().String()+"/reaction", svc)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "email not verified") {
		t.Errorf("missing error reason")
	}
}

func TestSetReactionOK(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := svcmock.NewMockStatsService(ctrl)
	defer ctrl.Finish()

	kind := models.ReactionLike
	svc.EXPECT().SetReaction(gomock.Any(), gomock.Any(), gomock.Any(), models.ReactionLike).Return(&models.Stats{
		Views:       10,
		Likes:       3,
		Dislikes:    0,
		MyReaction: &kind,
	}, nil)

	w := do("PUT", "/streams/"+uuid.New().String()+"/reaction", svc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"my_reaction":"like"`) {
		t.Errorf("missing my_reaction")
	}
}

func TestSetReactionInvalidKind(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := svcmock.NewMockStatsService(ctrl)
	defer ctrl.Finish()

	r := newRouter(svc)
	req := httptest.NewRequest("PUT", "/streams/"+uuid.New().String()+"/reaction", strings.NewReader(`{"kind":"meh"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRegisterViewOK(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := svcmock.NewMockStatsService(ctrl)
	defer ctrl.Finish()

	streamID := uuid.New()
	svc.EXPECT().RegisterView(gomock.Any(), streamID, "ip:192.0.2.1").Return(nil)

	w := do("POST", "/streams/"+streamID.String()+"/views", svc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Errorf("missing ok")
	}
}

func TestWriteServiceErrors(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		code  int
		body  string
	}{
		{"stream not published", service.ErrStreamNotPublished, http.StatusForbidden, "stream is not published"},
		{"stream not found", service.ErrStreamNotFound, http.StatusNotFound, "stream not found"},
		{"stream unavailable", service.ErrStreamUnavailable, http.StatusServiceUnavailable, "internal server error"},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "internal server error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			svc := svcmock.NewMockStatsService(ctrl)
			defer ctrl.Finish()

			svc.EXPECT().SetReaction(gomock.Any(), gomock.Any(), gomock.Any(), models.ReactionLike).Return(nil, tc.err)

			w := do("PUT", "/streams/"+uuid.New().String()+"/reaction", svc)
			if w.Code != tc.code {
				t.Fatalf("expected %d, got %d", tc.code, w.Code)
			}
			if !strings.Contains(w.Body.String(), tc.body) {
				t.Errorf("missing body text")
			}
		})
	}
}