package store

import (
	"context"
	"testing"
)

func TestArtifactClaimHandoffRequeuesUnacknowledgedInteractions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"author", "successor"} {
		if err := s.CreateTask(ctx, Task{ID: id, Title: id, Status: "Todo"}); err != nil {
			t.Fatal(err)
		}
	}

	first, previous, err := s.ClaimArtifact(ctx, "plan", "pl-board", "author")
	if err != nil || previous != nil || first.Generation != 1 {
		t.Fatalf("first claim = %+v previous=%+v err=%v", first, previous, err)
	}
	if err := s.InsertArtifactInteraction(ctx, ArtifactInteraction{
		ID: "pi-1", ArtifactKind: "plan", ArtifactID: "pl-board", Kind: "feedback",
		Body: "revise it", ClaimantTaskID: "author", ClaimGeneration: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkArtifactInteractionDelivery(ctx, "pi-1", true, ""); err != nil {
		t.Fatal(err)
	}

	current, previous, err := s.ClaimArtifact(ctx, "plan", "pl-board", "successor")
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != 2 || current.ClaimantTaskID != "successor" || previous == nil || previous.ClaimantTaskID != "author" {
		t.Fatalf("handoff current=%+v previous=%+v", current, previous)
	}
	pending, err := s.ListUndeliveredArtifactInteractions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ClaimantTaskID != "successor" || pending[0].ClaimGeneration != 2 {
		t.Fatalf("pending after handoff = %+v", pending)
	}
	if err := s.AcknowledgeArtifactInteractions(ctx, "successor", []string{"pi-1"}); err != nil {
		t.Fatal(err)
	}
	pending, err = s.ListUndeliveredArtifactInteractions(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after ack = %+v err=%v", pending, err)
	}
}
