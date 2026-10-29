package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
)

type fakeCredentials struct {
	result   service.AccessResult
	requests []service.AccessRequest
}

func (f *fakeCredentials) Request(_ context.Context, req service.AccessRequest) (service.AccessResult, error) {
	f.requests = append(f.requests, req)
	return f.result, nil
}

func (f *fakeCredentials) Credentials() []service.CredentialSummary {
	return []service.CredentialSummary{{Name: "eleven", Profile: "secret"}}
}

func TestRequestAccessIsOfferedOnlyWithCredentials(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	for _, with := range []bool{false, true} {
		opts := cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID}
		if with {
			opts.Credentials = &fakeCredentials{}
		}
		listed, err := connectMCP(t, opts).ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, tool := range listed.Tools {
			if tool.Name == "request_access" {
				found = true
				if !strings.Contains(tool.Description, "eleven") {
					t.Errorf("the description does not name the credentials: %s", tool.Description)
				}
			}
		}
		if found != with {
			t.Fatalf("with credentials %v: request_access offered %v", with, found)
		}
	}
}

func TestRequestAccessSaysWhatToDoForEachOutcome(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	for status, want := range map[string][]string{
		service.AccessGranted: {"Granted", "legato credential exec eleven"},
		service.AccessPending: {"[legato approvals] ap-12345678", "continue with other work", "end your turn"},
		service.AccessDenied:  {"Denied", "use the free voice", "another way"},
	} {
		creds := &fakeCredentials{result: service.AccessResult{Status: status, ApprovalID: "ap-12345678", Reason: "use the free voice", Grant: "this session"}}
		session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, Credentials: creds})

		result := callMCP(t, session, "request_access", map[string]any{
			"credential": "eleven", "operations": []string{"read"}, "reason": "narrate the video", "blocking": true,
		})

		text := mcpText(result)
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: %q does not say %q", status, text, w)
			}
		}
		req := creds.requests[0]
		if req.TaskID != "task1" || req.ExecutionID != executionID || req.Credential != "eleven" || req.Reason != "narrate the video" || !req.Blocking {
			t.Errorf("%s: request = %+v", status, req)
		}
	}
}
