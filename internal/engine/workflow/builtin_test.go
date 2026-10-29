package workflow

import "testing"

func TestWithBuiltInsAddsMissingAndKeepsUserDefinitions(t *testing.T) {
	user := &Definition{Name: "legato-job", Source: "/home/me/.config/legato/workflows/job.yaml"}
	result := WithBuiltIns(DiscoveryResult{Definitions: []*Definition{{Name: "zeta"}, user}},
		&Definition{Name: "legato-job", Source: "builtin:legato-job"}, &Definition{Name: "alpha", Source: "builtin:alpha"})

	var names []string
	for _, d := range result.Definitions {
		names = append(names, d.Name)
	}
	if len(names) != 3 || names[0] != "alpha" || names[1] != "legato-job" || names[2] != "zeta" {
		t.Fatalf("names = %v, want sorted alpha, legato-job, zeta", names)
	}
	if result.Definitions[1] != user {
		t.Fatalf("legato-job = %+v, want the user's definition", result.Definitions[1])
	}
}
