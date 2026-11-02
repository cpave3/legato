package store

import (
	"context"
	"testing"
)

func TestASettingOfThisLegatoIsKeptAndChanged(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if got, err := s.GetInstanceValue(ctx, "web.tint"); err != nil || got != "" {
		t.Fatalf("before it is set: %q, %v", got, err)
	}
	for _, value := range []string{"purple", "yellow", ""} {
		if err := s.SetInstanceValue(ctx, "web.tint", value); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.GetInstanceValue(ctx, "web.tint"); got != value {
			t.Fatalf("after setting %q: %q", value, got)
		}
	}
}
