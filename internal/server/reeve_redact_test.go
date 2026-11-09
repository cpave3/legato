package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// reeveToken starts a session and returns its API token.
func reeveToken(t *testing.T, srv *Server, db *store.Store) string {
	t.Helper()
	rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/sessions", `{}`)
	var started store.ReeveSession
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	row, err := db.GetReeveSession(context.Background(), started.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	return row.APIToken
}

// A change set's approval keeps what Legato read from the host (an edited
// file's old contents, config.yaml's diff, a setting's value before) from
// the Reeve, which reads approvals with its own token; the person sees all.
func TestTheReeveReadsItsChangeSetsWithoutWhatItReadFromTheHost(t *testing.T) {
	srv, db := reeveServer(t)
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	srv.SetApprovalService(approvals)
	token := reeveToken(t, srv, db)
	details := service.ChangeSetDetails{ID: "cs-1", Reason: "edit a file", Calls: []service.ProposedCall{
		{ChangeCall: service.ChangeCall{Kind: "edit_file", File: "/home/me/auth-token"}, Diff: "- SECRET-ORIGINAL\n"},
		{ChangeCall: service.ChangeCall{Kind: "api", Method: "PUT", Path: "/api/config/settings/jira.url"}, Setting: "jira.url", Before: "SECRET-BEFORE", After: "x"},
	}}
	approval, _, err := approvals.Submit(context.Background(), service.ApprovalInput{Source: service.ApprovalSourceReeveChangeSet, SourceKey: "cs-1", Title: "edit a file", Details: details})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/approvals", "/api/approvals/" + approval.ID} {
		rec := callAs(srv, token, http.MethodGet, path, "")
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "SECRET-") || !strings.Contains(rec.Body.String(), "auth-token") {
			t.Fatalf("the Reeve read %s: %d %s", path, rec.Code, rec.Body)
		}
		if rec := callAs(srv, "secret", http.MethodGet, path, ""); !strings.Contains(rec.Body.String(), "SECRET-ORIGINAL") || !strings.Contains(rec.Body.String(), "SECRET-BEFORE") {
			t.Fatalf("the person's view of %s lost the diff: %s", path, rec.Body)
		}
	}
}

// The pairing phrase authenticates the exchange that hands out the web
// token, so the Reeve never reads an open request nor opens one.
func TestTheReeveNeverReadsOrOpensAPairingRequest(t *testing.T) {
	srv, db := reeveServer(t)
	srv.SetPairingService(service.NewPairingService(func(context.Context, service.PairingClient) (service.PairingSecret, error) {
		return service.PairingSecret{}, nil
	}))
	if rec := callAs(srv, "secret", http.MethodPost, "/api/pairing", `{}`); rec.Code/100 != 2 {
		t.Fatalf("the person opens a request: %d %s", rec.Code, rec.Body)
	}
	token := reeveToken(t, srv, db)
	for _, call := range [][2]string{{http.MethodGet, "/api/pairing"}, {http.MethodPost, "/api/pairing"}} {
		if rec := callAs(srv, token, call[0], call[1], `{}`); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"never"`) {
			t.Fatalf("%s %s by the Reeve: %d %s", call[0], call[1], rec.Code, rec.Body)
		}
	}
	// Nor can a change set call them.
	for _, path := range []string{"/api/pairing", "/api/pair/exchange", "/api/pair/confirm"} {
		if class := ClassifyRequest(httptest.NewRequest(http.MethodPost, path, nil)).Class; class != ClassNever {
			t.Fatalf("POST %s is %s", path, class)
		}
	}
	if rec := callAs(srv, "secret", http.MethodGet, "/api/pairing", ""); !strings.Contains(rec.Body.String(), "phrase") {
		t.Fatalf("the person's view lost the phrase: %s", rec.Body)
	}
}
