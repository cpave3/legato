package service

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func roundItem(kind, answer string) store.HandoffReviewItem {
	return store.HandoffReviewItem{Kind: kind, Answer: answer}
}

// While a steward watches, an agent's round is done once every blocking
// finding is answered and it pushed or disputed one: nothing else waits
// for an answer.
func TestAStewardedRoundWaitsOnlyForBlockingFindings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		items  []store.HandoffReviewItem
		pushed bool
		done   bool
	}{
		{name: "nothing, no push", done: false},
		{name: "nothing, pushed", pushed: true, done: true},
		{name: "blocking unanswered", items: []store.HandoffReviewItem{roundItem(ItemBlocking, "")}, pushed: true, done: false},
		{name: "blocking fixed, no push", items: []store.HandoffReviewItem{roundItem(ItemBlocking, AnswerFixed)}, done: false},
		{name: "blocking fixed, pushed", items: []store.HandoffReviewItem{roundItem(ItemBlocking, AnswerFixed)}, pushed: true, done: true},
		{name: "blocking disputed", items: []store.HandoffReviewItem{roundItem(ItemBlocking, AnswerFixed), roundItem(ItemBlocking, AnswerReplied)}, done: true},
		{name: "unanswered check and comment ignored", items: []store.HandoffReviewItem{roundItem(ItemCheck, ""), roundItem(ItemComment, ""), roundItem(ItemBlocking, AnswerReplied)}, done: true},
	} {
		if got := stewardRoundDone(tc.items, tc.pushed); got != tc.done {
			t.Errorf("%s: done %v, want %v", tc.name, got, tc.done)
		}
	}
}

// Without a steward, every item but a nit needs an answer; the round is
// done on a push, a disputed blocking finding, or failing checks that were
// all answered by a reply (nothing to review again).
func TestARoundWaitsForEveryItemButNits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		items  []store.HandoffReviewItem
		pushed bool
		done   bool
	}{
		{name: "nothing, no push", done: false},
		{name: "nothing, pushed", pushed: true, done: true},
		{name: "nit unanswered, pushed", items: []store.HandoffReviewItem{roundItem(ItemNit, "")}, pushed: true, done: true},
		{name: "comment unanswered", items: []store.HandoffReviewItem{roundItem(ItemComment, "")}, pushed: true, done: false},
		{name: "check unanswered", items: []store.HandoffReviewItem{roundItem(ItemCheck, "")}, pushed: true, done: false},
		{name: "blocking disputed", items: []store.HandoffReviewItem{roundItem(ItemBlocking, AnswerReplied)}, done: true},
		{name: "comment replied, no push", items: []store.HandoffReviewItem{roundItem(ItemComment, AnswerReplied)}, done: false},
		{name: "checks replied", items: []store.HandoffReviewItem{roundItem(ItemCheck, AnswerReplied), roundItem(ItemNit, ""), roundItem(ItemCheck, AnswerReplied)}, done: true},
		{name: "check fixed, no push", items: []store.HandoffReviewItem{roundItem(ItemCheck, AnswerFixed)}, done: false},
		{name: "checks replied beside a fixed blocking", items: []store.HandoffReviewItem{roundItem(ItemCheck, AnswerReplied), roundItem(ItemBlocking, AnswerFixed)}, done: false},
	} {
		if got := reviewRoundDone(tc.items, tc.pushed); got != tc.done {
			t.Errorf("%s: done %v, want %v", tc.name, got, tc.done)
		}
	}
}
