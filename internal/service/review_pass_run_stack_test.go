package service

import (
	"encoding/json"
	"testing"
)

func TestARunSavedBeforeStacksIsAStackOfOne(t *testing.T) {
	var run reviewRun
	if err := json.Unmarshal([]byte(`{"run_id":"wr-1","repo":"acme/widget","pr":12,"branch":"legato-review/pr-12","seen_head":"bbb222"}`), &run); err != nil {
		t.Fatal(err)
	}
	prs := run.prs()
	if len(prs) != 1 || prs[0].Number != 12 || prs[0].Branch != "legato-review/pr-12" || reviewStackName(run.Repo, run.numbers()) != "acme/widget#12" {
		t.Fatalf("prs %+v", prs)
	}
	if scope, err := reviewScope(map[string]any{}); err != nil || scope != ReviewScopeStack {
		t.Fatalf("default scope %q, %v", scope, err)
	}
	if _, err := reviewScope(map[string]any{"scope": "everything"}); err == nil {
		t.Fatal("an unknown scope was taken")
	}
}
