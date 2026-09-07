package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	gitpkg "github.com/cpave3/legato/internal/engine/git"
	"github.com/cpave3/legato/internal/engine/store"
)

const (
	maxHandoffString      = 16 * 1024
	maxHandoffSourceBytes = 64 * 1024
	maxHandoffSources     = 128
	maxHandoffItems       = 128
)

// DecodeHandoff decodes exactly one bounded, strict JSON authoring packet.
func DecodeHandoff(reader io.Reader) (HandoffInput, error) {
	limited := io.LimitReader(reader, MaxHandoffBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return HandoffInput{}, err
	}
	if len(raw) > MaxHandoffBytes {
		return HandoffInput{}, validationError("handoff packet exceeds %d bytes", MaxHandoffBytes)
	}
	if !utf8.Valid(raw) {
		return HandoffInput{}, validationError("handoff packet must be valid UTF-8")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var input HandoffInput
	if err := decoder.Decode(&input); err != nil {
		return HandoffInput{}, validationError("invalid handoff JSON: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return HandoffInput{}, validationError("handoff JSON must contain one value")
		}
		return HandoffInput{}, validationError("invalid trailing handoff JSON: %v", err)
	}
	return input, nil
}

func (r *ReviewService) SubmitHandoff(ctx context.Context, tourID string, input HandoffInput) (*HandoffPacket, error) {
	if err := validateHandoffInput(&input); err != nil {
		return nil, err
	}
	_, task, err := r.tourTask(ctx, tourID)
	if err != nil {
		return nil, err
	}
	boundary, err := r.store.ReviewHandoffBoundary(ctx, tourID)
	if err != nil {
		return nil, err
	}
	if boundary.TourStatus != "ready" || boundary.PassStatus != "ready" || boundary.PassID != input.PassID ||
		boundary.BaseSHA != input.BaseSHA || boundary.TourHeadSHA != input.HeadSHA || boundary.PassHeadSHA != input.HeadSHA {
		return nil, validationError("handoff must exactly match the clean ready review pass, base, and head")
	}
	repo, err := r.repositoryForTask(ctx, task)
	if err != nil {
		return nil, err
	}
	if err := ensureCleanHead(ctx, repo, input.HeadSHA); err != nil {
		return nil, err
	}
	changedFiles, err := gitpkg.ChangedFiles(ctx, repo, input.BaseSHA, input.HeadSHA)
	if err != nil {
		return nil, err
	}
	if err := normalizeCoverage(&input, changedFiles); err != nil {
		return nil, err
	}

	steps, err := r.store.ListReviewStepsByPass(ctx, input.PassID)
	if err != nil {
		return nil, err
	}
	chapters := make([]HandoffChapter, 0)
	chapterIDs := map[string]bool{}
	for _, step := range steps {
		if step.Kind == "chapter" {
			chapters = append(chapters, HandoffChapter{ID: step.ID, Title: step.Title, Narration: step.Narration})
			chapterIDs[step.ID] = true
		}
	}
	if err := validateChapterReferences(input.Concepts, chapterIDs); err != nil {
		return nil, err
	}

	sources := make([]HandoffSource, 0, len(input.Sources))
	totalSourceBytes := 0
	for _, source := range input.Sources {
		commit := input.HeadSHA
		if source.Side == "base" {
			commit = input.BaseSHA
		}
		text, err := gitpkg.ReadBlobExcerpt(ctx, repo, commit, source.Path, source.LineStart, source.LineEnd, maxHandoffSourceBytes)
		if err != nil {
			return nil, validationError("source %q: %v", source.ID, err)
		}
		totalSourceBytes += len(text)
		if totalSourceBytes > MaxHandoffBytes {
			return nil, validationError("captured source excerpts exceed %d bytes", MaxHandoffBytes)
		}
		digest := sha256.Sum256([]byte(text))
		sources = append(sources, HandoffSource{HandoffSourceInput: source, CommitSHA: commit, Text: text, Digest: "sha256:" + hex.EncodeToString(digest[:])})
	}

	repository := ""
	if checkout, inspectErr := gitpkg.InspectCheckout(ctx, repo); inspectErr == nil {
		repository = "repo:github.com/" + strings.ToLower(checkout.Repo)
	} else {
		identity, identityErr := NewProjectIdentityResolver(nil).Resolve(nil, repo)
		if identityErr != nil {
			return nil, identityErr
		}
		repository = identity.Key
	}

	packet := HandoffPacket{
		ID: generateReviewID("hp-"), Revision: input.ExpectedRevision + 1,
		Repository: repository, Input: input, Sources: sources, Chapters: chapters,
		Note: emptyHandoffNote(),
	}
	if plan, planErr := r.store.GetReviewPassPlan(ctx, input.PassID); planErr == nil {
		packet.PlanID, packet.PlanRevision, packet.PlanMarkdown = plan.PlanID, plan.RevisionID, plan.Markdown
	} else if !errors.Is(planErr, store.ErrNotFound) {
		return nil, planErr
	}
	packet.Comparison, err = r.compareHandoff(ctx, repo, tourID, &packet, changedFiles)
	if err != nil {
		return nil, err
	}
	if err := ensureCleanHead(ctx, repo, input.HeadSHA); err != nil {
		return nil, ErrHandoffConflict
	}
	raw, err := json.Marshal(packet)
	if err != nil {
		return nil, err
	}
	record := store.HandoffPacketRecord{ID: packet.ID, TourID: tourID, Revision: packet.Revision,
		PassID: input.PassID, BaseSHA: input.BaseSHA, HeadSHA: input.HeadSHA, Repository: repository,
		BoundaryFingerprint: boundary.Fingerprint, PacketJSON: string(raw)}
	if err := r.store.InsertHandoffPacket(ctx, record, input.ExpectedRevision, *boundary); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrHandoffConflict
		}
		return nil, err
	}
	stored, err := r.store.GetHandoffPacket(ctx, tourID, packet.ID)
	if err != nil {
		return nil, err
	}
	r.publish(tourID, "", "handoff_submitted")
	return decodeStoredHandoff(*stored)
}

func (r *ReviewService) Handoff(ctx context.Context, tourID string) (*HandoffView, error) {
	packets, err := r.store.ListHandoffPackets(ctx, tourID)
	if err != nil {
		return nil, err
	}
	view := &HandoffView{TourID: tourID, Packets: []HandoffPacket{}}
	if observation, observationErr := r.store.HandoffObservation(ctx, tourID); observationErr == nil {
		view.CheckedAt = observation.CheckedAt
	} else if !errors.Is(observationErr, store.ErrNotFound) {
		return nil, observationErr
	}
	for _, record := range packets {
		packet, err := decodeStoredHandoff(record)
		if err != nil {
			return nil, err
		}
		view.Packets = append(view.Packets, *packet)
	}
	return view, nil
}

func (r *ReviewService) SaveHandoffNote(ctx context.Context, tourID, packetID string, input HandoffNote) (*HandoffNote, error) {
	record, err := r.store.GetHandoffPacket(ctx, tourID, packetID)
	if err != nil {
		return nil, err
	}
	packet, err := decodeStoredHandoff(*record)
	if err != nil {
		return nil, err
	}
	if err := validateHandoffNote(input, packet.Input.Scenarios); err != nil {
		return nil, err
	}
	expected := input.Revision
	input.Revision = expected + 1
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	next, err := r.store.ReplaceHandoffNote(ctx, tourID, packetID, expected, string(raw))
	if errors.Is(err, store.ErrConflict) {
		return nil, ErrHandoffConflict
	}
	if err != nil {
		return nil, err
	}
	input.Revision = next
	return &input, nil
}

func (r *ReviewService) ExportHandoff(ctx context.Context, tourID string, input HandoffExportInput) (*HandoffExport, error) {
	if input.PacketID == "" {
		return nil, validationError("packet_id is required")
	}
	if input.Format != "github" && input.Format != "maintenance" {
		return nil, validationError("format must be github or maintenance")
	}
	record, err := r.store.GetHandoffPacket(ctx, tourID, input.PacketID)
	if err != nil {
		return nil, err
	}
	packet, err := decodeStoredHandoff(*record)
	if err != nil {
		return nil, err
	}
	selected, err := selectHandoffConcepts(packet.Input.Concepts, input.ConceptIDs)
	if err != nil {
		return nil, err
	}
	markdown := renderHandoffMarkdown(*packet, selected, input)
	warnings := []string{"Preview and inspect this AI-authored snapshot for credentials or sensitive content before sharing.", "PR linkage and current GitHub base/head are unverified; compare this snapshot with the current PR."}
	if packet.Outdated {
		warnings = append(warnings, "This packet is outdated: "+packet.OutdatedReason)
	}
	filename := fmt.Sprintf("handoff-r%d-%s.md", packet.Revision, input.Format)
	return &HandoffExport{Markdown: markdown, Filename: filename, Warnings: warnings}, nil
}

func (r *ReviewService) compareHandoff(ctx context.Context, repo, tourID string, current *HandoffPacket, currentFiles []string) (HandoffComparison, error) {
	records, err := r.store.ListHandoffPackets(ctx, tourID)
	if err != nil {
		return HandoffComparison{}, err
	}
	comparison := HandoffComparison{ChangedConcepts: []string{}, ChangedFiles: []string{}, EvidenceToRecheck: []string{}}
	if len(records) == 0 {
		comparison.Reason = "first handoff packet; review the full snapshot"
		comparison.ChangedConcepts = conceptIDs(current.Input.Concepts)
		comparison.ChangedFiles = append([]string(nil), currentFiles...)
		comparison.EvidenceToRecheck = evidenceIDs(current.Input.Evidence)
		return comparison, nil
	}
	previous, err := decodeStoredHandoff(records[0])
	if err != nil {
		return HandoffComparison{}, err
	}
	comparison.PreviousRevision = previous.Revision
	safe := previous.Repository == current.Repository && previous.Input.BaseSHA == current.Input.BaseSHA
	if safe {
		safe, err = gitpkg.IsAncestor(ctx, repo, previous.Input.HeadSHA, current.Input.HeadSHA)
		if err != nil {
			safe = false
		}
	}
	if !safe {
		comparison.Reason = "snapshots cannot be safely related; perform a full reread"
		comparison.ChangedConcepts = conceptIDs(current.Input.Concepts)
		comparison.ChangedFiles = append([]string(nil), currentFiles...)
		comparison.EvidenceToRecheck = evidenceIDs(current.Input.Evidence)
		return comparison, nil
	}
	comparison.Safe = true
	comparison.ChangedConcepts = changedConceptIDs(previous.Input.Concepts, current.Input.Concepts)
	changedIDs := stringSet(comparison.ChangedConcepts)
	oldSources := map[string]HandoffSource{}
	for _, source := range previous.Sources {
		oldSources[source.ID] = source
	}
	changedSources := map[string]bool{}
	for _, source := range current.Sources {
		if old, ok := oldSources[source.ID]; !ok || old.Digest != source.Digest || old.HandoffSourceInput != source.HandoffSourceInput {
			changedSources[source.ID] = true
		}
	}
	oldEvidence := map[string]HandoffEvidence{}
	for _, evidence := range previous.Input.Evidence {
		oldEvidence[evidence.ID] = evidence
	}
	for _, concept := range current.Input.Concepts {
		changed := previous.Input.Overview != current.Input.Overview
		for _, id := range concept.SourceIDs {
			changed = changed || changedSources[id]
		}
		for _, id := range concept.EvidenceIDs {
			for _, evidence := range current.Input.Evidence {
				if evidence.ID == id {
					changed = changed || !reflect.DeepEqual(oldEvidence[id], evidence)
				}
			}
		}
		if changed && !changedIDs[concept.ID] {
			comparison.ChangedConcepts = append(comparison.ChangedConcepts, concept.ID)
			changedIDs[concept.ID] = true
		}
	}
	comparison.ChangedFiles, err = gitpkg.ChangedFiles(ctx, repo, previous.Input.HeadSHA, current.Input.HeadSHA)
	if err != nil {
		return HandoffComparison{}, err
	}
	changedConcept := stringSet(comparison.ChangedConcepts)
	for _, evidence := range current.Input.Evidence {
		if !reflect.DeepEqual(oldEvidence[evidence.ID], evidence) || evidence.HeadSHA != current.Input.HeadSHA || evidenceTouchesChangedConcept(evidence.ID, current.Input.Concepts, changedConcept) {
			comparison.EvidenceToRecheck = append(comparison.EvidenceToRecheck, evidence.ID)
		}
	}
	return comparison, nil
}

func decodeStoredHandoff(record store.HandoffPacketRecord) (*HandoffPacket, error) {
	var packet HandoffPacket
	if err := json.Unmarshal([]byte(record.PacketJSON), &packet); err != nil {
		return nil, fmt.Errorf("decoding stored handoff packet: %w", err)
	}
	var note HandoffNote
	if err := json.Unmarshal([]byte(record.NoteJSON), &note); err != nil {
		return nil, fmt.Errorf("decoding stored handoff note: %w", err)
	}
	note.Revision = record.NoteRevision
	normalizeHandoffNote(&note)
	packet.ID, packet.Revision, packet.CreatedAt = record.ID, record.Revision, record.CreatedAt
	packet.Repository, packet.Outdated, packet.OutdatedReason, packet.Note = record.Repository, record.Outdated, record.OutdatedReason, note
	normalizePacket(&packet)
	return &packet, nil
}

func (r *ReviewService) observeHandoff(ctx context.Context, tourID, base, repo string) error {
	head, err := gitpkg.Head(ctx, repo)
	if err != nil {
		return err
	}
	status, err := gitpkg.StatusPorcelain(ctx, repo)
	if err != nil {
		return err
	}
	err = r.store.ObserveHandoff(ctx, store.HandoffObservation{
		TourID: tourID, CheckedAt: time.Now().UTC().Format(time.RFC3339),
		ObservedBase: base, ObservedHead: head, Clean: strings.TrimSpace(status) == "",
	})
	if err == nil {
		r.publish(tourID, "", "handoff_synced")
	}
	return err
}

func ensureCleanHead(ctx context.Context, repo, expectedHead string) error {
	head, err := gitpkg.Head(ctx, repo)
	if err != nil {
		return err
	}
	if head != expectedHead {
		return validationError("review head changed: got %s, expected %s", head, expectedHead)
	}
	status, err := gitpkg.StatusPorcelain(ctx, repo)
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return validationError("handoff requires a clean working tree and index")
	}
	return nil
}

func validateHandoffInput(input *HandoffInput) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return validationError("invalid handoff input: %v", err)
	}
	if len(raw) > MaxHandoffBytes {
		return validationError("handoff packet exceeds %d bytes", MaxHandoffBytes)
	}
	if input.SchemaVersion != 1 {
		return validationError("schema_version must be 1")
	}
	if input.ExpectedRevision < 0 || input.PassID == "" || input.BaseSHA == "" || input.HeadSHA == "" {
		return validationError("expected_revision, pass_id, base_sha, and head_sha are required")
	}
	if len(input.Concepts) < 1 || len(input.Concepts) > 32 || len(input.Sources) > maxHandoffSources || len(input.Evidence) > maxHandoffItems || len(input.Scenarios) > 32 || len(input.Coverage) > 4096 {
		return validationError("handoff item count is outside allowed bounds")
	}
	if err := validateStrings(input); err != nil {
		return err
	}
	sources, err := uniqueHandoffIDs("source", sourceInputIDs(input.Sources))
	if err != nil {
		return err
	}
	concepts, err := uniqueHandoffIDs("concept", conceptIDs(input.Concepts))
	if err != nil {
		return err
	}
	evidence, err := uniqueHandoffIDs("evidence", evidenceIDs(input.Evidence))
	if err != nil {
		return err
	}
	_, err = uniqueHandoffIDs("scenario", scenarioIDs(input.Scenarios))
	if err != nil {
		return err
	}
	for _, source := range input.Sources {
		if source.Side != "base" && source.Side != "head" {
			return validationError("source %q side must be base or head", source.ID)
		}
		if err := gitpkg.ValidateRepositoryPath(source.Path); err != nil || source.LineStart < 1 || source.LineEnd < source.LineStart {
			return validationError("source %q has an invalid path or line range", source.ID)
		}
	}
	if err := requireHandoffText("overview", input.Overview.Before, input.Overview.After, input.Overview.Why,
		input.Overview.NonGoals, input.Overview.Decision, input.Overview.Alternative, input.Overview.Risk,
		input.Overview.VerificationGaps, input.Overview.ReviewQuestion, input.Overview.Rollback); err != nil {
		return err
	}
	allowedRoutes := map[string]bool{"owner": true, "reviewer": true, "maintainer": true, "colleague": true}
	for _, concept := range input.Concepts {
		if err := requireHandoffText("concept "+concept.ID, concept.Title, concept.Claim, concept.Invariant, concept.FailureCase, concept.ChangeGuide); err != nil {
			return err
		}
		if len(concept.Flow) == 0 || len(concept.Flow) > 32 {
			return validationError("concept %q requires 1-32 flow steps", concept.ID)
		}
		if err := requireHandoffText("concept flow", concept.Flow...); err != nil {
			return err
		}
		if err := validateRefs("concept "+concept.ID+" source", concept.SourceIDs, sources); err != nil {
			return err
		}
		if err := validateRefs("concept "+concept.ID+" evidence", concept.EvidenceIDs, evidence); err != nil {
			return err
		}
		for _, route := range concept.Routes {
			if !allowedRoutes[route] {
				return validationError("concept %q has invalid route %q", concept.ID, route)
			}
		}
	}
	for _, item := range input.Evidence {
		if err := requireHandoffText("check "+item.ID, item.Description, item.Command, item.Reporter, item.Limitations); err != nil {
			return err
		}
		if item.Outcome != "passed" && item.Outcome != "failed" && item.Outcome != "skipped" {
			return validationError("check %q outcome must be passed, failed, or skipped", item.ID)
		}
		if !handoffSHA.MatchString(item.HeadSHA) {
			return validationError("check %q requires the full tested head_sha", item.ID)
		}
		if _, err := time.Parse(time.RFC3339, item.RecordedAt); err != nil {
			return validationError("check %q recorded_at must be RFC3339", item.ID)
		}
		if err := validateRefs("evidence "+item.ID+" source", item.SourceIDs, sources); err != nil {
			return err
		}
	}
	for _, scenario := range input.Scenarios {
		if err := requireHandoffText("scenario "+scenario.ID, scenario.Prompt, scenario.Explanation); err != nil {
			return err
		}
		if len(scenario.SourceIDs) == 0 {
			return validationError("scenario %q requires source references", scenario.ID)
		}
		if !concepts[scenario.ConceptID] {
			return validationError("scenario %q references unknown concept %q", scenario.ID, scenario.ConceptID)
		}
		if err := validateRefs("scenario "+scenario.ID+" source", scenario.SourceIDs, sources); err != nil {
			return err
		}
	}
	return nil
}

func validateStrings(input *HandoffInput) error {
	var visit func(reflect.Value) error
	visit = func(value reflect.Value) error {
		if value.Kind() == reflect.Pointer {
			value = value.Elem()
		}
		switch value.Kind() {
		case reflect.String:
			if !utf8.ValidString(value.String()) || len(value.String()) > maxHandoffString || strings.ContainsRune(value.String(), 0) {
				return validationError("handoff strings must be valid, NUL-free UTF-8 and at most %d bytes", maxHandoffString)
			}
		case reflect.Struct:
			for i := 0; i < value.NumField(); i++ {
				if err := visit(value.Field(i)); err != nil {
					return err
				}
			}
		case reflect.Slice:
			for i := 0; i < value.Len(); i++ {
				if err := visit(value.Index(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(reflect.ValueOf(input))
}

func normalizeCoverage(input *HandoffInput, changed []string) error {
	changedSet := stringSet(changed)
	seen := map[string]bool{}
	allowed := map[string]bool{"behavior": true, "tests": true, "mechanical": true, "uncategorized": true}
	for _, coverage := range input.Coverage {
		if !changedSet[coverage.Path] {
			return validationError("coverage path %q is not in the review diff", coverage.Path)
		}
		if !allowed[coverage.Category] {
			return validationError("coverage path %q has invalid category %q", coverage.Path, coverage.Category)
		}
		if seen[coverage.Path] {
			return validationError("coverage path %q is duplicated", coverage.Path)
		}
		seen[coverage.Path] = true
	}
	for _, name := range changed {
		if !seen[name] {
			input.Coverage = append(input.Coverage, HandoffCoverage{Path: name, Category: "uncategorized"})
		}
	}
	return nil
}

func validateChapterReferences(concepts []HandoffConcept, valid map[string]bool) error {
	for _, concept := range concepts {
		if err := validateRefs("concept "+concept.ID+" chapter", concept.ChapterIDs, valid); err != nil {
			return err
		}
	}
	return nil
}

func validateHandoffNote(note HandoffNote, scenarios []HandoffScenario) error {
	raw, err := json.Marshal(note)
	if err != nil || len(raw) > MaxHandoffBytes {
		return validationError("human note exceeds allowed size")
	}
	allowed := map[string]bool{"": true, "ready": true, "need_help": true, "deferred": true}
	if note.Revision < 0 || !allowed[note.Readiness] || len(note.Body) > maxHandoffString || !utf8.ValidString(note.Body) {
		return validationError("invalid handoff note revision, readiness, or body")
	}
	valid := stringSet(scenarioIDs(scenarios))
	for id, answer := range note.Answers {
		if !valid[id] || len(answer) > maxHandoffString || !utf8.ValidString(answer) {
			return validationError("invalid answer for scenario %q", id)
		}
	}
	seen := map[string]bool{}
	for _, id := range note.Skipped {
		if !valid[id] || seen[id] || hasHandoffAnswer(note.Answers, id) {
			return validationError("invalid or overlapping skipped scenario %q", id)
		}
		seen[id] = true
	}
	return nil
}

func selectHandoffConcepts(all []HandoffConcept, ids []string) ([]HandoffConcept, error) {
	if len(ids) == 0 {
		return all, nil
	}
	wanted := stringSet(ids)
	selected := make([]HandoffConcept, 0, len(ids))
	for _, concept := range all {
		if wanted[concept.ID] {
			selected = append(selected, concept)
			delete(wanted, concept.ID)
		}
	}
	if len(wanted) > 0 {
		for id := range wanted {
			return nil, validationError("unknown concept %q", id)
		}
	}
	return selected, nil
}

var handoffID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
var handoffSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func requireHandoffText(label string, values ...string) error {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return validationError("%s fields must describe the behavior or explicitly state the unknown", label)
		}
	}
	return nil
}
func hasHandoffAnswer(answers map[string]string, id string) bool { _, ok := answers[id]; return ok }

func validationError(format string, args ...any) error {
	return &HandoffValidationError{Message: fmt.Sprintf(format, args...)}
}
func uniqueHandoffIDs(kind string, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if !handoffID.MatchString(id) || out[id] {
			return nil, validationError("%s IDs must be unique and use 1-64 letters, digits, dots, underscores, or hyphens", kind)
		}
		out[id] = true
	}
	return out, nil
}
func validateRefs(label string, refs []string, valid map[string]bool) error {
	seen := map[string]bool{}
	for _, id := range refs {
		if !valid[id] || seen[id] {
			return validationError("%s reference %q is unknown or duplicated", label, id)
		}
		seen[id] = true
	}
	return nil
}
func sourceInputIDs(items []HandoffSourceInput) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].ID
	}
	return out
}
func conceptIDs(items []HandoffConcept) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].ID
	}
	return out
}
func evidenceIDs(items []HandoffEvidence) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].ID
	}
	return out
}
func scenarioIDs(items []HandoffScenario) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].ID
	}
	return out
}
func stringSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}
func changedConceptIDs(previous, current []HandoffConcept) []string {
	old := map[string]HandoffConcept{}
	for _, item := range previous {
		old[item.ID] = item
	}
	changed := []string{}
	currentIDs := map[string]bool{}
	for _, item := range current {
		currentIDs[item.ID] = true
		if prior, ok := old[item.ID]; !ok || !reflect.DeepEqual(prior, item) {
			changed = append(changed, item.ID)
		}
	}
	for _, item := range previous {
		if !currentIDs[item.ID] {
			changed = append(changed, item.ID)
		}
	}
	return changed
}
func evidenceTouchesChangedConcept(id string, concepts []HandoffConcept, changed map[string]bool) bool {
	for _, concept := range concepts {
		if changed[concept.ID] {
			for _, evidenceID := range concept.EvidenceIDs {
				if evidenceID == id {
					return true
				}
			}
		}
	}
	return false
}
func emptyHandoffNote() HandoffNote {
	return HandoffNote{Answers: map[string]string{}, Skipped: []string{}}
}
func normalizeHandoffNote(note *HandoffNote) {
	if note.Answers == nil {
		note.Answers = map[string]string{}
	}
	if note.Skipped == nil {
		note.Skipped = []string{}
	}
}
func normalizePacket(packet *HandoffPacket) {
	normalizeHandoffNote(&packet.Note)
	if packet.Sources == nil {
		packet.Sources = []HandoffSource{}
	}
	if packet.Chapters == nil {
		packet.Chapters = []HandoffChapter{}
	}
	if packet.Input.Concepts == nil {
		packet.Input.Concepts = []HandoffConcept{}
	}
	if packet.Input.Evidence == nil {
		packet.Input.Evidence = []HandoffEvidence{}
	}
	if packet.Input.Scenarios == nil {
		packet.Input.Scenarios = []HandoffScenario{}
	}
	if packet.Input.Coverage == nil {
		packet.Input.Coverage = []HandoffCoverage{}
	}
	if packet.Comparison.ChangedConcepts == nil {
		packet.Comparison.ChangedConcepts = []string{}
	}
	if packet.Comparison.ChangedFiles == nil {
		packet.Comparison.ChangedFiles = []string{}
	}
	if packet.Comparison.EvidenceToRecheck == nil {
		packet.Comparison.EvidenceToRecheck = []string{}
	}
}
