package handofftest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The main↔worker review contract: what a main reads from a worker about a
// handoff's review, recorded in testdata/review_contract.json from the
// worker as it was before review loops moved into workflows (LEG-575, M6
// of LEG-559). A main and a worker may be updated in either order, so the
// worker must keep sending every recorded field with the recorded value,
// and keep the another-pass and message routes as they were. A field added
// beside them (optional, which an older main ignores) passes; renaming or
// removing anything fails. Do not edit the fixture to make this test pass:
// a change it catches breaks a main that is not updated with the worker.

const reviewContractFile = "testdata/review_contract.json"

type reviewContract struct {
	// Pull is the route a main reads summary rows from; the Events
	// stream's pull message carries the same rows.
	Pull contractRoute `json:"pull"`
	// Blocks are the review blocks of a handoff's summary row, by the
	// situation that gives each (reviewContractScenarios).
	Blocks map[string]map[string]any `json:"blocks"`
	// Routes are a main's review actions on a stuck review.
	Routes map[string]contractRoute `json:"routes"`
}

type contractRoute struct {
	Method string `json:"method"`
	// Path names the handoff as {handoff_id}.
	Path string `json:"path"`
	// Body is what the main sends; Answer what the worker answers.
	Body   map[string]any `json:"body,omitempty"`
	Answer map[string]any `json:"answer,omitempty"`
	// Refused is the worker's refusal of the action in the situation named.
	Refused map[string]contractRefusal `json:"refused,omitempty"`
	// Typed is what the author is told of the body's text.
	Typed string `json:"typed,omitempty"`
}

// body is what the main sends: nothing without a recorded body.
func (r contractRoute) body() any {
	if r.Body == nil {
		return nil
	}
	return r.Body
}

type contractRefusal struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
}

func loadReviewContract(t *testing.T) reviewContract {
	t.Helper()
	data, err := os.ReadFile(reviewContractFile)
	if err != nil {
		t.Fatal(err)
	}
	var c reviewContract
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("%s: %v", reviewContractFile, err)
	}
	return c
}

// contractDiff says how got breaks the recorded value want: each recorded
// field missing or different, recursively. Fields only in got are
// additions, which the contract allows.
func contractDiff(path string, want, got any) []string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: got %v, want an object", path, got)}
		}
		var out []string
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			v, ok := g[k]
			if !ok {
				out = append(out, fmt.Sprintf("%s.%s: missing", path, k))
				continue
			}
			out = append(out, contractDiff(path+"."+k, w[k], v)...)
		}
		return out
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return []string{fmt.Sprintf("%s: got %v, want %v", path, got, want)}
		}
		var out []string
		for i := range w {
			out = append(out, contractDiff(fmt.Sprintf("%s[%d]", path, i), w[i], g[i])...)
		}
		return out
	default:
		if !reflect.DeepEqual(want, got) {
			return []string{fmt.Sprintf("%s: got %v, want %v", path, got, want)}
		}
		return nil
	}
}

func TestTheContractCheckFailsARenameOrARemovalAndPassesAnAddition(t *testing.T) {
	c := loadReviewContract(t)
	want := c.Blocks["waiting_for_author"]
	if want == nil {
		t.Fatal("no waiting_for_author block recorded")
	}
	edited := func(edit func(map[string]any)) map[string]any {
		var copied map[string]any
		data, _ := json.Marshal(want)
		_ = json.Unmarshal(data, &copied)
		edit(copied)
		return copied
	}
	if diff := contractDiff("review", want, edited(func(m map[string]any) { m["phase"] = "author_fixing" })); len(diff) == 0 {
		t.Error("a renamed phase word passed")
	}
	if diff := contractDiff("review", want, edited(func(m map[string]any) { delete(m, "blocking_open") })); len(diff) == 0 {
		t.Error("a removed field passed")
	}
	if diff := contractDiff("review", want, edited(func(m map[string]any) { m["blocking"] = m["blocking_open"]; delete(m, "blocking_open") })); len(diff) == 0 {
		t.Error("a renamed field passed")
	}
	if diff := contractDiff("review", want, edited(func(m map[string]any) { m["links"].([]any)[0].(map[string]any)["number"] = 7.0 })); len(diff) != 0 {
		t.Errorf("an added field inside a link failed: %v", diff)
	}
	if diff := contractDiff("review", want, edited(func(m map[string]any) { m["loop_id"] = "rl-1" })); len(diff) != 0 {
		t.Errorf("an added optional field failed: %v", diff)
	}
}

// reviewContractScenarios bring a handoff's review to each recorded block.
var reviewContractScenarios = map[string]func(t *testing.T) reviewed{
	"reviewing": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{})
		r.finish(t)
		return r
	},
	"waiting_on_author_working": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{})
		r.setAuthor(t, "working", nil)
		r.finish(t)
		return r
	},
	"waiting_on_author_waiting": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{})
		r.setAuthor(t, "waiting", nil)
		r.finish(t)
		return r
	},
	"waiting_on_author_push": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{})
		r.worker.Checkout.Dirty(true)
		r.finish(t)
		return r
	},
	"waiting_on_github": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{})
		r.worker.ReviewGitHub.FailReads(1000)
		r.finish(t)
		return r
	},
	"waiting_for_reviewer": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{MaxConcurrent: 1})
		ctx := context.Background()
		// A second reviewing card takes the only reviewer.
		if err := r.worker.Store.CreateTask(ctx, store.Task{ID: "busy", Title: "busy", Status: "Review"}); err != nil {
			t.Fatal(err)
		}
		if err := r.worker.Store.SaveHandoffReview(ctx, store.HandoffReview{TaskID: "busy", Phase: service.ReviewPhaseReviewing, UpdatedAt: "x"}); err != nil {
			t.Fatal(err)
		}
		r.finish(t)
		return r
	},
	"waiting_for_author": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{})
		r.finish(t)
		blocking := func(title string, line int) service.SubmittedFinding {
			return service.SubmittedFinding{PR: 7, Severity: "blocking", Title: title, Body: "It breaks.",
				FailureScenario: "A login while the server is down never returns.", Place: &service.ReviewPlace{Path: "login.go", Line: line}}
		}
		if _, err := r.submit(t, service.ReviewSubmission{Summary: "Three bugs.", Findings: []service.SubmittedFinding{
			blocking("Retries never stop", 20), blocking("The error is dropped", 22), blocking("The timeout is ignored", 24),
		}}); err != nil {
			t.Fatal(err)
		}
		// Then people comment, a stranger too, and the build fails.
		r.worker.ReviewGitHub.SetComments(7, ownersReview, botComment, strangers)
		r.worker.ReviewGitHub.SetChecks("aaa111", buildFails)
		r.tick()
		r.worker.Clock.Advance(3 * time.Minute)
		r.tick()
		return r
	},
	"clean": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{})
		r.finish(t)
		if _, err := r.submit(t, service.ReviewSubmission{Summary: "Solid fix."}); err != nil {
			t.Fatal(err)
		}
		return r
	},
	"clean_checks_pending": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{})
		r.worker.ReviewGitHub.SetChecks("aaa111", buildRuns)
		r.finish(t)
		if _, err := r.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
			t.Fatal(err)
		}
		return r
	},
	"clean_checks_replied": func(t *testing.T) reviewed {
		r := newReviewed(t, ReviewPassOptions{})
		r.worker.ReviewGitHub.SetChecks("aaa111", buildFails)
		r.finish(t)
		if _, err := r.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
			t.Fatal(err)
		}
		r.tick()
		r.worker.Clock.Advance(3 * time.Minute)
		r.tick()
		r.answer(t, "k-7-build", service.AnswerReplied, "Flaky: the runner ran out of disk.")
		r.finish(t)
		return r
	},
	"stuck": stuckReview,
	"waiting_on_start": func(t *testing.T) reviewed {
		r := stuckReview(t)
		// Run another pass, as a main asks it; the next tick starts it.
		if err := r.worker.Handoffs.ReviewAction(context.Background(), r.main.InstanceID(), r.away(t).HandoffID, service.ReviewActionAnotherPass, ""); err != nil {
			t.Fatal(err)
		}
		return r
	},
}

func stuckReview(t *testing.T) reviewed {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)
	r.worker.Clock.Advance(46 * time.Minute)
	r.tick()
	return r
}

// wireReview is the review block of the card's summary row, as a main
// reads it from the worker: from Pull, over the main's own client, and
// from the pull message the Events stream opens with.
func (r reviewed) wireReview(t *testing.T, pull contractRoute) (fromPull, fromStream any) {
	t.Helper()
	ctx := context.Background()
	client, err := r.main.Workers.Client(ctx, r.worker.InstanceID())
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := client.Call(ctx, pull.Method, pull.Path, nil, &answer); err != nil {
		t.Fatalf("%s %s: %v", pull.Method, pull.Path, err)
	}
	fromPull = r.rowReview(t, answer["handoffs"])

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	first := make(chan service.HandoffStreamMessage, 1)
	go func() {
		_ = r.worker.Handoffs.Stream(streamCtx, r.main.InstanceID(), func(m service.HandoffStreamMessage) error {
			select {
			case first <- m:
			default:
			}
			return nil
		})
	}()
	select {
	case m := <-first:
		var message map[string]any
		data, _ := json.Marshal(m) // as the Events connection writes it
		_ = json.Unmarshal(data, &message)
		if message["type"] != "pull" {
			t.Fatalf("the stream opened with %v", message)
		}
		fromStream = r.rowReview(t, message["handoffs"])
	case <-time.After(10 * time.Second):
		t.Fatal("the Events stream sent nothing")
	}
	return fromPull, fromStream
}

func (r reviewed) rowReview(t *testing.T, handoffs any) any {
	t.Helper()
	rows, _ := handoffs.([]any)
	for _, row := range rows {
		if row, ok := row.(map[string]any); ok && row["worker_task_id"] == r.card {
			return row["review"]
		}
	}
	t.Fatalf("no row for %s in %v", r.card, handoffs)
	return nil
}

func TestTheWorkerSendsTheRecordedReviewBlocks(t *testing.T) {
	c := loadReviewContract(t)
	if c.Pull.Path != service.HandoffPullPath {
		t.Errorf("the main pulls %s; the contract says %s", service.HandoffPullPath, c.Pull.Path)
	}
	for name := range c.Blocks {
		if reviewContractScenarios[name] == nil {
			t.Errorf("no scenario brings a review to the recorded block %q", name)
		}
	}
	for name, scenario := range reviewContractScenarios {
		t.Run(name, func(t *testing.T) {
			want, ok := c.Blocks[name]
			if !ok {
				t.Fatalf("no block recorded for %q", name)
			}
			r := scenario(t)
			fromPull, fromStream := r.wireReview(t, c.Pull)
			for _, got := range []struct {
				from  string
				block any
			}{{"pull", fromPull}, {"events", fromStream}} {
				if diff := contractDiff("review", want, got.block); len(diff) > 0 {
					live, _ := json.Marshal(got.block)
					t.Errorf("the %s row breaks the contract:\n  %s\nsent %s", got.from, strings.Join(diff, "\n  "), live)
				}
			}
		})
	}
}

func TestTheMainReadsEveryRecordedReviewBlock(t *testing.T) {
	// A main updated before its worker reads the worker's recorded block
	// whole, and has words for every phase and wait in it.
	c := loadReviewContract(t)
	for name, want := range c.Blocks {
		row, _ := json.Marshal(map[string]any{"handoff_id": "h1", "worker_task_id": "c1", "state": "in_progress", "review": want})
		var summary service.HandoffSummary
		if err := json.Unmarshal(row, &summary); err != nil || summary.Review == nil {
			t.Errorf("%s: the main cannot read the row: %v", name, err)
			continue
		}
		var reread map[string]any
		data, _ := json.Marshal(summary.Review)
		_ = json.Unmarshal(data, &reread)
		if diff := contractDiff("review", want, reread); len(diff) > 0 {
			t.Errorf("%s: the main drops part of the block:\n  %s", name, strings.Join(diff, "\n  "))
		}
		if summary.Review.Summary() == "" {
			t.Errorf("%s: the main has no words for %v", name, want)
		}
	}
}

func TestTheReviewRoutesKeepTheirRecordedShape(t *testing.T) {
	c := loadReviewContract(t)
	another, message := c.Routes["another_pass"], c.Routes["message"]
	if another.Path == "" || message.Path == "" {
		t.Fatalf("routes recorded: %+v", c.Routes)
	}
	ctx := context.Background()
	// call makes the review action on the recorded route, as a main does.
	call := func(t *testing.T, r reviewed, route contractRoute, action string) (map[string]any, error) {
		t.Helper()
		handoffID := r.away(t).HandoffID
		path := strings.ReplaceAll(route.Path, "{handoff_id}", handoffID)
		// The main's own call goes to the recorded route.
		if got := service.HandoffReviewPath(handoffID, action); got != path {
			t.Errorf("the main calls %s; the contract says %s", got, path)
		}
		client, err := r.main.Workers.Client(ctx, r.worker.InstanceID())
		if err != nil {
			t.Fatal(err)
		}
		var answer map[string]any
		err = client.Call(ctx, route.Method, path, route.body(), &answer)
		return answer, err
	}
	refusal := func(t *testing.T, err error, want contractRefusal) {
		t.Helper()
		var refused *service.WorkerRefusal
		if !errors.As(err, &refused) || refused.Status != want.Status || refused.Code != want.Code {
			t.Errorf("refusal %v (%+v); want %d %s", err, refused, want.Status, want.Code)
		}
	}

	t.Run("another_pass", func(t *testing.T) {
		r := newReviewed(t, ReviewPassOptions{})
		r.finish(t)
		_, err := call(t, r, another, service.ReviewActionAnotherPass)
		refusal(t, err, another.Refused["not_stuck"])

		r.worker.Clock.Advance(46 * time.Minute)
		r.tick()
		answer, err := call(t, r, another, service.ReviewActionAnotherPass)
		if err != nil {
			t.Fatal(err)
		}
		if diff := contractDiff("answer", another.Answer, answer); len(diff) > 0 {
			t.Errorf("answer %v: %v", answer, diff)
		}
		r.tick()
		if loop := r.loop(t); loop.Phase != service.ReviewPhaseReviewing || loop.Pass != 2 {
			t.Fatalf("after another pass: %+v", loop)
		}
	})

	t.Run("message", func(t *testing.T) {
		r := newReviewed(t, ReviewPassOptions{})
		r.finish(t)
		answer, err := call(t, r, message, service.ReviewActionMessage)
		if err != nil {
			t.Fatal(err)
		}
		if diff := contractDiff("answer", message.Answer, answer); len(diff) > 0 {
			t.Errorf("answer %v: %v", answer, diff)
		}
		if typed := r.worker.typedTo("legato-" + r.card); !strings.Contains(typed, message.Typed) {
			t.Errorf("typed to the author %q; want %q", typed, message.Typed)
		}
	})

	t.Run("unknown handoff", func(t *testing.T) {
		r := newReviewed(t, ReviewPassOptions{})
		client, err := r.main.Workers.Client(ctx, r.worker.InstanceID())
		if err != nil {
			t.Fatal(err)
		}
		for _, route := range []contractRoute{another, message} {
			err := client.Call(ctx, route.Method, strings.ReplaceAll(route.Path, "{handoff_id}", "h-none"), route.body(), nil)
			refusal(t, err, route.Refused["not_found"])
		}
	})
}
