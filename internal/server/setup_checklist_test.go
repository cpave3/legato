package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeChecklist struct {
	dismissed bool
}

func (f *fakeChecklist) Status(context.Context) service.SetupStatus {
	return service.SetupStatus{Items: []service.SetupItem{{ID: "repository", Title: "Add a repository"}}, Done: 0, Total: 1, Dismissed: f.dismissed, Show: !f.dismissed}
}

func (f *fakeChecklist) SetDismissed(_ context.Context, dismissed bool) error {
	f.dismissed = dismissed
	return nil
}

func checklistServer(list *fakeChecklist) *Server {
	srv := New(&mockBoardService{}, nil, nil, ":0")
	srv.SetAuthToken("secret")
	srv.SetSetupChecklist(list)
	return srv
}

func checklistRequest(srv *Server, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestTheSetupChecklistNeedsAToken(t *testing.T) {
	srv := checklistServer(&fakeChecklist{})
	for _, r := range []struct{ method, path, body string }{{http.MethodGet, "/api/setup", ""}, {http.MethodPut, "/api/setup/dismissed", `{"dismissed":true}`}} {
		if rec := checklistRequest(srv, r.method, r.path, r.body, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", r.method, r.path, rec.Code)
		}
	}
}

func TestDismissingTheSetupChecklistIsKeptForEveryDevice(t *testing.T) {
	list := &fakeChecklist{}
	srv := checklistServer(list)
	rec := checklistRequest(srv, http.MethodPut, "/api/setup/dismissed", `{"dismissed":true}`, "secret")
	if rec.Code != http.StatusOK || !list.dismissed {
		t.Fatalf("dismiss: %d %s", rec.Code, rec.Body)
	}
	rec = checklistRequest(srv, http.MethodGet, "/api/setup", "", "secret")
	var st service.SetupStatus
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &st) != nil || !st.Dismissed || st.Show {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	settings := checklistRequest(srv, http.MethodGet, "/api/settings", "", "secret").Body.String()
	if !strings.Contains(settings, `"setup":{"show":false,"done":0,"total":1}`) {
		t.Fatalf("settings carry no setup summary: %s", settings)
	}
}
