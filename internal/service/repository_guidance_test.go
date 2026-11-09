package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestRepositoryGuidanceService(t *testing.T) {
	ctx := context.Background()
	newService := func(t *testing.T) *RepositoryGuidanceService {
		return NewRepositoryGuidanceService(newServiceTestStore(t), []config.AgentGuidance{
			{Repo: "github.com/acme/web-app", Text: "From the config file."},
		})
	}

	t.Run("lists the config file's guidance, then the saved", func(t *testing.T) {
		svc := newService(t)
		if _, err := svc.Save(ctx, "git@github.com:Acme/API.git", "  Run the API checks.  "); err != nil {
			t.Fatal(err)
		}
		got, err := svc.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := []RepositoryGuidance{
			{Repo: "github.com/acme/web-app", Text: "From the config file.", Configured: true},
			{Repo: "github.com/acme/api", Text: "Run the API checks."},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("List = %#v, want %#v", got, want)
		}
	})

	t.Run("saving a repository again replaces its guidance", func(t *testing.T) {
		svc := newService(t)
		for _, text := range []string{"First.", "Second."} {
			if _, err := svc.Save(ctx, "acme/api", text); err != nil {
				t.Fatal(err)
			}
		}
		text, err := svc.GuidanceFor(ctx, "github.com/acme/api")
		if err != nil || text != "Second." {
			t.Fatalf("GuidanceFor = %q, %v", text, err)
		}
		if got, _ := svc.List(ctx); len(got) != 2 {
			t.Fatalf("List = %#v", got)
		}
	})

	t.Run("the config file's guidance is changed there", func(t *testing.T) {
		svc := newService(t)
		if _, err := svc.Save(ctx, "Acme/Web-App", "Other."); !errors.Is(err, ErrRepositoryGuidanceConfigured) {
			t.Fatalf("Save = %v", err)
		}
		if err := svc.Remove(ctx, "acme/web-app"); !errors.Is(err, ErrRepositoryGuidanceConfigured) {
			t.Fatalf("Remove = %v", err)
		}
	})

	t.Run("what is not guidance for a repository is refused", func(t *testing.T) {
		svc := newService(t)
		if _, err := svc.Save(ctx, "web-app", "Text."); !errors.Is(err, ErrRepositoryGuidanceInvalid) {
			t.Fatalf("Save with no owner = %v", err)
		}
		if _, err := svc.Save(ctx, "acme/api", "   "); !errors.Is(err, ErrRepositoryGuidanceInvalid) {
			t.Fatalf("Save with no text = %v", err)
		}
	})

	t.Run("removing saved guidance", func(t *testing.T) {
		svc := newService(t)
		if _, err := svc.Save(ctx, "acme/api", "Text."); err != nil {
			t.Fatal(err)
		}
		if err := svc.Remove(ctx, "https://github.com/acme/api"); err != nil {
			t.Fatal(err)
		}
		if text, _ := svc.GuidanceFor(ctx, "github.com/acme/api"); text != "" {
			t.Fatalf("GuidanceFor after Remove = %q", text)
		}
		if err := svc.Remove(ctx, "acme/api"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Remove of none = %v", err)
		}
	})
}
