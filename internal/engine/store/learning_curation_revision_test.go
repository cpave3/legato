package store

import (
	"context"
	"errors"
	"testing"
)

func TestStaleLearningProposalAcceptConflictsAndStaysPending(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.DB().Exec(`INSERT INTO learnings (id,scope,kind,claim,status,revision) VALUES ('learn-old','user','fact','original','candidate',1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`INSERT INTO learning_curation_generations (id,attempt_id,packet_digest,packet_json,status) VALUES ('gen','attempt','digest','{}','completed')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`INSERT INTO learning_proposals (id,generation_id,operation,learning_id,scope,status,revision,target_learning_revision) VALUES ('proposal','gen','scope','learn-old','project','pending',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateLearningDecision(ctx, "learn-old", 1, "validated", "edited", "", ""); err != nil {
		t.Fatal(err)
	}

	if err := s.ApplyLearningProposalDecision(ctx, "proposal", 1, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v", err)
	}
	var learning Learning
	if err := s.DB().Get(&learning, `SELECT * FROM learnings WHERE id = 'learn-old'`); err != nil {
		t.Fatal(err)
	}
	if learning.Claim != "edited" || learning.Status != "validated" || learning.Scope != "user" || learning.Revision != 2 {
		t.Fatalf("learning changed: %+v", learning)
	}
	var proposal LearningProposal
	if err := s.DB().Get(&proposal, `SELECT * FROM learning_proposals WHERE id = 'proposal'`); err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "pending" || proposal.Revision != 1 {
		t.Fatalf("proposal changed: %+v", proposal)
	}
}

func TestRejectedReviseProposalLeavesLearningUnchanged(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.DB().Exec(`INSERT INTO learnings (id,scope,kind,claim,rationale,status,revision) VALUES ('learn-revise','user','fact','original','original reason','candidate',1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`INSERT INTO learning_curation_generations (id,attempt_id,packet_digest,packet_json,status) VALUES ('gen-revise','attempt-revise','digest','{}','completed')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`INSERT INTO learning_proposals (id,generation_id,operation,learning_id,scope,claim,rationale,kind,reason,status,revision,target_learning_revision) VALUES ('proposal-revise','gen-revise','revise','learn-revise','user','general','general reason','pitfall','improve transfer','pending',1,1)`)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ApplyLearningProposalDecision(ctx, "proposal-revise", 1, false); err != nil {
		t.Fatal(err)
	}
	var learning Learning
	if err := s.DB().Get(&learning, `SELECT * FROM learnings WHERE id = 'learn-revise'`); err != nil {
		t.Fatal(err)
	}
	if learning.Claim != "original" || learning.Rationale != "original reason" || learning.Kind != "fact" || learning.Revision != 1 {
		t.Fatalf("learning changed: %+v", learning)
	}
}

func TestDiscardProposalDoesNotRequireOrChangeLearning(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.DB().Exec(`INSERT INTO learning_curation_generations (id,attempt_id,packet_digest,packet_json,status) VALUES ('gen','attempt','digest','{}','completed')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`INSERT INTO learning_proposals (id,generation_id,operation,status,revision,target_learning_revision) VALUES ('discard','gen','discard','pending',1,0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyLearningProposalDecision(ctx, "discard", 1, true); err != nil {
		t.Fatal(err)
	}
}
