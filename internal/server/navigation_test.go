package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNavigationSummaryRequiresGETAndConfiguredServices(t *testing.T) {
	s := New(nil, nil, nil, ":0")

	post := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/navigation-summary", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d body=%s", post.Code, post.Body.String())
	}

	get := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/navigation-summary", nil))
	if get.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
}

type countingLearning struct {
	LearningService
	candidates int
}

func (c countingLearning) CandidateCount(context.Context) (int, error) { return c.candidates, nil }

func TestNavigationSummaryCountsLearningsWaitingForADecision(t *testing.T) {
	s := New(nil, nil, nil, ":0")
	if count, err := s.learningCandidates(context.Background()); err != nil || count != 0 {
		t.Fatalf("without a learning service: %d, %v", count, err)
	}

	s.SetLearningService(countingLearning{candidates: 8})
	count, err := s.learningCandidates(context.Background())
	if err != nil || count != 8 {
		t.Fatalf("learning candidates = %d, %v; want 8", count, err)
	}
}
