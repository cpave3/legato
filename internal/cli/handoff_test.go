package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/service"
)

func TestHandoffSubmitShowExport(t *testing.T) {
	svc, _, tourID := newReviewCLIFixture(t)
	if err := ReviewReady(svc, tourID, "Ready"); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Tour(context.Background(), tourID)
	if err != nil {
		t.Fatal(err)
	}
	input := service.HandoffInput{SchemaVersion: 1, PassID: view.Passes[len(view.Passes)-1].Pass.ID, BaseSHA: view.Tour.BaseSHA, HeadSHA: view.Tour.HeadSHA,
		Overview: service.HandoffOverview{Before: "No package", After: "Package a", Why: "Provide a package", NonGoals: "No runtime behavior", Decision: "Small package", Alternative: "No package", Risk: "No runtime checks", VerificationGaps: "No check result attached", ReviewQuestion: "Is the package needed?", Rollback: "Revert the addition"},
		Concepts: []service.HandoffConcept{{ID: "package", Title: "Package a", Claim: "Package declaration is present", Flow: []string{"a.go declares package a"}, Invariant: "Valid package declaration", FailureCase: "Invalid declaration", ChangeGuide: "Start in a.go", SourceIDs: []string{"source"}, Routes: []string{"owner", "reviewer", "maintainer", "colleague"}}},
		Sources:  []service.HandoffSourceInput{{ID: "source", Path: "a.go", Side: "head", LineStart: 1, LineEnd: 1}},
		Coverage: []service.HandoffCoverage{{Path: "a.go", Category: "behavior"}}}
	runtime, err := os.MkdirTemp("/tmp", "handoff-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtime) })
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	messages := make(chan ipc.Message, 1)
	server, err := ipc.NewServer(filepath.Join(runtime, "legato", "legato-test.sock"), func(msg ipc.Message) { messages <- msg })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	data, _ := json.Marshal(input)
	saved, err := ReviewHandoffSubmit(svc, tourID, strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-messages:
		if msg.Type != "review_changed" || msg.TourID != tourID || msg.Kind != "handoff_submitted" {
			t.Fatalf("event: %+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no submission event")
	}
	if saved.ID == "" || saved.Revision != 1 {
		t.Fatalf("packet: %+v", saved)
	}
	var out strings.Builder
	if err := ReviewHandoffShow(svc, tourID, true, &out); err != nil {
		t.Fatal(err)
	}
	var got service.HandoffView
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Packets) != 1 || got.Packets[0].Sources[0].Text != "package a" {
		t.Fatalf("show: %s", out.String())
	}
	out.Reset()
	if err := ReviewHandoffShow(svc, tourID, false, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{saved.ID, "Package a", "No evidence attached", view.Tour.HeadSHA} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	out.Reset()
	if err := ReviewHandoffExport(svc, tourID, service.HandoffExportInput{PacketID: saved.ID, Format: "github", ConceptIDs: []string{"package"}}, false, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Package a", view.Tour.HeadSHA} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("export missing %q: %s", want, out.String())
		}
	}
}

func TestHandoffTemplateExposesNestedAuthoringFields(t *testing.T) {
	svc, _, tourID := newReviewCLIFixture(t)
	if err := ReviewReady(svc, tourID, "Ready"); err != nil {
		t.Fatal(err)
	}
	var template strings.Builder
	if err := ReviewHandoffTemplate(svc, tourID, &template); err != nil {
		t.Fatal(err)
	}
	input, err := service.DecodeHandoff(strings.NewReader(template.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Evidence) != 1 || len(input.Scenarios) != 1 || len(input.Coverage) != 1 {
		t.Fatal("template must expose the nested contract")
	}
	input.Sources[0].Path = "a.go"
	input.Coverage[0].Path = "a.go"
	input.Evidence[0].Command = "go test ./..."
	input.Evidence[0].RecordedAt = "2026-09-07T12:00:00Z"
	input.Evidence[0].Outcome = "skipped"
	input.Evidence[0].Limitations = "Not run for this fixture."
	input.Concepts[0].Claim = "A package declaration exists, not evidence of runtime behavior."
	input.Scenarios[0].Prompt = "Where should implementation begin?"
	input.Scenarios[0].Explanation = "Inspect the declaration and trace real callers before implementation."
	raw, _ := json.Marshal(input)
	packet, err := ReviewHandoffSubmit(svc, tourID, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if packet.Input.Evidence[0].Command != input.Evidence[0].Command || packet.Input.Scenarios[0].Prompt != input.Scenarios[0].Prompt || packet.Input.Coverage[0].Path != "a.go" {
		t.Fatalf("template fields lost: %+v", packet.Input)
	}
}

func TestHandoffSubmitRejectsUntrustedJSON(t *testing.T) {
	svc, _, tourID := newReviewCLIFixture(t)
	for _, input := range []string{`{"note":{"readiness":"ready"}}`, `{}`, `{} {}`, strings.Repeat(" ", service.MaxHandoffBytes+1)} {
		if _, err := ReviewHandoffSubmit(svc, tourID, strings.NewReader(input)); err == nil {
			t.Fatal("accepted invalid packet")
		}
	}
	var out strings.Builder
	if err := ReviewHandoffShow(svc, tourID, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No handoff packet") {
		t.Fatal(out.String())
	}
}
