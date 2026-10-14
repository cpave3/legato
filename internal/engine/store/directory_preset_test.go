package store

import (
	"context"
	"testing"
)

func TestDirectoryPresetsAreListedInTheOrderTheyWereSaved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, preset := range []DirectoryPreset{
		{Path: "/home/me/Projects/legato", Name: "legato"},
		{Path: "/home/me/Development/autopilot", Name: "autopilot"},
	} {
		if err := s.SaveDirectoryPreset(ctx, preset); err != nil {
			t.Fatal(err)
		}
	}

	presets, err := s.ListDirectoryPresets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 2 || presets[0].Name != "legato" || presets[1].Path != "/home/me/Development/autopilot" {
		t.Fatalf("presets = %+v", presets)
	}
}

func TestSavingADirectoryPresetAgainKeepsOneRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveDirectoryPreset(ctx, DirectoryPreset{Path: "/srv/app", Name: "app"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDirectoryPreset(ctx, DirectoryPreset{Path: "/srv/app", Name: "renamed"}); err != nil {
		t.Fatal(err)
	}

	presets, err := s.ListDirectoryPresets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 1 || presets[0].Name != "renamed" {
		t.Fatalf("presets = %+v", presets)
	}
}
