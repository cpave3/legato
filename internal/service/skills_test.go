package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func writeLegatoSkill(t *testing.T, dir, name, description string) string {
	t.Helper()
	folder := filepath.Join(dir, name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
	if description == "" {
		content = "# no frontmatter\n"
	}
	if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return folder
}

func newSkillService(t *testing.T) (*SkillService, string) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	dir := filepath.Join(t.TempDir(), "skills")
	return NewSkillService(dir, s), dir
}

func TestASkillFoundOnDiskIsOnUntilTurnedOff(t *testing.T) {
	svc, dir := newSkillService(t)
	ctx := context.Background()
	writeLegatoSkill(t, dir, "tdd", "Tests first.")
	writeLegatoSkill(t, dir, "broken", "")

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "broken" || list[1].Name != "tdd" {
		t.Fatalf("skills = %+v", list)
	}
	if !list[1].Enabled || list[1].Description != "Tests first." || list[1].Path != filepath.Join(dir, "tdd") {
		t.Fatalf("tdd = %+v, want it on with its description and path", list[1])
	}
	if list[0].Err == "" {
		t.Fatalf("broken = %+v, want its error", list[0])
	}

	if err := svc.SetEnabled(ctx, "tdd", false); err != nil {
		t.Fatal(err)
	}
	list, _ = svc.List(ctx)
	if list[1].Enabled {
		t.Fatalf("tdd = %+v after turning it off", list[1])
	}
}

func TestTurningOnASkillThatIsNotThereIsRefused(t *testing.T) {
	svc, dir := newSkillService(t)
	writeLegatoSkill(t, dir, "tdd", "Tests first.")
	for _, name := range []string{"missing", "../tdd", ""} {
		if err := svc.SetEnabled(context.Background(), name, true); !errors.Is(err, ErrSkillNotFound) {
			t.Errorf("SetEnabled(%q) = %v, want ErrSkillNotFound", name, err)
		}
	}
}

func TestAgentsAreGivenTheSkillsThatAreOnAndUsable(t *testing.T) {
	svc, dir := newSkillService(t)
	ctx := context.Background()
	writeLegatoSkill(t, dir, "tdd", "Tests first.")
	writeLegatoSkill(t, dir, "deploy", "Ship it.")
	writeLegatoSkill(t, dir, "broken", "")
	if err := svc.SetEnabled(ctx, "deploy", false); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Capabilities(ctx, CapabilityRequest{AgentKind: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "tdd" {
		t.Fatalf("skills given = %+v, want tdd alone", got.Skills)
	}

	// Found again at each ask: one dropped in later is given too.
	writeLegatoSkill(t, dir, "later", "Added while running.")
	got, _ = svc.Capabilities(ctx, CapabilityRequest{AgentKind: "claude-code"})
	if len(got.Skills) != 2 {
		t.Fatalf("skills given after adding one = %+v", got.Skills)
	}
}

func TestUsableNamesTheSkillsAgentsAreGiven(t *testing.T) {
	svc, dir := newSkillService(t)
	ctx := context.Background()
	writeLegatoSkill(t, dir, "tdd", "Tests first.")
	writeLegatoSkill(t, dir, "deploy", "Ship it.")
	writeLegatoSkill(t, dir, "broken", "")
	if err := svc.SetEnabled(ctx, "deploy", false); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Usable(ctx); err != nil || len(got) != 1 || got[0] != "tdd" {
		t.Fatalf("usable = %v, %v; want tdd alone", got, err)
	}
}
