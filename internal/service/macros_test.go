package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/macros"
)

func macroNames(list []MacroEntry) []string {
	var out []string
	for _, m := range list {
		out = append(out, m.Name)
	}
	return out
}

func findMacro(t *testing.T, list []MacroEntry, name string) MacroEntry {
	t.Helper()
	for _, m := range list {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no macro %q in %v", name, macroNames(list))
	return MacroEntry{}
}

func TestMacrosListBuiltInsThenTheFilesThenTheDatabases(t *testing.T) {
	settings, _ := newSettingsFixture(t, "macros:\n  - name: fromfile\n    keys: hello $HOME\n")
	ctx := context.Background()
	if err := settings.SetEntry(ctx, "macros", "fromdb", json.RawMessage(`{"keys":"db text","applies_to":"pr","agent":"codex","tier":"smart"}`), "web"); err != nil {
		t.Fatal(err)
	}
	svc := NewMacroService(settings)
	list, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Plan", "Implement", "Review", "Address feedback", "fromfile", "fromdb"}
	if got := macroNames(list); len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("names = %v, want %v", got, want)
			}
		}
	}
	plan := findMacro(t, list, "Plan")
	if !plan.Builtin || plan.Source != MacroBuiltin || plan.AppliesTo != macros.AppliesToTask || plan.Keys == "" || plan.Overridden {
		t.Fatalf("Plan = %+v", plan)
	}
	if review := findMacro(t, list, "Review"); review.AppliesTo != macros.AppliesToPR {
		t.Fatalf("Review applies to %q", review.AppliesTo)
	}
	file := findMacro(t, list, "fromfile")
	if !file.Pinned || file.Source != MacroFromFile || file.Keys != "hello $HOME" {
		t.Fatalf("file entry = %+v", file)
	}
	db := findMacro(t, list, "fromdb")
	if db.Pinned || db.Source != MacroFromDatabase || db.AppliesTo != "pr" || db.Agent != "codex" || db.Tier != "smart" {
		t.Fatalf("database entry = %+v", db)
	}
}

func TestAnEditedBuiltInShadowsItAndResetRestoresIt(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	svc := NewMacroService(settings)
	original, err := svc.Get(ctx, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(ctx, "Plan", macros.Macro{Keys: "my plan", AppliesTo: "task", Agent: "codex"}, "web"); err != nil {
		t.Fatal(err)
	}
	// Seen at the next use: no restart.
	got, err := svc.Get(ctx, "Plan")
	if err != nil || got.Keys != "my plan" || got.Agent != "codex" || !got.Builtin || !got.Overridden || got.Source != MacroOverride {
		t.Fatalf("edited Plan = %+v, %v", got, err)
	}
	list, _ := svc.List(ctx)
	if names := macroNames(list); len(names) != 4 || names[0] != "Plan" {
		t.Fatalf("an override must stay in the built-in's place: %v", names)
	}
	if err := svc.Reset(ctx, "plan"); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Get(ctx, "Plan")
	if got.Keys != original.Keys || got.Overridden || got.Agent != "" {
		t.Fatalf("reset Plan = %+v, want %+v", got, original)
	}
	if err := svc.Reset(ctx, "Plan"); err != nil {
		t.Fatalf("resetting an unedited built-in: %v", err)
	}
}

func TestABuiltInCannotBeDeleted(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	svc := NewMacroService(settings)
	if err := svc.Delete(ctx, "Review"); !errors.Is(err, ErrBuiltinMacro) {
		t.Fatalf("delete built-in = %v, want ErrBuiltinMacro", err)
	}
	if err := svc.Save(ctx, "mine", macros.Macro{Keys: "x"}, "web"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, "mine"); !errors.Is(err, ErrMacroNotFound) {
		t.Fatalf("deleted macro: %v", err)
	}
	if err := svc.Reset(ctx, "Mine"); !errors.Is(err, ErrNotBuiltinMacro) {
		t.Fatalf("reset of a non built-in = %v", err)
	}
}

func TestAFileEntryIsPinnedEvenOverABuiltIn(t *testing.T) {
	settings, _ := newSettingsFixture(t, "macros:\n  - name: review\n    keys: use my skill\n")
	ctx := context.Background()
	svc := NewMacroService(settings)
	got, err := svc.Get(ctx, "Review")
	if err != nil || got.Keys != "use my skill" || !got.Pinned || !got.Builtin || got.Source != MacroFromFile {
		t.Fatalf("Review = %+v, %v", got, err)
	}
	if err := svc.Save(ctx, "Review", macros.Macro{Keys: "x"}, "web"); !errors.Is(err, config.ErrEntryPinned) {
		t.Fatalf("save over the file = %v", err)
	}
	if err := svc.Reset(ctx, "Review"); !errors.Is(err, config.ErrEntryPinned) {
		t.Fatalf("reset of a pinned built-in = %v", err)
	}
	list, _ := svc.List(ctx)
	if len(list) != 4 {
		t.Fatalf("the file's Review must replace the built-in, not add one: %v", macroNames(list))
	}
}

func TestAMacroRefusesAnUnknownAppliesTo(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	svc := NewMacroService(settings)
	if err := svc.Save(context.Background(), "x", macros.Macro{Keys: "x", AppliesTo: "card"}, "web"); err == nil {
		t.Fatal("accepted applies_to: card")
	}
	if err := svc.Save(context.Background(), "x", macros.Macro{AppliesTo: "task"}, "web"); err == nil {
		t.Fatal("accepted a macro with no text")
	}
}

func TestSettingsListsMacrosWithTheBuiltIns(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	svc := NewMacroService(settings)
	if err := svc.Save(ctx, "Review", macros.Macro{Keys: "my review", AppliesTo: "pr"}, "web"); err != nil {
		t.Fatal(err)
	}
	lists, err := settings.Lists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lists {
		if l.Key != "macros" {
			continue
		}
		if len(l.Entries) != 4 || l.Entries[0].Name != "Plan" || !l.Entries[0].Builtin || l.Entries[0].Overridden || l.Entries[0].Source != SettingFromDefault {
			t.Fatalf("macros = %+v", l.Entries)
		}
		review := l.Entries[2]
		if !review.Builtin || !review.Overridden || review.Source != SettingFromDatabase || review.Value.(map[string]any)["keys"] != "my review" {
			t.Fatalf("Review = %+v", review)
		}
		return
	}
	t.Fatal("no macros list")
}

func TestAMacroOfYoursCanBeRenamed(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	ctx := context.Background()
	svc := NewMacroService(settings)
	if err := svc.Save(ctx, "Deep review", macros.Macro{Keys: "review deeply", AppliesTo: "pr", Agent: "codex"}, "web"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Rename(ctx, "deep review", "Thorough review", "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, "Deep review"); !errors.Is(err, ErrMacroNotFound) {
		t.Fatalf("old name: err = %v, want ErrMacroNotFound", err)
	}
	got, err := svc.Get(ctx, "Thorough review")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Thorough review" || got.Keys != "review deeply" || got.AppliesTo != "pr" || got.Agent != "codex" {
		t.Fatalf("renamed = %+v", got)
	}
	if err := svc.Rename(ctx, "Thorough review", "THOROUGH review", "web"); err != nil {
		t.Fatalf("changing only the case: %v", err)
	}
	if got, _ := svc.Get(ctx, "thorough review"); got.Name != "THOROUGH review" {
		t.Fatalf("case change: name = %q", got.Name)
	}
}

func TestARenameIsRefusedForBuiltInsFileMacrosAndNamesInUse(t *testing.T) {
	settings, _ := newSettingsFixture(t, "macros:\n  - name: fromfile\n    keys: hi\n")
	ctx := context.Background()
	svc := NewMacroService(settings)
	for _, name := range []string{"Mine", "Other"} {
		if err := svc.Save(ctx, name, macros.Macro{Keys: "text"}, "web"); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Rename(ctx, "Plan", "My plan", "web"); !errors.Is(err, ErrBuiltinMacroName) {
		t.Errorf("built-in: err = %v, want ErrBuiltinMacroName", err)
	}
	if err := svc.Rename(ctx, "fromfile", "Other file", "web"); !errors.Is(err, config.ErrEntryPinned) {
		t.Errorf("config.yaml: err = %v, want ErrEntryPinned", err)
	}
	for _, taken := range []string{"other", "review", "FromFile"} {
		if err := svc.Rename(ctx, "Mine", taken, "web"); !errors.Is(err, ErrMacroNameTaken) {
			t.Errorf("to %q: err = %v, want ErrMacroNameTaken", taken, err)
		}
	}
	if err := svc.Rename(ctx, "Missing", "Anything", "web"); !errors.Is(err, ErrMacroNotFound) {
		t.Errorf("missing: err = %v, want ErrMacroNotFound", err)
	}
	if err := svc.Rename(ctx, "Mine", "  ", "web"); err == nil {
		t.Error("blank name: want an error")
	}
	if got, err := svc.Get(ctx, "Mine"); err != nil || got.Keys != "text" {
		t.Fatalf("Mine after refused renames = %+v, %v", got, err)
	}
}

func TestMacroTiersAreEachAgentsTiersInOrder(t *testing.T) {
	settings, _ := newSettingsFixture(t, `adapters:
  claude-code:
    tiers:
      small: {description: Haiku for quick jobs, launch_args: [--model, haiku]}
      large: {description: Opus for hard reviews, launch_args: [--model, opus]}
  codex: {}
`)
	tiers, err := NewMacroService(settings).Tiers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []AgentTier{{Name: "large", Description: "Opus for hard reviews"}, {Name: "small", Description: "Haiku for quick jobs"}}
	got := tiers["claude-code"]
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("claude-code tiers = %+v, want %+v", got, want)
	}
	if _, ok := tiers["codex"]; ok {
		t.Errorf("codex has no tiers but is listed: %+v", tiers["codex"])
	}
}

func TestAMacroEntryReadsBackWhole(t *testing.T) {
	in := MacroEntry{Macro: macros.Macro{Name: "Plan", Keys: "plan", AppliesTo: "task", Agent: "codex", Tier: "large"}, Source: MacroOverride, Builtin: true, Overridden: true}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out MacroEntry
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("read back %+v, want %+v", out, in)
	}
}
