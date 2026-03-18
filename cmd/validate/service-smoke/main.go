package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func main() {
	dir, err := os.MkdirTemp("", "legato-validate-*")
	if err != nil {
		fatal("creating temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "validate.db")
	s, err := store.New(dbPath)
	if err != nil {
		fatal("opening store: %v", err)
	}
	defer s.Close()

	bus := events.New()
	syncSvc := service.NewStubSyncService(s, bus)
	boardSvc := service.NewBoardService(s, bus)
	ctx := context.Background()

	// 1. Sync
	fmt.Println("=== Sync ===")
	result, err := syncSvc.Sync(ctx)
	if err != nil {
		fatal("sync: %v", err)
	}
	fmt.Printf("Synced %d tickets\n", result.TicketsSynced)

	status := syncSvc.Status()
	fmt.Printf("Last sync: %s, In progress: %v\n\n", status.LastSync.Format("15:04:05"), status.InProgress)

	// 2. List columns
	fmt.Println("=== Columns ===")
	cols, err := boardSvc.ListColumns(ctx)
	if err != nil {
		fatal("list columns: %v", err)
	}
	for _, col := range cols {
		fmt.Printf("  [%d] %s\n", col.SortOrder, col.Name)
	}
	fmt.Println()

	// 3. List cards per column
	fmt.Println("=== Board ===")
	for _, col := range cols {
		cards, err := boardSvc.ListCards(ctx, col.Name)
		if err != nil {
			fatal("list cards for %q: %v", col.Name, err)
		}
		fmt.Printf("%s (%d cards):\n", col.Name, len(cards))
		for _, card := range cards {
			fmt.Printf("  %s  %-50s  [%s] %s\n", card.ID, card.Summary, card.Priority, card.IssueType)
		}
	}
	fmt.Println()

	// 4. Get card detail
	fmt.Println("=== Card Detail (LEG-3) ===")
	detail, err := boardSvc.GetCard(ctx, "LEG-3")
	if err != nil {
		fatal("get card: %v", err)
	}
	fmt.Printf("ID: %s\nSummary: %s\nStatus: %s\nPriority: %s\nType: %s\nAssignee: %s\n",
		detail.ID, detail.Summary, detail.Status, detail.Priority, detail.IssueType, detail.Assignee)
	fmt.Println()

	// 5. Move card
	fmt.Println("=== Move LEG-3 to Done ===")
	if err := boardSvc.MoveCard(ctx, "LEG-3", "Done"); err != nil {
		fatal("move card: %v", err)
	}
	movedCard, _ := boardSvc.GetCard(ctx, "LEG-3")
	fmt.Printf("LEG-3 now in: %s\n\n", movedCard.Status)

	// 6. Search
	fmt.Println("=== Search: 'jira' ===")
	results, err := boardSvc.SearchCards(ctx, "jira")
	if err != nil {
		fatal("search: %v", err)
	}
	for _, card := range results {
		fmt.Printf("  %s: %s\n", card.ID, card.Summary)
	}
	fmt.Println()

	// 7. Export
	fmt.Println("=== Export (description-only) LEG-7 ===")
	descOut, err := boardSvc.ExportCardContext(ctx, "LEG-7", service.ExportFormatDescription)
	if err != nil {
		fatal("export desc: %v", err)
	}
	fmt.Println(descOut)

	fmt.Println("=== Export (full) LEG-7 ===")
	fullOut, err := boardSvc.ExportCardContext(ctx, "LEG-7", service.ExportFormatFull)
	if err != nil {
		fatal("export full: %v", err)
	}
	fmt.Println(fullOut)

	fmt.Println("=== Phase 2 Validation Complete ===")
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", args...)
	os.Exit(1)
}
