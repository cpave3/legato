package workflow

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

//go:embed examples/*.workflow.yaml
var exampleFiles embed.FS

// Example is a workflow definition Legato ships for people to add to their
// own workflows.
type Example struct {
	Name string
	YAML []byte
}

// Examples are the embedded example workflows, by name.
func Examples() []Example {
	entries, _ := fs.ReadDir(exampleFiles, "examples")
	out := make([]Example, 0, len(entries))
	for _, entry := range entries {
		data, err := exampleFiles.ReadFile("examples/" + entry.Name())
		if err != nil {
			continue
		}
		out = append(out, Example{Name: strings.TrimSuffix(entry.Name(), ".workflow.yaml"), YAML: data})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ExampleNamed is one embedded example, or false.
func ExampleNamed(name string) (Example, bool) {
	for _, example := range Examples() {
		if example.Name == name {
			return example, true
		}
	}
	return Example{}, false
}

// ReviewPassesWorkflowName is the review-pr-passes template, withdrawn from
// legato workflow add by the built-in review-loop (spec LEG-559 §
// Deprecation; LEG-599). Operators' copies of it still run unchanged.
const ReviewPassesWorkflowName = "review-pr-passes"

//go:embed withdrawn/review-pr-passes.workflow.yaml
var oldReviewPassesCopy []byte

// OldReviewPassesCopy is the review-pr-passes file as legato workflow add
// wrote it before review-loop replaced it: an operator's old copy, schema
// version 1, on the deprecated inline step github.pr.review_passes.
func OldReviewPassesCopy() Example {
	return Example{Name: ReviewPassesWorkflowName, YAML: append([]byte(nil), oldReviewPassesCopy...)}
}
