package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestSettingAWorkflowsTagsWritesThemIntoItsFile(t *testing.T) {
	f := newFilesFixture(t)
	path := f.write(t, f.global, "review.yaml", reviewFile)
	code, body := f.call(t, http.MethodPut, "/api/workflows/review/tags", map[string]any{"tags": []string{"autopilot", "ops"}})
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	got, _ := os.ReadFile(path)
	if want := strings.Replace(reviewFile, "title: Review\n", "title: Review\ntags: [autopilot, ops]\n", 1); string(got) != want {
		t.Fatalf("file =\n%s", got)
	}
	_, list := f.call(t, http.MethodGet, "/api/workflows", nil)
	for _, d := range list["definitions"].([]any) {
		if definition := d.(map[string]any); definition["name"] == "review" {
			if tags, _ := definition["tags"].([]any); len(tags) != 2 || tags[0] != "autopilot" {
				t.Fatalf("listed tags = %v", definition["tags"])
			}
		}
	}
}

func TestATagsChangeIsRefusedWhereTheFileCannotTakeIt(t *testing.T) {
	f := newFilesFixture(t)
	f.write(t, f.global, "review.yaml", reviewFile)
	if code, body := f.call(t, http.MethodPut, "/api/workflows/review/tags", map[string]any{"tags": []string{"Not A Tag"}}); code != http.StatusUnprocessableEntity || body["code"] != "invalid" {
		t.Fatalf("bad tag: %d %v", code, body)
	}
	if code, body := f.call(t, http.MethodPut, "/api/workflows/legato-job/tags", map[string]any{"tags": []string{"ops"}}); code != http.StatusForbidden || body["code"] != "not_editable" {
		t.Fatalf("built-in: %d %v", code, body)
	}
	if code, _ := f.call(t, http.MethodPut, "/api/workflows/nope/tags", map[string]any{"tags": []string{"ops"}}); code != http.StatusNotFound {
		t.Fatalf("unknown: %d", code)
	}
}

func TestTheWorkflowListSaysWhichWorkflowsRunEach(t *testing.T) {
	f := newFilesFixture(t)
	f.write(t, f.global, "review.yaml", reviewFile)
	f.write(t, f.global, "nightly.yaml", `schema_version: 2
name: nightly
title: Nightly
steps:
  - id: call
    title: Call
    type: workflow.run
    with: {workflow: review}
`)
	_, list := f.call(t, http.MethodGet, "/api/workflows", nil)
	for _, d := range list["definitions"].([]any) {
		definition := d.(map[string]any)
		switch definition["name"] {
		case "review":
			if callers, _ := definition["called_by"].([]any); len(callers) != 1 || callers[0] != "nightly" {
				t.Fatalf("review called_by = %v", definition["called_by"])
			}
		case "nightly":
			if _, has := definition["called_by"]; has {
				t.Fatalf("nightly called_by = %v, want none", definition["called_by"])
			}
		}
	}
}

func TestWorkflowStatsCountRunsByDayInTheAskedZone(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateWorkflowRun(t.Context(), store.WorkflowRun{ID: "r1", WorkflowName: "triage", WorkflowTitle: "Triage", DefinitionSource: "test", DefinitionDigest: "d", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "failed", CurrentStepID: "a"}, nil); err != nil {
		t.Fatal(err)
	}
	f := newFilesFixture(t)
	f.s.SetWorkflowService(service.NewWorkflowService(db, nil), "", "")
	code, body := f.call(t, http.MethodGet, "/api/workflow-stats?tz=Australia/Brisbane", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	brisbane, _ := time.LoadLocation("Australia/Brisbane")
	days := body["days"].([]any)
	if len(days) != 7 || days[6] != time.Now().In(brisbane).Format(time.DateOnly) {
		t.Fatalf("days = %v", days)
	}
	triage := body["workflows"].(map[string]any)["triage"].(map[string]any)
	today := triage["days"].([]any)[6].(map[string]any)
	if today["runs"] != 1.0 || today["failed"] != 1.0 || triage["last"].(map[string]any)["id"] != "r1" {
		t.Fatalf("triage = %v", triage)
	}
	if code, _ := f.call(t, http.MethodGet, "/api/workflow-stats?tz=Not/AZone", nil); code != http.StatusBadRequest {
		t.Fatalf("bad zone: %d", code)
	}
}
