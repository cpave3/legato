package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func newResourceStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestResourcesFromTheConfigurationComeFirstAndHideSavedOnesOfTheSameName(t *testing.T) {
	ctx := context.Background()
	db := newResourceStore(t)
	for _, name := range []string{"Vault", "Specs"} {
		if _, err := db.SaveResource(ctx, store.Resource{Name: name, Paths: []store.ResourcePath{{Path: "/saved/" + name, Access: "read"}}}); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewResourceService(db, []Resource{{Name: "vault", Paths: []ResourcePath{{Path: "/home/me/My Vault", Access: "write"}}}}, "/home/me")

	resources, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 2 {
		t.Fatalf("resources = %+v", resources)
	}
	if resources[0].Name != "vault" || !resources[0].Configured || resources[0].Paths[0].Path != "/home/me/My Vault" {
		t.Fatalf("configured = %+v", resources[0])
	}
	if resources[1].Name != "Specs" || resources[1].Configured || resources[1].ID == 0 {
		t.Fatalf("saved = %+v", resources[1])
	}
}

func TestSavingAResourceExpandsTheHomeDirectoryAndChecksItCanBeUsed(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	vault := filepath.Join(home, "Documents", "My Vault")
	notes := filepath.Join(home, "Old Notes")
	for _, dir := range []string{vault, notes} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewResourceService(newResourceStore(t), []Resource{{Name: "Specs", Paths: []ResourcePath{{Path: "/specs", Access: "read"}}}}, home)

	saved, err := svc.Save(ctx, Resource{Name: " Vault ", Paths: []ResourcePath{{Path: "~/Documents/My Vault/"}, {Path: notes, Access: "write"}}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 0 || saved.Name != "Vault" {
		t.Fatalf("saved = %+v", saved)
	}
	if saved.Paths[0] != (ResourcePath{Path: vault, Access: "read"}) || saved.Paths[1] != (ResourcePath{Path: notes, Access: "write"}) {
		t.Fatalf("paths = %+v", saved.Paths)
	}

	for _, bad := range []Resource{
		{Name: "", Paths: []ResourcePath{{Path: vault}}},
		{Name: "Empty"},
		{Name: "Relative", Paths: []ResourcePath{{Path: "notes"}}},
		{Name: "Missing", Paths: []ResourcePath{{Path: filepath.Join(home, "nowhere")}}},
		{Name: "Access", Paths: []ResourcePath{{Path: vault, Access: "admin"}}},
		{Name: "specs", Paths: []ResourcePath{{Path: vault}}},
	} {
		if _, err := svc.Save(ctx, bad); !errors.Is(err, ErrResourceInvalid) {
			t.Errorf("Save(%+v) = %v", bad, err)
		}
	}
}

func TestResolvingResourcesByNameFindsThemWhateverTheCase(t *testing.T) {
	ctx := context.Background()
	db := newResourceStore(t)
	saved, err := db.SaveResource(ctx, store.Resource{Name: "Specs", Paths: []store.ResourcePath{{Path: "/specs", Access: "read"}}})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewResourceService(db, []Resource{{Name: "Vault", Paths: []ResourcePath{{Path: "/vault", Access: "write"}}}}, "/home/me")

	resources, err := svc.Resolve(ctx, []string{"specs", "Vault", "SPECS"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 2 || resources[0].Name != "Specs" || resources[1].Name != "Vault" {
		t.Fatalf("resources = %+v", resources)
	}
	if _, err := svc.Resolve(ctx, []string{"Nope"}); !errors.Is(err, ErrResourceInvalid) {
		t.Fatalf("unknown name = %v", err)
	}

	if err := svc.Delete(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, []string{"Specs"}); err == nil {
		t.Fatal("a deleted resource still resolves")
	}
}

type recordingAttacher struct {
	taskID    string
	resources []Resource
	attached  []string
}

func (r *recordingAttacher) AttachResources(_ context.Context, taskID string, resources []Resource) error {
	r.taskID, r.resources = taskID, resources
	return nil
}

func (r *recordingAttacher) AttachedResources(context.Context, string) ([]string, error) {
	return r.attached, nil
}

func TestAttachingResourcesByNameGivesThemToTheRunningAgent(t *testing.T) {
	ctx := context.Background()
	attacher := &recordingAttacher{attached: []string{"Vault"}}
	svc := NewResourceService(nil, []Resource{{Name: "Vault", Paths: []ResourcePath{{Path: "/vault", Access: "write"}}}}, "/home/me")
	svc.SetAttacher(attacher)

	if err := svc.Attach(ctx, "task-1", []string{"vault"}); err != nil {
		t.Fatal(err)
	}
	if attacher.taskID != "task-1" || len(attacher.resources) != 1 || attacher.resources[0].Paths[0].Path != "/vault" {
		t.Fatalf("attached %+v to %q", attacher.resources, attacher.taskID)
	}
	if err := svc.Attach(ctx, "task-1", []string{"nope"}); !errors.Is(err, ErrResourceInvalid) {
		t.Fatalf("unknown name = %v", err)
	}
	names, err := svc.Attached(ctx, "task-1")
	if err != nil || len(names) != 1 || names[0] != "Vault" {
		t.Fatalf("attached = %v, %v", names, err)
	}
}

func TestAddingPathsByNameCreatesTheResourceOrGrowsIt(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	vault, templates := filepath.Join(home, "My Vault"), filepath.Join(home, "templates")
	for _, dir := range []string{vault, templates} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewResourceService(newResourceStore(t), nil, home)

	if _, err := svc.AddPaths(ctx, "Vault", []ResourcePath{{Path: "~/My Vault", Access: "write"}}); err != nil {
		t.Fatal(err)
	}
	grown, err := svc.AddPaths(ctx, "vault", []ResourcePath{{Path: templates}, {Path: vault, Access: "read"}})
	if err != nil {
		t.Fatal(err)
	}
	if grown.Name != "Vault" || len(grown.Paths) != 2 || grown.Paths[0] != (ResourcePath{Path: vault, Access: "read"}) || grown.Paths[1].Path != templates {
		t.Fatalf("grown = %+v", grown)
	}

	if err := svc.RemoveNamed(ctx, "VAULT"); err != nil {
		t.Fatal(err)
	}
	if all, _ := svc.List(ctx); len(all) != 0 {
		t.Fatalf("after removal = %+v", all)
	}
	if err := svc.RemoveNamed(ctx, "Vault"); !errors.Is(err, ErrResourceInvalid) {
		t.Fatalf("removing a missing resource = %v", err)
	}
}
