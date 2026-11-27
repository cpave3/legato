package service

import (
	"slices"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// The layers a steward adds to its loop are the author's own pull requests
// stacked on the top: one from a fork or by someone else is left out, and
// one whose branch a layer already checks out, or whose branch name git
// would refuse, is checked out on legato-review/pr-N instead.
func TestStackedLayersToAddAreTheAuthorsOwn(t *testing.T) {
	rec := &store.ReviewLoop{Repo: "cpave3/legato", AuthorTask: "author"}
	layers := []store.ReviewLoopPR{
		{Number: 10, Branch: "base", Author: "dev"},
		{Number: 11, Branch: "feature", LocalBranch: "legato-review/pr-11", Author: "Dev"},
	}
	stacked := []ReviewGitHubPR{
		{Number: 12, Branch: "next", Base: "feature", Title: "Next", URL: "u12", Author: "DEV", HeadSHA: "s12", Draft: true, AuthorAssociation: "OWNER"},
		{Number: 13, Branch: "fork", HeadRepo: "someone/legato", Author: "dev"},
		{Number: 14, Branch: "theirs", Author: "someone"},
		{Number: 15, Branch: "base", HeadRepo: "CPAVE3/legato", Author: "dev"},
		{Number: 16, Branch: "bad..name", Author: "dev"},
		{Number: 17, Branch: "next", Author: "dev"},
	}
	add, numbers := stackedLayersToAdd(rec, layers, stacked)
	if !slices.Equal(numbers, []int{10, 11, 12, 15, 16, 17}) {
		t.Fatalf("numbers %v", numbers)
	}
	want := []store.ReviewLoopPR{
		{Number: 12, Branch: "next", Base: "feature", Title: "Next", URL: "u12", State: "open", Draft: true, Author: "DEV",
			AuthorAssociation: "OWNER", AuthorTask: "author", LocalBranch: "next"},
		{Number: 15, Branch: "base", HeadRepo: "CPAVE3/legato", State: "open", Author: "dev", AuthorTask: "author", LocalBranch: "legato-review/pr-15"},
		{Number: 16, Branch: "bad..name", State: "open", Author: "dev", AuthorTask: "author", LocalBranch: "legato-review/pr-16"},
		{Number: 17, Branch: "next", State: "open", Author: "dev", AuthorTask: "author", LocalBranch: "legato-review/pr-17"},
	}
	if !slices.Equal(add, want) {
		t.Fatalf("add\n%+v\nwant\n%+v", add, want)
	}

	// Nothing stacked that is the author's: nothing to add.
	if add, _ := stackedLayersToAdd(rec, layers, stacked[1:3]); len(add) != 0 {
		t.Fatalf("added %+v", add)
	}
}
