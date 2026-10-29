package credential

import "testing"

func TestParseRefPicksTheStoreByScheme(t *testing.T) {
	for in, want := range map[string]Ref{
		"legato://elevenlabs-key":             {Scheme: "legato", Path: "elevenlabs-key"},
		"op://Legato/eleven labs/credential":  {Scheme: "op", Path: "Legato/eleven labs/credential"},
		"systemd-creds://github-app":          {Scheme: "systemd-creds", Path: "github-app"},
		"keychain://legato/eleven":            {Scheme: "keychain", Path: "legato/eleven"},
		"env://ELEVEN_KEY":                    {Scheme: "env", Path: "ELEVEN_KEY"},
		"file:///home/me/.config/eleven.key":  {Scheme: "file", Path: "/home/me/.config/eleven.key"},
	} {
		got, err := ParseRef(in)
		if err != nil || got != want {
			t.Errorf("ParseRef(%q) = %+v, %v; want %+v", in, got, err, want)
		}
		if got.String() != in {
			t.Errorf("%q round-trips as %q", in, got.String())
		}
	}
	for _, bad := range []string{"", "eleven", "vault://kv/x", "legato://", "file://relative/path", "keychain://onlyservice", "env://not a name"} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) accepted", bad)
		}
	}
}

func TestCoversMatchesResourcesAndPatternsAsStrings(t *testing.T) {
	cases := []struct {
		pattern, resource string
		want              bool
	}{
		{"", "anything", true},
		{"github://cpave3/*", "github://cpave3/legato", true},
		{"github://cpave3/*", "github://cpave3/legato/refs/heads/main", false},
		{"github://cpave3/*/refs/heads/legato/**", "github://cpave3/legato/refs/heads/legato/fix-sort", true},
		{"github://cpave3/*/refs/heads/legato/**", "github://cpave3/legato/refs/heads/main", false},
		// A requested pattern is covered when the rule's pattern matches it as a string.
		{"github://cpave3/*/refs/heads/legato/**", "github://cpave3/legato/refs/heads/legato/attention-*", true},
		{"ssh://*@*.lan", "ssh://deploy@nas.lan", true},
		{"ssh://*@*.lan", "ssh://deploy@prod.example", false},
	}
	for _, c := range cases {
		if got := Covers(c.pattern, c.resource); got != c.want {
			t.Errorf("Covers(%q, %q) = %v, want %v", c.pattern, c.resource, got, c.want)
		}
	}
}

func TestSpecificityCountsLiteralCharacters(t *testing.T) {
	if Specificity("github://cpave3/*") >= Specificity("github://cpave3/legato") {
		t.Fatal("a literal resource should be more specific than a pattern over it")
	}
	if Specificity("") != 0 {
		t.Fatal("an absent pattern has no specificity")
	}
	if Specificity("a*b") != 2 || Specificity("**") != 0 || Specificity("x?") != 1 {
		t.Fatalf("specificity = %d %d %d", Specificity("a*b"), Specificity("**"), Specificity("x?"))
	}
}

func TestIsPatternSaysWhetherARequestIsBroader(t *testing.T) {
	if !IsPattern("github://cpave3/legato/refs/heads/legato/attention-*") || IsPattern("github://cpave3/legato") {
		t.Fatal("IsPattern")
	}
}
