package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestAgentServiceComposesLearningForAdaptersAndBindsManifest(t *testing.T) {
	for _, name := range []string{"chimera", "claude-code", "codex"} {
		t.Run(name, func(t *testing.T) {
			db := newServiceTestStore(t)
			ctx := context.Background()
			createTask(t, db, "task-1")
			if err := db.SetCodeContext(ctx, "task-1", &store.CodeContext{Repo: "git@github.com:Acme/Widget.git"}); err != nil {
				t.Fatal(err)
			}
			insertPromptLearning(t, db, "prefer-focused", "project", "repo:github.com/acme/widget", "validated", "Prefer focused changes.", true, 1)

			adapter := &fakeAdapter{name: name, rolePrompts: map[string]string{"worker": "STANDING ROLE"}}
			mt := newMockTmux()
			svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{
				Adapter: adapter, LearningPromptProvider: NewLearningService(db, nil),
				LearningPromptMaxItems: 10, LearningPromptMaxBytes: 2048,
			})
			opt := AgentSpawnOptions{Role: "worker"}
			if name == "chimera" {
				opt.Assignment = "KEEP THIS ASSIGNMENT"
			}
			if err := svc.SpawnAgent(ctx, "task-1", 0, 0, opt); err != nil {
				t.Fatal(err)
			}
			env := mt.envVarsFor("legato-task-1")
			data, err := os.ReadFile(env["LEGATO_ROLE_PROMPT_FILE"])
			if err != nil {
				t.Fatal(err)
			}
			prompt := string(data)
			if !strings.Contains(prompt, "STANDING ROLE") || !strings.Contains(prompt, "Prefer focused changes.") {
				t.Fatalf("adapter %s prompt did not compose standing and learned context: %q", name, prompt)
			}
			if name == "chimera" {
				brief, err := os.ReadFile(env["LEGATO_BRIEF_FILE"])
				if err != nil || string(brief) != "KEEP THIS ASSIGNMENT" {
					t.Fatalf("assignment brief=%q err=%v", brief, err)
				}
			}
			session, err := db.GetAgentSessionByTaskID(ctx, "task-1")
			if err != nil {
				t.Fatal(err)
			}
			manifests, err := db.ListLearningPromptManifestsBySession(ctx, session.ID)
			if err != nil || len(manifests) != 1 {
				t.Fatalf("manifests=%+v err=%v", manifests, err)
			}
			learned := "## Learned context\n- Prefer focused changes."
			digest := sha256.Sum256([]byte(learned))
			if manifests[0].ProjectKey != "repo:github.com/acme/widget" || manifests[0].RenderedDigest != "sha256:"+hex.EncodeToString(digest[:]) {
				t.Fatalf("manifest does not match rendered learning: %+v", manifests[0])
			}
		})
	}
}
