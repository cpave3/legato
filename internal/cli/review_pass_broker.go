package cli

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ReviewPassAnswerResult is what `review_pass.answer` answers: the task the
// loop is kept on, the item and the answer recorded.
type ReviewPassAnswerResult struct {
	TaskID string `json:"task_id"`
	Item   string `json:"item"`
	Answer string `json:"answer"`
}

// ReviewPassBrokerOps are `legato review-pass feedback` and `answer` for the
// caller's session: the loop is the one the session's task authors, found
// from the session the broker token names and never from a task the agent
// names (B4). Only the author task's main slot is let through. hasSkill says
// whether the author's agent has a skill (the stack guidance names one); nil
// for none.
func ReviewPassBrokerOps(db *store.Store, hasSkill func(string) bool) map[string]BrokerOp {
	answerer := func(caller BrokerCaller) service.ReviewAnswerer {
		return service.ReviewAnswerer{TaskID: caller.TaskID, ExecutionID: caller.ExecutionID}
	}
	return map[string]BrokerOp{
		"review_pass.feedback": func(ctx context.Context, caller BrokerCaller, _ json.RawMessage) (any, error) {
			view, err := service.OwnReviewFeedback(ctx, db, answerer(caller), service.ReviewFeedbackOptions{HasSkill: hasSkill})
			if err != nil {
				return nil, reviewPassRefusal(err)
			}
			return view, nil
		},
		"review_pass.answer": func(ctx context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
			var args struct {
				Item   string `json:"item"`
				Answer string `json:"answer"`
				Note   string `json:"note"`
			}
			if err := json.Unmarshal(raw, &args); err != nil || args.Item == "" || args.Answer == "" {
				return nil, refuse("bad_request", "an item and its answer are required")
			}
			loopTask, err := service.AnswerOwnReviewItem(ctx, db, answerer(caller), args.Item, args.Answer, args.Note, time.Now())
			if err != nil {
				return nil, reviewPassRefusal(err)
			}
			return ReviewPassAnswerResult{TaskID: loopTask, Item: args.Item, Answer: args.Answer}, nil
		},
	}
}

// reviewPassRefusal is a review-pass error as the broker reports it: a
// caller who may not act is forbidden, anything else rejected.
func reviewPassRefusal(err error) error {
	if errors.Is(err, service.ErrReviewAnswerRefused) {
		return refuse("forbidden", err.Error())
	}
	return refuse("rejected", err.Error())
}
