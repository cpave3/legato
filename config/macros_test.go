package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestMessageIsAnotherNameForKeys(t *testing.T) {
	for _, tc := range []struct{ name, yamlText, jsonText string }{
		{"message", "name: Plan\nmessage: plan it\n", `{"name":"Plan","message":"plan it"}`},
		{"keys", "name: Plan\nkeys: plan it\n", `{"name":"Plan","keys":"plan it"}`},
		{"both the same", "name: Plan\nkeys: plan it\nmessage: plan it\n", `{"name":"Plan","keys":"plan it","message":"plan it"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fromYAML, fromJSON Macro
			if err := yaml.Unmarshal([]byte(tc.yamlText), &fromYAML); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.jsonText), &fromJSON); err != nil {
				t.Fatal(err)
			}
			for _, got := range []Macro{fromYAML, fromJSON} {
				if got.Name != "Plan" || got.Keys != "plan it" {
					t.Errorf("got %+v, want Plan with keys %q", got, "plan it")
				}
			}
		})
	}
}

func TestMessageAndKeysThatDifferAreRefused(t *testing.T) {
	var m Macro
	if err := yaml.Unmarshal([]byte("name: Plan\nkeys: one\nmessage: two\n"), &m); err == nil {
		t.Error("yaml: want an error for keys and message that differ")
	}
	if err := json.Unmarshal([]byte(`{"name":"Plan","keys":"one","message":"two"}`), &m); err == nil {
		t.Error("json: want an error for keys and message that differ")
	}
}

func TestMacroJSONStillWritesKeys(t *testing.T) {
	data, err := json.Marshal(Macro{Name: "Plan", Keys: "plan it"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != `{"name":"Plan","keys":"plan it"}` {
		t.Errorf("got %s", got)
	}
}
