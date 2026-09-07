package service

import (
	"context"
	"strings"
	"testing"
)

func TestExportHandoffSelectedContext(t *testing.T) {
	f, input := readyHandoffFixture(t)
	gitRun(t, f.repo, "remote", "add", "origin", "https://github.com/example/questions.git")
	input.Concepts[0].SourceIDs = nil
	input.Evidence[0].SourceIDs = []string{"unchanged"}
	other := input.Concepts[0]
	other.ID, other.Title, other.Claim = "delivery", "Private delivery concept", "Unselected delivery claim"
	other.SourceIDs, other.EvidenceIDs = []string{"entry"}, []string{"private"}
	input.Concepts = append(input.Concepts, other)
	evidence := input.Evidence[0]
	evidence.ID, evidence.Description = "private", "Unselected delivery evidence"
	input.Evidence = append(input.Evidence, evidence)
	input.Scenarios = append(input.Scenarios, HandoffScenario{ID: "private", ConceptID: "delivery", Prompt: "Unselected delivery scenario", Explanation: "Private scenario explanation", SourceIDs: []string{"entry"}})
	p := submitHandoff(t, f, input)
	out, err := f.svc.ExportHandoff(context.Background(), f.tourID, HandoffExportInput{PacketID: p.ID, Format: "maintenance", ConceptIDs: []string{"persist"}, IncludeSources: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<details>", "repo:github.com/example/questions", "https://github.com/example/questions/blob/" + input.BaseSHA + "/README.md#L1-L1", "https://github.com/example/questions/blob/" + input.HeadSHA + "/entry.go#L1-L2", input.Evidence[0].RecordedAt, input.Scenarios[0].Explanation, "hello", "package entry"} {
		if !strings.Contains(out.Markdown, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, forbidden := range []string{other.Title, other.Claim, evidence.Description, "Unselected delivery scenario", "#diff-"} {
		if strings.Contains(out.Markdown, forbidden) {
			t.Errorf("leaked %q", forbidden)
		}
	}
	input.ExpectedRevision = 1
	input.Concepts[0].EvidenceIDs = nil
	input.Evidence = input.Evidence[1:]
	input.Scenarios = input.Scenarios[1:]
	p = submitHandoff(t, f, input)
	out, err = f.svc.ExportHandoff(context.Background(), f.tourID, HandoffExportInput{PacketID: p.ID, Format: "github", ConceptIDs: []string{"persist"}, IncludeSources: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Markdown, "package entry") || strings.Contains(out.Markdown, "hello") {
		t.Fatal("source-free selection exported unrelated excerpts")
	}
	if !strings.Contains(out.Markdown, "No evidence attached") || !strings.Contains(out.Markdown, "entry.go") {
		t.Fatal("missing evidence warning or full coverage")
	}
}

func TestExportHandoffSafeProseAndExactExcerpts(t *testing.T) {
	f, input := readyHandoffFixture(t)
	gitRun(t, f.repo, "remote", "add", "origin", "https://github.com/example/questions.git")
	path := "notes [draft] #1.md"
	text := "Fence example: `````\n</details><img src=\"https://evil.example/source\">\nexact\tbytes\u202e stay"
	writeRepoFile(t, f.repo, path, text+"\n")
	gitCommitAll(t, f.repo, "Add source example")
	if err := f.svc.Ready(context.Background(), f.tourID, "Source example"); err != nil {
		t.Fatal(err)
	}
	tour, err := f.svc.Tour(context.Background(), f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	input.HeadSHA = tour.Tour.HeadSHA
	input.Sources = append(input.Sources, HandoffSourceInput{ID: "example", Path: path, Side: "head", LineStart: 1, LineEnd: 3})
	input.Concepts[0].SourceIDs = []string{"example"}
	input.Overview.Before = strings.Repeat("Earlier questions were not retained. ", 80)
	input.Concepts[0].Claim = "Keep <details>closed</details> ![tracking](https://evil.example/pixel) [unsafe](javascript:alert(1)) visible\u202ehere &lt;img&gt;"
	input.Evidence[0].Outcome = "failed"
	input.Changes = "Retain the author explanation of source examples."
	p := submitHandoff(t, f, input)
	_, err = f.svc.SaveHandoffNote(context.Background(), f.tourID, p.ID, HandoffNote{Body: "private readiness note"})
	if err != nil {
		t.Fatal(err)
	}
	export := func(sources, notes bool) string {
		t.Helper()
		out, err := f.svc.ExportHandoff(context.Background(), f.tourID, HandoffExportInput{PacketID: p.ID, Format: "github", IncludeSources: sources, IncludeNotes: notes})
		if err != nil {
			t.Fatal(err)
		}
		return out.Markdown
	}
	brief := export(false, false)
	if n := len(strings.Fields(strings.SplitN(brief, "<details>", 2)[0])); n > 300 {
		t.Fatalf("first screen has %d words", n)
	}
	for _, want := range []string{"notes%20%5Bdraft%5D%20%231.md", "FAILED agent report", "STALE agent report", "visiblehere", input.Changes, "&lt;details&gt;", `\!\[tracking\]`, `\[unsafe\]`} {
		if !strings.Contains(brief, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, forbidden := range []string{"<img", "\u202e", "private readiness note", text} {
		if strings.Contains(brief, forbidden) {
			t.Errorf("unsafe/default leak %q", forbidden)
		}
	}
	full := export(true, true)
	if !strings.Contains(full, "``````text\n"+text+"\n``````") {
		t.Fatal("source excerpt bytes or enclosing fence changed")
	}
	if !strings.Contains(full, "private readiness note") {
		t.Fatal("explicit notes omitted")
	}
	for _, source := range p.Sources {
		if source.ID == "example" && !strings.Contains(full, source.Digest) {
			t.Fatal("captured digest omitted")
		}
	}
}

func TestExportHandoffKeepsPacketLevelFailuresAndInitialNonGoals(t *testing.T) {
	f, input := readyHandoffFixture(t)
	evidence := input.Evidence[0]
	evidence.ID = "packet-check"
	evidence.Description = "Unlinked failed check"
	evidence.Outcome = "failed"
	input.Evidence = append(input.Evidence, evidence)
	packet := submitHandoff(t, f, input)
	result, err := f.svc.ExportHandoff(context.Background(), f.tourID, HandoffExportInput{PacketID: packet.ID, Format: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Markdown, evidence.Description) {
		t.Fatal("export omitted packet-level failure")
	}
	opening := strings.SplitN(result.Markdown, "<details>", 2)[0]
	if !strings.Contains(opening, input.Overview.NonGoals) {
		t.Fatal("opening omitted non-goals")
	}
}
