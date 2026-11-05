package service

import "testing"

// A worker's card for a provider-backed task is a copy under the provider's
// name and "-copy", which says where the copy came from.
func TestAHandedOffTicketIsACopyUnderItsProvidersName(t *testing.T) {
	for _, provider := range []string{"jira", "burnlite"} {
		copied := HandoffCopyProvider(provider)
		if copied != provider+"-copy" || !IsHandoffCopy(copied) || HandoffCopyOf(copied) != provider {
			t.Fatalf("%s: copy %q, a copy %v, of %q", provider, copied, IsHandoffCopy(copied), HandoffCopyOf(copied))
		}
		if IsHandoffCopy(provider) || HandoffCopyOf(provider) != "" {
			t.Fatalf("%s is taken for a copy", provider)
		}
	}
	for _, provider := range []string{"", "-copy"} {
		if IsHandoffCopy(provider) {
			t.Fatalf("%q is taken for a copy", provider)
		}
	}
}

// A ticket's provider is named for people by its own name, and a copy by the
// provider it came from.
func TestATicketProviderIsNamedForPeople(t *testing.T) {
	for provider, want := range map[string]string{
		"jira": "Jira", "jira-copy": "Jira", "burnlite": "Burnlite", "burnlite-copy": "Burnlite",
		"github": "GitHub", "linear": "linear", "linear-copy": "linear",
	} {
		if got := TicketProviderLabel(provider); got != want {
			t.Errorf("%s: %q, want %q", provider, got, want)
		}
	}
}
