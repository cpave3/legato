package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/service"
)

// runReviewPassListContract is `legato review-pass list`: every review loop
// Ship looks after, as the Shipping page and GET /api/shipping list them
// (LEG-706). It reads the loop records, so it needs no running Legato.
func runReviewPassListContract(args []string) int {
	const command = "review-pass.list"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("review-pass", "list"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	engine := service.NewReviewPassService(db, service.ReviewPassOptions{Config: reviewPassConfig(cfg)})
	view, err := service.NewReviewRunCaller(engine, service.ReviewRunOptions{}).Shipping(context.Background())
	if err != nil {
		return renderCommandError(command, &commandError{Code: "review_loops_unreadable", Message: err.Error(), Exit: exitInternal}, jsonMode)
	}
	return writeCommandSuccess(command, view, shippingText(view), jsonMode)
}

// shippingText is one line per loop: its phase, its pull requests, its task
// and its ID.
func shippingText(view service.ShippingView) string {
	if len(view.Loops) == 0 {
		return "No review loops."
	}
	var b strings.Builder
	for _, loop := range view.Loops {
		numbers := make([]string, 0, len(loop.Layers))
		for _, layer := range loop.Layers {
			numbers = append(numbers, fmt.Sprintf("#%d", layer.Number))
		}
		phase := loop.Phase
		if loop.Outcome != "" && phase == service.ShippingEnded {
			phase += " (" + loop.Outcome + ")"
		}
		if loop.NeedsYou {
			phase += ", needs you"
		}
		task := ""
		if loop.Task != nil {
			task = loop.Task.ID
		}
		fmt.Fprintf(&b, "%-28s %s%s  %s  %s\n", phase, loop.Repo, strings.Join(numbers, ","), task, loop.LoopID)
	}
	return b.String()
}
