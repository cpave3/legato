package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type activityBoard struct {
	*mockBoardService
	activity *service.TaskActivity
}

func (b *activityBoard) TaskActivity(context.Context, string) (*service.TaskActivity, error) {
	return b.activity, nil
}
func TestTaskActivityEndpointReturnsRelatedCollections(t *testing.T) {
	board := &activityBoard{mockBoardService: &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, activity: &service.TaskActivity{Reviews: []store.ReviewTour{{ID: "rt-1"}}, Plans: []store.Plan{{ID: "pl-1"}}, WorkflowRuns: []service.TaskWorkflowActivity{{Run: store.WorkflowRun{ID: "wr-1"}, Artifacts: []store.WorkflowArtifact{{ID: "wfa-1"}}}}}}
	handler := New(board, nil, nil, ":0").Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/T-1/activity", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var got service.TaskActivity
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Reviews) != 1 || len(got.Plans) != 1 || len(got.WorkflowRuns) != 1 || len(got.WorkflowRuns[0].Artifacts) != 1 {
		t.Fatalf("activity=%#v", got)
	}
}
