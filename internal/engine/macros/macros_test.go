package macros

import (
	"encoding/json"
	"testing"
)

func TestMacroJSONRoundTrip(t *testing.T) {
	m := Macro{Name: "run tests", Keys: "task test\n"}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got Macro
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != m.Name || got.Keys != m.Keys {
		t.Errorf("round-trip failed: got %+v, want %+v", got, m)
	}
}

func TestListResultJSON(t *testing.T) {
	lr := ListResult{
		Macros: []Macro{
			{Name: "run tests", Keys: "task test\n"},
			{Name: "git diff", Keys: "! git diff\n"},
		},
	}
	data, err := json.Marshal(lr)
	if err != nil {
		t.Fatal(err)
	}
	var got ListResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Macros) != 2 {
		t.Fatalf("expected 2 macros, got %d", len(got.Macros))
	}
	if got.Macros[0].Name != "run tests" {
		t.Errorf("macro[0].Name = %q, want %q", got.Macros[0].Name, "run tests")
	}
	if got.Macros[1].Keys != "! git diff\n" {
		t.Errorf("macro[1].Keys = %q, want %q", got.Macros[1].Keys, "! git diff\n")
	}
}
