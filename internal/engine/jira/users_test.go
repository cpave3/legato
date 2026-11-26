package jira

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// Myself is the user whose token Legato uses: ticket.update's assignee "me"
// (LEG-656).
func TestMyselfReadsTheTokensUser(t *testing.T) {
	srv, seen := jiraServer(t, http.StatusOK, `{"accountId":"5b10ac8d","displayName":"Cameron","emailAddress":"cam@example.com","active":true}`)
	c := NewClient(srv.URL, "cam@example.com", "secret-token", 5*time.Second)
	me, err := c.Myself(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me != (UserField{AccountID: "5b10ac8d", DisplayName: "Cameron", EmailAddress: "cam@example.com"}) {
		t.Fatalf("me = %+v", me)
	}
	if r := (*seen)[0]; r.method != http.MethodGet || r.path != "/rest/api/3/myself" {
		t.Fatalf("request = %+v", r)
	}
}

func TestFindUsersSearchesByQuery(t *testing.T) {
	srv, seen := jiraServer(t, http.StatusOK, `[{"accountId":"a1","displayName":"Ann","emailAddress":"ann@example.com"},{"accountId":"a2","displayName":"Annie"}]`)
	c := NewClient(srv.URL, "cam@example.com", "secret-token", 5*time.Second)
	users, err := c.FindUsers(context.Background(), "ann+x@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].AccountID != "a1" || users[0].EmailAddress != "ann@example.com" || users[1].DisplayName != "Annie" {
		t.Fatalf("users = %+v", users)
	}
	if r := (*seen)[0]; r.method != http.MethodGet || r.path != "/rest/api/3/user/search?query=ann%2Bx%40example.com" {
		t.Fatalf("request = %+v", r)
	}
}

func TestAssignPutsTheAccountID(t *testing.T) {
	srv, seen := jiraServer(t, http.StatusNoContent, ``)
	c := NewClient(srv.URL, "cam@example.com", "secret-token", 5*time.Second)
	if err := c.Assign(context.Background(), "AG-12", "5b10ac8d"); err != nil {
		t.Fatal(err)
	}
	r := (*seen)[0]
	if r.method != http.MethodPut || r.path != "/rest/api/3/issue/AG-12/assignee" || r.body["accountId"] != "5b10ac8d" {
		t.Fatalf("request = %+v", r)
	}
}

// Unassigning sends a null account ID, never "-1", which is the project's
// default assignee.
func TestAssignWithNoAccountUnassigns(t *testing.T) {
	srv, seen := jiraServer(t, http.StatusNoContent, ``)
	c := NewClient(srv.URL, "cam@example.com", "secret-token", 5*time.Second)
	if err := c.Assign(context.Background(), "AG-12", ""); err != nil {
		t.Fatal(err)
	}
	r := (*seen)[0]
	id, present := r.body["accountId"]
	if r.method != http.MethodPut || !present || id != nil {
		t.Fatalf("request = %+v", r)
	}
}

func TestAssignRefusedIsAnAPIError(t *testing.T) {
	srv, _ := jiraServer(t, http.StatusBadRequest, `{"errorMessages":["User 'x' cannot be assigned issues."],"errors":{}}`)
	c := NewClient(srv.URL, "cam@example.com", "secret-token", 5*time.Second)
	err := c.Assign(context.Background(), "AG-12", "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || apiErr.Messages[0] != "User 'x' cannot be assigned issues." {
		t.Fatalf("err = %v", err)
	}
}

func TestAnIssuesAssigneeCarriesItsAccountID(t *testing.T) {
	var issue Issue
	if err := json.Unmarshal([]byte(`{"key":"AG-1","fields":{"assignee":{"accountId":"a1","displayName":"Ann"}}}`), &issue); err != nil {
		t.Fatal(err)
	}
	if issue.Fields.Assignee == nil || issue.Fields.Assignee.AccountID != "a1" {
		t.Fatalf("assignee = %+v", issue.Fields.Assignee)
	}
}

func TestCreateIssueAssignsAsItCreates(t *testing.T) {
	srv, seen := jiraServer(t, http.StatusCreated, `{"id":"1","key":"AG-12"}`)
	c := NewClient(srv.URL, "cam@example.com", "secret-token", 5*time.Second)
	if _, err := c.CreateIssue(context.Background(), IssueDraft{Project: "AG", Type: "Bug", Summary: "S", AssigneeID: "5b10ac8d"}); err != nil {
		t.Fatal(err)
	}
	if got := path((*seen)[0].body, "fields", "assignee", "accountId"); got != "5b10ac8d" {
		t.Fatalf("fields.assignee.accountId = %v", got)
	}
}
