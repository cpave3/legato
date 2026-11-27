package handofftest

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestReviewPassFreezesItsLocalAccountBeforeRetry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "claude-original")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	r := newReviewRun(t, ReviewPassOptions{Runtime: func(o service.ReviewPassOptions) service.ReviewPassOptions {
		o.Config, o.Advert = ReviewPassOptions{}.config, ReviewPassOptions{}.advert
		o.ReviewerDefaults = func(_ context.Context, _ string, defaults service.ReviewPassSettings) (service.ReviewPassSettings, error) {
			defaults.Agent, defaults.Account = "claude-code", "default"
			return defaults, nil
		}
		return o
	}})
	view := r.startLoop(t, pr12)
	r.tick()
	ctx := context.Background()
	session, err := r.worker.Store.GetAgentSessionInSlot(ctx, view.Run.TaskID, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := r.worker.Store.ExecutionAccount(ctx, session.ExecutionID)
	if err != nil || identity == nil || identity.ConfigDir != root {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
	pass, err := r.worker.Store.GetHandoffReviewPass(ctx, view.Run.TaskID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pass.ReviewerSettings, root) {
		t.Fatalf("pass did not freeze local identity: %s", pass.ReviewerSettings)
	}
	var settings service.ReviewPassSettings
	if err := json.Unmarshal([]byte(pass.ReviewerSettings), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Account != "default" {
		t.Fatalf("reviewer profile account lost: %+v", settings)
	}
	advert, err := json.Marshal(service.ReviewPassesAdvert{Defaults: settings})
	if err != nil || strings.Contains(string(advert), root) {
		t.Fatalf("local path leaked into advert: %s %v", advert, err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "changed"))
	if err := r.worker.Tmux.Kill(session.TmuxSession); err != nil {
		t.Fatal(err)
	}
	r.tick()
	loop, err := r.worker.Store.GetHandoffReview(ctx, view.Run.TaskID)
	if err != nil || loop.Phase != service.ReviewPhaseStuck || !strings.Contains(loop.Reason, "configuration changed") {
		t.Fatalf("retry silently switched account: %+v err=%v", loop, err)
	}
}
