package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

const reviewFile = `# Review every pull request.
schema_version: 1
name: review
title: Review
steps:
  - id: run
    title: Run
    type: agent.run   # the reviewer
    with: {prompt: review}
`

type filesFixture struct {
	s               *Server
	global, project string
	db              *store.Store
}

func newFilesFixture(t *testing.T) *filesFixture {
	t.Helper()
	s, global, project := templateServer(t)
	s.builtInWorkflows = []*workflowengine.Definition{{
		SchemaVersion: 1, Name: "legato-job", Title: "Agent job", Source: "builtin:legato-job",
		Steps: []workflowengine.Step{{ID: "agent", Title: "Run agent", Type: "agent.run", Trigger: "automatic", With: map[string]any{"prompt": "hi"}}},
	}}
	return &filesFixture{s: s, global: global, project: project}
}

// withSwitches gives the server a trigger coordinator over its folders.
func (f *filesFixture) withSwitches(t *testing.T) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f.db = db
	f.s.SetWorkflowTriggerCoordinator(service.NewWorkflowTriggerCoordinator(db, service.NewWorkflowService(db, nil), f.s.discoverWorkflows))
}

func (f *filesFixture) write(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f *filesFixture) call(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = strings.NewReader(string(data))
	} else {
		reader = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, reader))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestReadingAWorkflowsFileGivesItsBytes(t *testing.T) {
	f := newFilesFixture(t)
	path := f.write(t, f.global, "review.yaml", reviewFile)
	code, body := f.call(t, http.MethodGet, "/api/workflows/review/file", nil)
	if code != http.StatusOK || body["yaml"] != reviewFile || body["path"] != path || body["folder"] != "global" || body["editable"] != true {
		t.Fatalf("%d %v", code, body)
	}
	if body["digest"] != workflowengine.FileDigest([]byte(reviewFile)) {
		t.Fatalf("digest = %v", body["digest"])
	}
	code, body = f.call(t, http.MethodGet, "/api/workflows/legato-job/file", nil)
	if code != http.StatusOK || body["editable"] != false || body["reason"] == "" || !strings.Contains(body["yaml"].(string), "name: legato-job") {
		t.Fatalf("built-in: %d %v", code, body)
	}
	if code, _ := f.call(t, http.MethodGet, "/api/workflows/nope/file", nil); code != http.StatusNotFound {
		t.Fatalf("unknown: %d", code)
	}
}

func TestCheckingAWorkflowWritesNothing(t *testing.T) {
	f := newFilesFixture(t)
	code, body := f.call(t, http.MethodPost, "/api/workflow-files/check", map[string]string{"yaml": reviewFile})
	if code != http.StatusOK || body["definition"] == nil || body["graph"] == nil || len(body["diagnostics"].([]any)) != 0 {
		t.Fatalf("%d %v", code, body)
	}
	code, body = f.call(t, http.MethodPost, "/api/workflow-files/check", map[string]string{"yaml": strings.Replace(reviewFile, "agent.run", "agent.nope", 1)})
	diagnostics, _ := body["diagnostics"].([]any)
	if code != http.StatusOK || body["definition"] != nil || len(diagnostics) != 1 || diagnostics[0].(map[string]any)["line"] != float64(8) {
		t.Fatalf("%d %v", code, body)
	}
	if entries, _ := os.ReadDir(f.global); len(entries) != 0 {
		t.Fatalf("check wrote %v", entries)
	}
}

func TestCreatingAWorkflowFile(t *testing.T) {
	f := newFilesFixture(t)
	code, body := f.call(t, http.MethodPost, "/api/workflow-files", map[string]string{"yaml": reviewFile, "folder": "project"})
	if code != http.StatusCreated || body["name"] != "review" || body["folder"] != "project" {
		t.Fatalf("%d %v", code, body)
	}
	if got, _ := os.ReadFile(filepath.Join(f.project, "review.workflow.yaml")); string(got) != reviewFile {
		t.Fatalf("file = %q", got)
	}
	// Taken at any precedence, built-ins included.
	for _, name := range []string{"review", "legato-job"} {
		code, body = f.call(t, http.MethodPost, "/api/workflow-files", map[string]string{"yaml": strings.Replace(reviewFile, "name: review", "name: "+name, 1), "folder": "global"})
		if code != http.StatusConflict || body["code"] != "name_taken" {
			t.Fatalf("%s: %d %v", name, code, body)
		}
	}
	code, body = f.call(t, http.MethodPost, "/api/workflow-files", map[string]string{"yaml": "schema_version: 1\nname: x\n", "folder": "global"})
	if code != http.StatusUnprocessableEntity || body["code"] != "invalid" || len(body["diagnostics"].([]any)) == 0 {
		t.Fatalf("invalid: %d %v", code, body)
	}
	code, body = f.call(t, http.MethodPost, "/api/workflow-files", map[string]string{"yaml": strings.Replace(reviewFile, "name: review", "name: other", 1), "folder": "legacy"})
	if code != http.StatusBadRequest || body["code"] != "folder" {
		t.Fatalf("legacy folder: %d %v", code, body)
	}
	code, body = f.call(t, http.MethodPost, "/api/workflow-files", map[string]string{"yaml": strings.Replace(reviewFile, "name: review", "name: ../../escape", 1), "folder": "global"})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("a name with a path: %d %v", code, body)
	}
}

const scheduled = "on:\n  schedule: {cron: \"0 9 * * *\"}\n"

func TestASaveThatStartsATriggerAsksFirst(t *testing.T) {
	f := newFilesFixture(t)
	f.withSwitches(t)
	withTrigger := strings.Replace(reviewFile, "steps:\n", scheduled+"steps:\n", 1)
	code, body := f.call(t, http.MethodPost, "/api/workflow-files", map[string]any{"yaml": withTrigger, "folder": "global"})
	if code != http.StatusConflict || body["code"] != "confirm_triggers" || body["triggers"].([]any)[0] != "schedule" {
		t.Fatalf("%d %v", code, body)
	}
	paused := strings.Replace(withTrigger, "\"0 9 * * *\"}", "\"0 9 * * *\", enabled: false}", 1)
	if code, body := f.call(t, http.MethodPost, "/api/workflow-files", map[string]any{"yaml": paused, "folder": "global"}); code != http.StatusCreated {
		t.Fatalf("paused: %d %v", code, body)
	}
	_, file := f.call(t, http.MethodGet, "/api/workflows/review/file", nil)
	// Switching it on in the file needs confirming; paused here, it does not.
	code, body = f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": withTrigger, "digest": file["digest"]})
	if code != http.StatusConflict || body["code"] != "confirm_triggers" {
		t.Fatalf("switched on: %d %v", code, body)
	}
	if err := f.db.SetWorkflowTriggerSwitch(context.Background(), "review", "schedule", false); err != nil {
		t.Fatal(err)
	}
	if code, body := f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": withTrigger, "digest": file["digest"]}); code != http.StatusOK {
		t.Fatalf("paused here: %d %v", code, body)
	}
	_, file = f.call(t, http.MethodGet, "/api/workflows/review/file", nil)
	other := strings.Replace(withTrigger, scheduled, scheduled+"  task.entered_column: {column: Doing}\n", 1)
	if code, _ := f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": other, "digest": file["digest"]}); code != http.StatusConflict {
		t.Fatalf("a new trigger: %d", code)
	}
	if code, body := f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": other, "digest": file["digest"], "confirm_triggers": true}); code != http.StatusOK {
		t.Fatalf("confirmed: %d %v", code, body)
	}
}

func TestSavingAWorkflowFile(t *testing.T) {
	f := newFilesFixture(t)
	path := f.write(t, f.global, "review.yaml", reviewFile)
	_, file := f.call(t, http.MethodGet, "/api/workflows/review/file", nil)
	edited := strings.Replace(reviewFile, "title: Review\n", "title: Review it\n# a new comment\n", 1)
	code, body := f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": edited, "digest": file["digest"]})
	if code != http.StatusOK || body["digest"] != workflowengine.FileDigest([]byte(edited)) {
		t.Fatalf("%d %v", code, body)
	}
	if got, _ := os.ReadFile(path); string(got) != edited {
		t.Fatalf("file = %q, want it byte for byte", got)
	}
	// The old digest is stale now: the save is refused with the file's text.
	code, body = f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": reviewFile, "digest": file["digest"]})
	if code != http.StatusConflict || body["code"] != "stale" || body["yaml"] != edited {
		t.Fatalf("stale: %d %v", code, body)
	}
	code, body = f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": strings.Replace(edited, "name: review", "name: renamed", 1), "digest": workflowengine.FileDigest([]byte(edited))})
	if code != http.StatusUnprocessableEntity || body["code"] != "rename" {
		t.Fatalf("rename: %d %v", code, body)
	}
	code, body = f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": "name: [", "digest": workflowengine.FileDigest([]byte(edited))})
	if code != http.StatusUnprocessableEntity || body["code"] != "invalid" {
		t.Fatalf("invalid: %d %v", code, body)
	}
	if code, body := f.call(t, http.MethodPut, "/api/workflows/legato-job/file", map[string]any{"yaml": reviewFile, "digest": ""}); code != http.StatusForbidden || body["code"] != "not_editable" {
		t.Fatalf("built-in: %d %v", code, body)
	}
}

func TestAWorkflowLinkedFromOutsideItsFolderIsReadOnly(t *testing.T) {
	f := newFilesFixture(t)
	outside := f.write(t, t.TempDir(), "review.yaml", reviewFile)
	if err := os.MkdirAll(f.global, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.global, "review.yaml")); err != nil {
		t.Fatal(err)
	}
	_, file := f.call(t, http.MethodGet, "/api/workflows/review/file", nil)
	if file["editable"] != false {
		t.Fatalf("file = %v", file)
	}
	code, body := f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": reviewFile, "digest": file["digest"]})
	if code != http.StatusForbidden || body["code"] != "not_editable" {
		t.Fatalf("%d %v", code, body)
	}
	if got, _ := os.ReadFile(outside); string(got) != reviewFile {
		t.Fatal("the file outside was written")
	}
}

func TestDeletingAWorkflowFile(t *testing.T) {
	f := newFilesFixture(t)
	path := f.write(t, f.global, "review.yaml", strings.Replace(reviewFile, "steps:\n", scheduled+"steps:\n", 1))
	f.withSwitches(t)
	ctx := context.Background()
	if err := f.db.SetWorkflowTriggerSwitch(ctx, "review", "schedule", false); err != nil {
		t.Fatal(err)
	}
	if code, body := f.call(t, http.MethodDelete, "/api/workflows/review/file", map[string]any{"digest": "sha256:old"}); code != http.StatusConflict || body["code"] != "stale" {
		t.Fatalf("stale: %d %v", code, body)
	}
	_, file := f.call(t, http.MethodGet, "/api/workflows/review/file", nil)
	if code, body := f.call(t, http.MethodDelete, "/api/workflows/review/file", map[string]any{"digest": file["digest"]}); code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the file is still there: %v", err)
	}
	if switches, _ := f.db.WorkflowTriggerSwitches(ctx); len(switches) != 0 {
		t.Fatalf("switches = %+v", switches)
	}
	if code, _ := f.call(t, http.MethodDelete, "/api/workflows/legato-job/file", map[string]any{"digest": ""}); code != http.StatusForbidden {
		t.Fatalf("built-in: %d", code)
	}
}

func TestCopyingABuiltInWorkflow(t *testing.T) {
	f := newFilesFixture(t)
	code, body := f.call(t, http.MethodPost, "/api/workflows/legato-job/copy", map[string]string{"folder": "global"})
	if code != http.StatusCreated {
		t.Fatalf("%d %v", code, body)
	}
	_, file := f.call(t, http.MethodGet, "/api/workflows/legato-job/file", nil)
	if file["editable"] != true || file["folder"] != "global" {
		t.Fatalf("the copy = %v", file)
	}
	if code, body := f.call(t, http.MethodPost, "/api/workflows/legato-job/copy", map[string]string{"folder": "global"}); code != http.StatusBadRequest || body["code"] != "not_builtin" {
		t.Fatalf("copying the copy: %d %v", code, body)
	}
}

func TestTheWorkflowListSaysWhereEachFileIs(t *testing.T) {
	f := newFilesFixture(t)
	repo := t.TempDir()
	f.s.workflowRepositories = []workflowengine.RepositoryPreset{{Name: "api", PrimaryDir: repo}}
	f.write(t, f.global, "review.yaml", reviewFile)
	shadowing := f.write(t, filepath.Join(repo, ".legato", "workflows"), "review.yaml", strings.Replace(reviewFile, "with: {prompt: review}", "with: {prompt: review, promt: x}", 1))
	code, body := f.call(t, http.MethodGet, "/api/workflows", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	var review, job map[string]any
	for _, d := range body["definitions"].([]any) {
		definition := d.(map[string]any)
		switch definition["name"] {
		case "review":
			review = definition
		case "legato-job":
			job = definition
		}
	}
	if review["source"] != shadowing || review["folder"] != "repo:api" || review["editable"] != true {
		t.Fatalf("review = %v", review)
	}
	if shadows := review["shadows"].([]any); len(shadows) != 1 || shadows[0] != filepath.Join(f.global, "review.yaml") {
		t.Fatalf("shadows = %v", review["shadows"])
	}
	if warnings := review["warnings"].([]any); len(warnings) != 1 || warnings[0].(map[string]any)["path"] != "steps[0].with.promt" {
		t.Fatalf("warnings = %v", review["warnings"])
	}
	if job["editable"] != false || job["read_only_reason"] == "" {
		t.Fatalf("built-in = %v", job)
	}
}

func TestARunKeepsItsDefinitionWhenItsFileIsSaved(t *testing.T) {
	s := serverWithRun(t, reviewOneStep, runWithCopy(t, reviewOneStep))
	f := &filesFixture{s: s}
	_, file := f.call(t, http.MethodGet, "/api/workflows/review/file", nil)
	if code, body := f.call(t, http.MethodPut, "/api/workflows/review/file", map[string]any{"yaml": reviewTwoSteps, "digest": file["digest"]}); code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	body := getRun(t, s)
	if ids := graphNodeIDs(body); len(ids) != 2 || body["definition_changed"] != true {
		t.Fatalf("nodes = %v, changed = %v", ids, body["definition_changed"])
	}
}

func TestCheckingAScheduleGivesItsNextTimes(t *testing.T) {
	f := newFilesFixture(t)
	withSchedule := strings.Replace(reviewFile, "steps:\n", "on:\n  schedule: {every: weekdays, at: \"09:00\", timezone: Europe/London}\nsteps:\n", 1)
	code, body := f.call(t, http.MethodPost, "/api/workflow-files/check", map[string]string{"yaml": withSchedule})
	schedule, _ := body["schedule"].(map[string]any)
	next, _ := schedule["next"].([]any)
	if code != http.StatusOK || schedule["description"] != "Every weekday at 09:00 (Europe/London)" || schedule["timezone"] != "Europe/London" || len(next) != 3 {
		t.Fatalf("%d %v", code, body)
	}
	for _, value := range next {
		at, err := time.Parse(time.RFC3339, value.(string))
		if err != nil || !at.After(time.Now()) || at.Weekday() == time.Saturday || at.Weekday() == time.Sunday {
			t.Fatalf("next = %v", next)
		}
	}
	if _, body := f.call(t, http.MethodPost, "/api/workflow-files/check", map[string]string{"yaml": reviewFile}); body["schedule"] != nil {
		t.Fatalf("a workflow with no schedule: %v", body["schedule"])
	}
}
