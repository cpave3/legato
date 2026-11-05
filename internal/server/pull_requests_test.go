package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

func stackedCardDetail() *service.CardDetail {
	return &service.CardDetail{
		ID: "t1", Title: "Task one", Status: "Doing", CreatedAt: time.Now(), UpdatedAt: time.Now(),
		PRMeta: &service.PRMetaView{Repo: "acme/app", Branch: "legato/t1-a", PRNumber: 11, SyncOutcome: "linked"},
		PullRequests: []service.PRMetaView{
			{Repo: "acme/app", Branch: "legato/t1-a", PRNumber: 11, PRURL: "https://github.com/acme/app/pull/11", State: "OPEN", CheckStatus: "fail"},
			{Repo: "acme/app", Branch: "legato/t1-b", PRNumber: 12, PRURL: "https://github.com/acme/app/pull/12", State: "OPEN", IsDraft: true, ReviewDecision: "APPROVED", CommentCount: 2},
		},
		Branches: []service.TrackedBranchView{
			{Repo: "acme/app", Branch: "legato/t1", Source: "code_context"},
			{Repo: "acme/app", Branch: "legato/t1-a", Source: "stack", PR: &service.PRMetaView{PRNumber: 11}},
			{Repo: "acme/app", Branch: "legato/t1-b", Source: "stack", PR: &service.PRMetaView{PRNumber: 12}},
		},
	}
}

func TestBoardCardsListEveryPRInStackOrder(t *testing.T) {
	svc := &mockBoardService{
		columns:    []service.Column{{Name: "Doing"}},
		cards:      map[string][]service.Card{"Doing": {{ID: "t1", Title: "Task one", Status: "Doing"}}},
		cardDetail: stackedCardDetail(),
	}
	w := httptest.NewRecorder()
	New(svc, nil, nil, ":0").Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp BoardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	prs := resp.Columns[0].Cards[0].PullRequests
	if len(prs) != 2 || prs[0].PRNumber != 11 || prs[0].CheckStatus != "fail" || prs[1].PRNumber != 12 || !prs[1].IsDraft ||
		prs[1].ReviewDecision != "APPROVED" || prs[1].CommentCount != 2 || prs[1].Branch != "legato/t1-b" || prs[1].PRURL == "" {
		t.Fatalf("pull_requests = %+v", prs)
	}
}

func TestTaskDetailListsEveryPRAndTrackedBranch(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}, cardDetail: stackedCardDetail()}
	w := httptest.NewRecorder()
	New(svc, nil, nil, ":0").Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tasks/t1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp CardDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.PullRequests) != 2 || resp.PullRequests[0].PRNumber != 11 || resp.PullRequests[1].PRNumber != 12 {
		t.Fatalf("pull_requests = %+v", resp.PullRequests)
	}
	if len(resp.TrackedBranches) != 3 || resp.TrackedBranches[0].Branch != "legato/t1" || resp.TrackedBranches[0].PR != nil ||
		resp.TrackedBranches[2].Source != "stack" || resp.TrackedBranches[2].PR == nil || resp.TrackedBranches[2].PR.PRNumber != 12 {
		t.Fatalf("tracked_branches = %+v", resp.TrackedBranches)
	}
	if resp.PRMeta == nil || resp.PRMeta.PRNumber != 11 {
		t.Fatalf("pr_meta = %+v", resp.PRMeta)
	}
}
