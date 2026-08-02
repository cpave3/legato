package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

func runAttentionCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: legato attention list [--producer <name>] [--category <cat>] [--urgency <level>] [--json]")
		return 1
	}
	verb := args[0]
	if verb != "list" {
		fmt.Fprintf(os.Stderr, "unknown attention command: %s\n", verb)
		return 1
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	db, err := openStore(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		return 1
	}
	defer db.Close()

	_, flags, _, parseErr := parseReviewArgs(args[1:])
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", parseErr)
		return exitUsage
	}
	_, jsonMode := flags["json"]

	attention := service.NewAttentionService(db, nil)
	// Register producers so the CLI can reconcile without a running server.
	planSvc := service.NewPlanService(db, nil, nil)
	reviewSvc := service.NewReviewService(db, nil, nil)
	_ = attention.RegisterProducerWithEvents(service.NewPlanAttentionAdapter(planSvc))
	_ = attention.RegisterProducerWithEvents(service.NewReviewAttentionAdapter(reviewSvc))
	_ = attention.RegisterProducerWithEvents(service.NewLearningAttentionAdapter(service.NewLearningService(db, nil)))
	_ = attention.ReconcileAll(context.Background())
	items, err := attention.List(context.Background(), service.AttentionFilter{
		Producer: flags["producer"],
		Category: flags["category"],
		Urgency:  flags["urgency"],
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if jsonMode {
		out, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(out))
		return 0
	}

	if len(items) == 0 {
		fmt.Println("No items need attention.")
		return 0
	}

	fmt.Printf("%d item%s need attention:\n\n", len(items), plural(len(items)))
	for _, item := range items {
		fmt.Printf("  %s — %s [%s/%s] %s\n", item.Title, item.Reason, item.Producer, item.Urgency, relativeAge(item.UpdatedAt))
		for _, action := range item.Actions {
			if action.Kind == "execute" {
				fmt.Printf("    action: %s\n", action.Label)
			}
		}
	}
	return 0
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func relativeAge(iso string) string {
	t, err := time.Parse("2006-01-02 15:04:05", iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
		if err != nil {
			return "?"
		}
	}
	d := time.Since(t)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return "just now"
	}
}
