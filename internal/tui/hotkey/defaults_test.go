package hotkey

import "testing"

func TestDefaultBindingsValidate(t *testing.T) {
	if _, err := NewRegistry(DefaultBindings()); err != nil {
		t.Fatal(err)
	}
}
