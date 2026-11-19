package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestFetchPRByNumberKeepsEveryReviewAcrossPages(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/widget/pulls/7":
			_, _ = w.Write([]byte(`{"number":7,"state":"open","head":{"ref":"x"}}`))
		case "/repos/acme/widget/pulls/7/reviews":
			pages++
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			var reviews []map[string]any
			count := 100
			if page == 2 {
				count = 3
			}
			for i := 0; i < count; i++ {
				id := (page-1)*100 + i + 1
				state := "COMMENTED"
				if id == 102 {
					state = "CHANGES_REQUESTED"
				}
				reviews = append(reviews, map[string]any{"id": id, "state": state, "submitted_at": fmt.Sprintf("2026-07-01T00:%02d:00Z", id%60), "user": map[string]any{"login": "rev", "type": "User"}})
			}
			_ = json.NewEncoder(w).Encode(reviews)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := New(Options{Token: "t", BaseURL: server.URL, HTTPClient: server.Client()})
	status, err := client.FetchPRByNumber("acme", "widget", 7)
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 || len(status.Reviews) != 103 {
		t.Fatalf("pages %d, reviews %d", pages, len(status.Reviews))
	}
	r := status.Reviews[101]
	if r.ID != 102 || r.State != "CHANGES_REQUESTED" || r.Login != "rev" || status.ReviewDecision != "CHANGES_REQUESTED" {
		t.Fatalf("review = %+v, decision %q", r, status.ReviewDecision)
	}
}
