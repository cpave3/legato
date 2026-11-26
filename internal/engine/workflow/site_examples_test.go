package workflow_test

import (
	"os"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// These examples are copied by operators, so check the published text rather
// than a second copy of the YAML in a fixture.
func TestOperatorGuideWorkflowExamplesValidate(t *testing.T) {
	for _, tc := range []struct {
		page, heading, prefix string
	}{
		{"delivery", "## A smaller outcome-branching workflow\n", ""},
		{"workflows", "### Loops\n", "schema_version: 2\nname: test-loop\ntitle: Test loop\ninputs:\n  task: {type: task, required: true}\nsteps:\n"},
	} {
		t.Run(tc.page+"/"+strings.TrimSpace(tc.heading), func(t *testing.T) {
			data, err := os.ReadFile("../../../site/src/content/docs/using/" + tc.page + ".md")
			if err != nil {
				t.Fatal(err)
			}
			_, section, ok := strings.Cut(string(data), tc.heading)
			if !ok {
				t.Fatalf("heading %q missing", tc.heading)
			}
			_, block, ok := strings.Cut(section, "```yaml\n")
			if !ok {
				t.Fatal("YAML example missing")
			}
			block, _, ok = strings.Cut(block, "\n```")
			if !ok {
				t.Fatal("YAML fence not closed")
			}
			definition, diagnostics := workflow.Check(tc.page+".yaml", []byte(tc.prefix+block))
			if definition == nil || len(diagnostics) != 0 {
				t.Fatalf("example diagnostics: %+v", diagnostics)
			}
			calls := workflow.CallDiagnostics([]byte(tc.prefix+block), definition, []*workflow.Definition{definition, workflow.BuiltInReviewLoop()})
			if len(calls) != 0 {
				t.Fatalf("workflow call diagnostics: %+v", calls)
			}
		})
	}
}
