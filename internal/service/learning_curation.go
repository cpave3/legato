package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

const (
	LearningPacketSchemaVersion    = 1
	CuratorOperationsSchemaVersion = 1
	MaxCuratorOperations           = 128
	MaxCuratorDocumentBytes        = 256 * 1024
	MaxLearningClaimBytes          = 8 * 1024
	MaxLearningRationaleBytes      = 8 * 1024
	MaxLearningKindBytes           = 128
)

var (
	ErrInvalidLearningPacket    = errors.New("invalid learning packet")
	ErrInvalidCuratorOperations = errors.New("invalid curator operations")
	strictLearningID            = regexp.MustCompile(`^learn-[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	strictEvidenceID            = regexp.MustCompile(`^lev-[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type LearningPacketContext struct {
	TaskID       string `json:"task_id,omitempty"`
	ProjectKey   string `json:"project_key,omitempty"`
	ProjectLabel string `json:"project_label,omitempty"`
}

type LearningPacketExclusion struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// LearningPacket is canonical JSON persisted before any curator is launched.
// Contents are untrusted attributable records; callers should preserve these
// boundaries when rendering a prompt.
type LearningPacket struct {
	SchemaVersion      int                       `json:"schema_version"`
	PacketDigest       string                    `json:"packet_digest"`
	DataClassification string                    `json:"data_classification"`
	Context            LearningPacketContext     `json:"context"`
	Evidence           []store.LearningEvidence  `json:"evidence"`
	Learnings          []store.Learning          `json:"learnings"`
	Exclusions         []LearningPacketExclusion `json:"exclusions"`
}

type PrepareLearningPacketInput struct {
	WorkflowRunID string
	AttemptID     string
	TaskID        string
	ProjectKey    string
	ProjectLabel  string
	MaxItems      int
	MaxBytes      int
}

type PreparedLearningPacket struct {
	Generation *store.LearningCurationGeneration `json:"generation"`
	Packet     LearningPacket                    `json:"packet"`
}

// PrepareLearningPacket selects whole records in stable source order and
// records every budget exclusion. It never truncates record content.
func (s *LearningService) PrepareLearningPacket(ctx context.Context, input PrepareLearningPacketInput) (*PreparedLearningPacket, error) {
	input.WorkflowRunID = strings.TrimSpace(input.WorkflowRunID)
	input.AttemptID = strings.TrimSpace(input.AttemptID)
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.ProjectKey = strings.TrimSpace(input.ProjectKey)
	input.ProjectLabel = strings.TrimSpace(input.ProjectLabel)
	if input.AttemptID == "" || input.MaxItems <= 0 || input.MaxBytes <= 0 {
		return nil, fmt.Errorf("%w: attempt_id and positive item/byte limits are required", ErrInvalidLearningPacket)
	}
	// Automatic workflow curation normally supplies a task but no explicit
	// project. Resolve it from the task using the shared identity policy.
	if input.ProjectKey == "" && input.TaskID != "" {
		task, taskErr := s.store.GetTask(ctx, input.TaskID)
		if taskErr == nil {
			identity, resolved, resolveErr := s.projectIdentityForTask(task)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if resolved {
				input.ProjectKey, input.ProjectLabel = identity.Key, identity.Label
			}
		} else if !errors.Is(taskErr, store.ErrNotFound) {
			return nil, taskErr
		}
	}
	var project *ProjectIdentity
	if input.ProjectKey != "" || input.ProjectLabel != "" {
		project = &ProjectIdentity{Key: input.ProjectKey, Label: input.ProjectLabel}
	}
	if input.TaskID != "" {
		if err := s.CollectTaskEvidence(ctx, input.TaskID, project); err != nil {
			return nil, err
		}
	}
	evidence, err := s.store.ListUnlinkedLearningEvidence(ctx, input.TaskID, input.ProjectKey)
	if err != nil {
		return nil, err
	}
	learnings, err := s.store.ListRelevantLearnings(ctx, input.ProjectKey)
	if err != nil {
		return nil, err
	}
	for _, item := range evidence {
		if !strictEvidenceID.MatchString(item.ID) || strings.TrimSpace(item.Author) == "" {
			return nil, fmt.Errorf("%w: evidence %q has an invalid ID or author", ErrInvalidLearningPacket, item.ID)
		}
	}
	for _, item := range learnings {
		if !strictLearningID.MatchString(item.ID) {
			return nil, fmt.Errorf("%w: learning %q has an invalid ID", ErrInvalidLearningPacket, item.ID)
		}
	}

	packet := LearningPacket{SchemaVersion: LearningPacketSchemaVersion, DataClassification: "untrusted",
		Context:  LearningPacketContext{TaskID: input.TaskID, ProjectKey: input.ProjectKey, ProjectLabel: input.ProjectLabel},
		Evidence: []store.LearningEvidence{}, Learnings: []store.Learning{}, Exclusions: []LearningPacketExclusion{}}
	type candidate struct {
		kind, id string
		evidence *store.LearningEvidence
		learning *store.Learning
	}
	candidates := make([]candidate, 0, len(evidence)+len(learnings))
	for i := range evidence {
		candidates = append(candidates, candidate{kind: "evidence", id: evidence[i].ID, evidence: &evidence[i]})
	}
	for i := range learnings {
		candidates = append(candidates, candidate{kind: "learning", id: learnings[i].ID, learning: &learnings[i]})
	}
	// Store queries define relevance order. This final tie breaker protects
	// deterministic output if a store implementation ever returns equal rows.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].kind != candidates[j].kind {
			return candidates[i].kind < candidates[j].kind
		}
		return candidates[i].id < candidates[j].id
	})
	for _, item := range candidates {
		reason := "item_limit"
		if len(packet.Evidence)+len(packet.Learnings) < input.MaxItems {
			trial := packet
			trial.Evidence = append([]store.LearningEvidence{}, packet.Evidence...)
			trial.Learnings = append([]store.Learning{}, packet.Learnings...)
			if item.evidence != nil {
				trial.Evidence = append(trial.Evidence, *item.evidence)
			} else {
				trial.Learnings = append(trial.Learnings, *item.learning)
			}
			if packetJSONSize(trial) <= input.MaxBytes {
				packet = trial
				continue
			}
			reason = "byte_limit"
		}
		packet.Exclusions = append(packet.Exclusions, LearningPacketExclusion{Kind: item.kind, ID: item.id, Reason: reason})
	}
	// Exclusion records also consume budget. If they push the final document over
	// the limit, evict whole records in reverse selection order until it fits.
	for packetJSONSize(packet) > input.MaxBytes && (len(packet.Learnings) > 0 || len(packet.Evidence) > 0) {
		if n := len(packet.Learnings); n > 0 {
			item := packet.Learnings[n-1]
			packet.Learnings = packet.Learnings[:n-1]
			packet.Exclusions = append(packet.Exclusions, LearningPacketExclusion{Kind: "learning", ID: item.ID, Reason: "byte_limit"})
		} else {
			n := len(packet.Evidence)
			item := packet.Evidence[n-1]
			packet.Evidence = packet.Evidence[:n-1]
			packet.Exclusions = append(packet.Exclusions, LearningPacketExclusion{Kind: "evidence", ID: item.ID, Reason: "byte_limit"})
		}
	}
	sort.Slice(packet.Exclusions, func(i, j int) bool {
		if packet.Exclusions[i].Kind != packet.Exclusions[j].Kind {
			return packet.Exclusions[i].Kind < packet.Exclusions[j].Kind
		}
		return packet.Exclusions[i].ID < packet.Exclusions[j].ID
	})
	packet.PacketDigest, _, err = learningPacketJSON(packet)
	if err != nil {
		return nil, err
	}
	_, encoded, err := learningPacketJSON(packet)
	if err != nil {
		return nil, err
	}
	if len(encoded) > input.MaxBytes {
		return nil, fmt.Errorf("%w: limits cannot contain context and exclusion manifest (%d > %d bytes)", ErrInvalidLearningPacket, len(encoded), input.MaxBytes)
	}
	generation := store.LearningCurationGeneration{ID: newLearningID("lcur-"), WorkflowRunID: input.WorkflowRunID,
		AttemptID: input.AttemptID, PacketDigest: packet.PacketDigest, PacketJSON: string(encoded), OperationsJSON: "[]", Status: store.CurationGenerationPrepared}
	generationPtr, err := s.store.CreateLearningCurationGeneration(ctx, generation)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("%w: attempt already has a different packet", ErrInvalidLearningPacket)
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(generationPtr.PacketJSON), &packet); err != nil {
		return nil, err
	}
	return &PreparedLearningPacket{Generation: generationPtr, Packet: packet}, nil
}

func packetJSONSize(packet LearningPacket) int { _, b, _ := learningPacketJSON(packet); return len(b) }
func learningPacketJSON(packet LearningPacket) (string, []byte, error) {
	packet.PacketDigest = ""
	unsigned, err := json.Marshal(packet)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(unsigned)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	packet.PacketDigest = digest
	encoded, err := json.Marshal(packet)
	return digest, encoded, err
}

type CuratorOperationsDocument struct {
	SchemaVersion int                `json:"schema_version"`
	PacketDigest  string             `json:"packet_digest"`
	Operations    []CuratorOperation `json:"operations"`
}

type CuratorOperation struct {
	Operation           string   `json:"operation"`
	LearningID          string   `json:"learning_id,omitempty"`
	NewLearningID       string   `json:"new_learning_id,omitempty"`
	EvidenceIDs         []string `json:"evidence_ids,omitempty"`
	Claim               string   `json:"claim,omitempty"`
	Kind                string   `json:"kind,omitempty"`
	Rationale           string   `json:"rationale,omitempty"`
	Scope               string   `json:"scope,omitempty"`
	ProjectKey          string   `json:"project_key,omitempty"`
	Confidence          float64  `json:"confidence,omitempty"`
	CanonicalLearningID string   `json:"canonical_learning_id,omitempty"`
	Destination         string   `json:"destination,omitempty"`
	Target              string   `json:"target,omitempty"`
	Action              string   `json:"action,omitempty"`
	Reason              string   `json:"reason,omitempty"`
}

type CuratorSubmission struct {
	Generation *store.LearningCurationGeneration `json:"generation"`
	Replayed   bool                              `json:"replayed"`
}

// SubmitCuratorOperationsJSON strictly decodes untrusted curator output.
func (s *LearningService) SubmitCuratorOperationsJSON(ctx context.Context, attemptID string, data []byte) (*CuratorSubmission, error) {
	if len(data) > MaxCuratorDocumentBytes {
		return nil, fmt.Errorf("%w: document too large", ErrInvalidCuratorOperations)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document CuratorOperationsDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCuratorOperations, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing JSON", ErrInvalidCuratorOperations)
	}
	return s.SubmitCuratorOperations(ctx, attemptID, document)
}

func (s *LearningService) SubmitCuratorOperations(ctx context.Context, attemptID string, document CuratorOperationsDocument) (*CuratorSubmission, error) {
	attemptID = strings.TrimSpace(attemptID)
	generation, err := s.store.GetLearningCurationGenerationByAttempt(ctx, attemptID)
	if errors.Is(err, store.ErrNotFound) {
		// Workflow definitions commonly carry the packet executor's generation_id
		// rather than its internal attempt ID. Both are stable references.
		generation, err = s.store.GetLearningCurationGeneration(ctx, attemptID)
		if err == nil {
			attemptID = generation.AttemptID
		}
	}
	if err != nil {
		return nil, err
	}
	var packet LearningPacket
	if err := json.Unmarshal([]byte(generation.PacketJSON), &packet); err != nil {
		return nil, err
	}
	if document.SchemaVersion != CuratorOperationsSchemaVersion || document.PacketDigest != generation.PacketDigest || document.PacketDigest != packet.PacketDigest || len(document.Operations) > MaxCuratorOperations {
		return nil, fmt.Errorf("%w: schema, packet digest, or operation count mismatch", ErrInvalidCuratorOperations)
	}
	evidenceIDs := map[string]bool{}
	learningIDs := map[string]bool{}
	learningRevisions := map[string]int{}
	learningScopes := map[string]string{}
	for _, e := range packet.Evidence {
		evidenceIDs[e.ID] = true
	}
	for _, l := range packet.Learnings {
		learningIDs[l.ID] = true
		learningRevisions[l.ID] = l.Revision
		learningScopes[l.ID] = l.Scope
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	apply := store.LearningCurationApply{GenerationID: generation.ID, AttemptID: attemptID, PacketDigest: generation.PacketDigest, OperationsJSON: string(encoded)}
	newIDs := map[string]bool{}
	for i := range document.Operations {
		op := &document.Operations[i]
		normalizeOperation(op)
		if err := validateCuratorOperation(op, evidenceIDs, learningIDs, learningScopes, newIDs, packet.Context.ProjectKey); err != nil {
			return nil, fmt.Errorf("%w: operation %d: %v", ErrInvalidCuratorOperations, i, err)
		}
		if op.Operation == "revise" {
			view, viewErr := s.store.GetLearningView(ctx, op.LearningID)
			if viewErr != nil {
				return nil, viewErr
			}
			if len(view.Evidence) == 0 {
				return nil, fmt.Errorf("%w: operation %d: revise requires an evidence-backed learning", ErrInvalidCuratorOperations, i)
			}
		}
		switch op.Operation {
		case "create":
			learning := store.Learning{ID: op.NewLearningID, Scope: op.Scope, ProjectKey: op.ProjectKey, Kind: op.Kind, Claim: op.Claim,
				Rationale: op.Rationale, Status: "candidate", Confidence: op.Confidence, Revision: 1, CuratorRunID: generation.ID}
			apply.Learnings = append(apply.Learnings, learning)
			for _, id := range op.EvidenceIDs {
				apply.Links = append(apply.Links, store.LearningEvidenceLink{LearningID: learning.ID, EvidenceID: id, Relation: "support"})
			}
		case "reinforce", "counter":
			relation := "support"
			if op.Operation == "counter" {
				relation = "counter"
			}
			for _, id := range op.EvidenceIDs {
				apply.Links = append(apply.Links, store.LearningEvidenceLink{LearningID: op.LearningID, EvidenceID: id, Relation: relation})
			}
		case "destination":
			apply.Incorporations = append(apply.Incorporations, store.LearningIncorporation{ID: newLearningID("linc-"), LearningID: op.LearningID,
				Destination: op.Destination, Target: op.Target, Action: op.Action, Status: "proposed", Revision: 1, ReferencesJSON: "[]"})
		case "duplicate", "supersede", "scope", "revise", "discard":
			evidenceJSON, _ := json.Marshal(op.EvidenceIDs)
			apply.Proposals = append(apply.Proposals, store.LearningProposal{ID: newLearningID("lprop-"), GenerationID: generation.ID,
				Operation: op.Operation, LearningID: op.LearningID, CanonicalLearningID: op.CanonicalLearningID,
				Scope: op.Scope, ProjectKey: op.ProjectKey, Claim: op.Claim, Rationale: op.Rationale, Kind: op.Kind,
				EvidenceIDsJSON: string(evidenceJSON), Reason: op.Reason,
				Status: "pending", Revision: 1, TargetLearningRevision: learningRevisions[op.LearningID]})
		}
	}
	replayed, err := s.store.ApplyLearningCuration(ctx, apply)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("%w: attempt already completed with different operations", ErrInvalidCuratorOperations)
		}
		return nil, err
	}
	generation, err = s.store.GetLearningCurationGenerationByAttempt(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	return &CuratorSubmission{Generation: generation, Replayed: replayed}, nil
}

func (s *LearningService) ListProposals(ctx context.Context, status string) ([]store.LearningProposal, error) {
	return s.store.ListLearningProposals(ctx, status)
}

func (s *LearningService) DecideProposal(ctx context.Context, id string, expectedRevision int, accept bool) error {
	if err := s.store.ApplyLearningProposalDecision(ctx, id, expectedRevision, accept); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ErrLearningConflict
		}
		return err
	}
	s.publish(id, "proposal_decided")
	return nil
}

func normalizeOperation(op *CuratorOperation) {
	op.Operation = strings.TrimSpace(op.Operation)
	op.LearningID = strings.TrimSpace(op.LearningID)
	op.NewLearningID = strings.TrimSpace(op.NewLearningID)
	op.Claim = strings.TrimSpace(op.Claim)
	op.Kind = strings.TrimSpace(op.Kind)
	op.Rationale = strings.TrimSpace(op.Rationale)
	op.Scope = strings.TrimSpace(op.Scope)
	op.ProjectKey = strings.TrimSpace(op.ProjectKey)
	op.CanonicalLearningID = strings.TrimSpace(op.CanonicalLearningID)
	op.Destination = strings.TrimSpace(op.Destination)
	op.Target = strings.TrimSpace(op.Target)
	op.Action = strings.TrimSpace(op.Action)
	op.Reason = strings.TrimSpace(op.Reason)
	for i := range op.EvidenceIDs {
		op.EvidenceIDs[i] = strings.TrimSpace(op.EvidenceIDs[i])
	}
}

func validateCuratorOperation(op *CuratorOperation, evidence, learnings map[string]bool, learningScopes map[string]string, newIDs map[string]bool, packetProjectKey string) error {
	allowed := map[string]bool{"create": true, "reinforce": true, "counter": true, "duplicate": true, "supersede": true, "scope": true, "revise": true, "destination": true, "discard": true}
	if !allowed[op.Operation] {
		return fmt.Errorf("operation %q is not allowed", op.Operation)
	}
	if len(op.EvidenceIDs) > 128 {
		return errors.New("too many evidence IDs")
	}
	seen := map[string]bool{}
	for _, id := range op.EvidenceIDs {
		if !strictEvidenceID.MatchString(id) || !evidence[id] {
			return fmt.Errorf("evidence ID %q is absent from packet", id)
		}
		if seen[id] {
			return fmt.Errorf("duplicate evidence ID %q", id)
		}
		seen[id] = true
	}
	needsLearning := map[string]bool{"reinforce": true, "counter": true, "duplicate": true, "supersede": true, "scope": true, "revise": true, "destination": true}
	if needsLearning[op.Operation] && (!strictLearningID.MatchString(op.LearningID) || !learnings[op.LearningID]) {
		return fmt.Errorf("learning ID %q is absent from packet", op.LearningID)
	}
	switch op.Operation {
	case "create":
		if !strictLearningID.MatchString(op.NewLearningID) || learnings[op.NewLearningID] || newIDs[op.NewLearningID] {
			return errors.New("new_learning_id must be a unique strict ID")
		}
		newIDs[op.NewLearningID] = true
		if op.Claim == "" || op.Kind == "" || len(op.EvidenceIDs) == 0 {
			return errors.New("create requires claim, kind, and evidence_ids")
		}
		if err := validateLearningProposalText(op); err != nil {
			return err
		}
		if op.Scope != "user" && op.Scope != "project" {
			return errors.New("create scope must be user or project")
		}
		if op.Scope == "project" && (packetProjectKey == "" || op.ProjectKey != packetProjectKey) {
			return errors.New("project scope project_key must exactly match the resolved packet project")
		}
		if op.Confidence < 0 || op.Confidence > 1 {
			return errors.New("confidence must be between zero and one")
		}
	case "reinforce", "counter":
		if len(op.EvidenceIDs) == 0 {
			return errors.New("evidence_ids are required")
		}
	case "duplicate", "supersede":
		if !strictLearningID.MatchString(op.CanonicalLearningID) || !learnings[op.CanonicalLearningID] || op.CanonicalLearningID == op.LearningID {
			return errors.New("canonical_learning_id must name another packet learning")
		}
	case "scope":
		if op.Scope != "user" && op.Scope != "project" {
			return errors.New("scope proposal must be user or project")
		}
		if op.Scope == "project" && (packetProjectKey == "" || op.ProjectKey != packetProjectKey) {
			return errors.New("project scope project_key must exactly match the resolved packet project")
		}
	case "revise":
		if learningScopes[op.LearningID] == "project" && op.Scope != "project" {
			return errors.New("revise cannot broaden a project learning to user scope")
		}
		if op.Claim == "" || op.Kind == "" || op.Reason == "" {
			return errors.New("revise requires claim, kind, and reason")
		}
		if err := validateLearningProposalText(op); err != nil {
			return err
		}
		if op.Scope != "user" && op.Scope != "project" {
			return errors.New("revise scope must be user or project")
		}
		if op.Scope == "project" && (packetProjectKey == "" || op.ProjectKey != packetProjectKey) {
			return errors.New("revise project_key must exactly match the resolved packet project")
		}
	case "destination":
		if op.Destination == "" {
			return errors.New("destination is required")
		}
	case "discard":
		if len(op.EvidenceIDs) == 0 || op.Reason == "" {
			return errors.New("discard requires evidence_ids and reason")
		}
	}
	return nil
}

func validateLearningProposalText(op *CuratorOperation) error {
	if len([]byte(op.Claim)) > MaxLearningClaimBytes || len([]byte(op.Rationale)) > MaxLearningRationaleBytes || len([]byte(op.Kind)) > MaxLearningKindBytes {
		return errors.New("claim, rationale, or kind exceeds field bounds")
	}
	return nil
}
