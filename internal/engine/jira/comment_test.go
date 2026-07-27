package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProviderAddTaskNotePostsJiraComment(t *testing.T) {
	var gotPath string
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"10001"}`))
	}))
	defer server.Close()
	provider := NewProvider(server.URL, "me@example.com", "token", time.Second)
	if err := provider.AddTaskNote(context.Background(), "LEG-42", "## Triage\n\nBlocked by logs."); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/rest/api/3/issue/LEG-42/comment" {
		t.Fatalf("path = %q", gotPath)
	}
	body := got["body"].(map[string]any)
	if body["type"] != "doc" || body["version"] != float64(1) {
		t.Fatalf("body = %#v", body)
	}
	encoded, _ := json.Marshal(body)
	if string(encoded) == "" || !containsJSONText(body, "## Triage\n\nBlocked by logs.") {
		t.Fatalf("body = %s", encoded)
	}
}

func containsJSONText(value any, want string) bool {
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if containsJSONText(child, want) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsJSONText(child, want) {
				return true
			}
		}
	case string:
		return v == want
	}
	return false
}
