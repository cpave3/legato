package main

import "testing"

// A call that is not a request at all is never made, and does not bring
// the process down.
func TestClassifyCallRefusesWhatDoesNotParse(t *testing.T) {
	if class, _ := classifyCall("DELETE", "/api/tasks/hello world"); class != "never" {
		t.Fatalf("a path with a space: %s", class)
	}
	if class, _ := classifyCall("GET", "/api/board"); class != "read" {
		t.Fatalf("GET /api/board: %s", class)
	}
}
