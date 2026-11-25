package service

import (
	"context"
	"testing"
)

// Importing a ticket says whether it put it on the board; a ticket already
// there stays in its column, and an archived one is restored (LEG-488).
func TestImportingATicketSaysWhetherItWasCreated(t *testing.T) {
	fake := buildMilestoneBurnlite()
	svc, s := newSourcesTest(t, fake.source(t))
	importer := svc.(TicketImporter)
	ctx := context.Background()

	first, err := importer.ImportTicket(ctx, "burnlite", "EST-10", ImportRemoteTaskOptions{Column: "doing"})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.Restored || first.Card.ID != "EST-10" || first.Card.Status != "Doing" {
		t.Fatalf("first = %+v, card %+v", first, first.Card)
	}

	moved, _ := s.GetTask(ctx, "EST-10")
	moved.Status = "Review"
	if err := s.UpdateTask(ctx, *moved); err != nil {
		t.Fatal(err)
	}
	again, err := importer.ImportTicket(ctx, "burnlite", "EST-10", ImportRemoteTaskOptions{Column: "Backlog"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Restored || again.Card.Status != "Review" {
		t.Fatalf("again = %+v, card %+v", again, again.Card)
	}

	if err := s.ArchiveTask(ctx, "EST-10"); err != nil {
		t.Fatal(err)
	}
	restored, err := importer.ImportTicket(ctx, "burnlite", "EST-10", ImportRemoteTaskOptions{Column: "Backlog"})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Created || !restored.Restored || restored.Card.Status != "Review" {
		t.Fatalf("restored = %+v, card %+v", restored, restored.Card)
	}
	task, err := s.GetTask(ctx, "EST-10")
	if err != nil || task.ArchivedAt != nil || task.Status != "Review" {
		t.Fatalf("task = %+v, %v", task, err)
	}
}
