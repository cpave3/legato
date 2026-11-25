package service

import (
	"context"
	"math"
	"reflect"
	"testing"
)

// A pull request's own diff is GitHub's: the head's added lines, and a file with no patch changed in full.
func TestAPullRequestsOwnDiffIsReadFromGitHub(t *testing.T) {
	f := newPublisherFixture(t)

	diff, err := f.publisher.PullDiff(context.Background(), "cpave3/legato", 12)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string][][2]int{"main.go": {{2, 3}, {22, 22}}, "big.bin": {{1, math.MaxInt}}}
	if !reflect.DeepEqual(diff, want) {
		t.Fatalf("diff = %v, want %v", diff, want)
	}
}

// Lines only deleted change the head where they were: the lines either
// side.
func TestAPatchsDeletionChangesTheLinesAroundIt(t *testing.T) {
	patch := "@@ -10,4 +10,2 @@ func f() {\n \ta()\n-\tb()\n-\tc()\n \td()"
	if got, want := patchChangedLines(patch), [][2]int{{10, 11}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
}
