package service

import "errors"

const MaxHandoffBytes = 256 * 1024

var ErrHandoffConflict = errors.New("handoff or review changed; refresh and try again")

type HandoffValidationError struct{ Message string }

func (e *HandoffValidationError) Error() string { return e.Message }

// HandoffInput is the agent authoring contract. Human notes and captured source
// content are intentionally absent: neither can be supplied by the author.
type HandoffInput struct {
	SchemaVersion    int                  `json:"schema_version"`
	ExpectedRevision int                  `json:"expected_revision"`
	PassID           string               `json:"pass_id"`
	BaseSHA          string               `json:"base_sha"`
	HeadSHA          string               `json:"head_sha"`
	Overview         HandoffOverview      `json:"overview"`
	Concepts         []HandoffConcept     `json:"concepts"`
	Sources          []HandoffSourceInput `json:"sources"`
	Evidence         []HandoffEvidence    `json:"evidence"`
	Scenarios        []HandoffScenario    `json:"scenarios"`
	Coverage         []HandoffCoverage    `json:"coverage"`
	Changes          string               `json:"changes"`
}

type HandoffOverview struct {
	Before           string `json:"before"`
	After            string `json:"after"`
	Why              string `json:"why"`
	NonGoals         string `json:"non_goals"`
	Decision         string `json:"decision"`
	Alternative      string `json:"alternative"`
	Risk             string `json:"risk"`
	VerificationGaps string `json:"verification_gaps"`
	ReviewQuestion   string `json:"review_question"`
	Rollback         string `json:"rollback"`
}

type HandoffConcept struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Claim       string   `json:"claim"`
	Flow        []string `json:"flow"`
	Invariant   string   `json:"invariant"`
	FailureCase string   `json:"failure_case"`
	ChangeGuide string   `json:"change_guide"`
	SourceIDs   []string `json:"source_ids"`
	EvidenceIDs []string `json:"evidence_ids"`
	ChapterIDs  []string `json:"chapter_ids"`
	Routes      []string `json:"routes"`
}

type HandoffSourceInput struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Side      string `json:"side"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
}

type HandoffSource struct {
	HandoffSourceInput
	CommitSHA string `json:"commit_sha"`
	Text      string `json:"text"`
	Digest    string `json:"digest"`
}

type HandoffEvidence struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	SourceIDs   []string `json:"source_ids"`
	Command     string   `json:"command"`
	HeadSHA     string   `json:"head_sha"`
	RecordedAt  string   `json:"recorded_at"`
	Outcome     string   `json:"outcome"`
	Reporter    string   `json:"reporter"`
	Limitations string   `json:"limitations"`
}

type HandoffScenario struct {
	ID          string   `json:"id"`
	ConceptID   string   `json:"concept_id"`
	Prompt      string   `json:"prompt"`
	Explanation string   `json:"explanation"`
	SourceIDs   []string `json:"source_ids"`
}

type HandoffCoverage struct {
	Path     string `json:"path"`
	Category string `json:"category"`
}

type HandoffChapter struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Narration string `json:"narration"`
}

// HandoffNote belongs to one immutable packet, not to a review completion state.
// Revision is an optimistic-write token; a new note starts at revision zero.
type HandoffNote struct {
	Revision  int               `json:"revision"`
	Readiness string            `json:"readiness"`
	Body      string            `json:"body"`
	Answers   map[string]string `json:"answers"`
	Skipped   []string          `json:"skipped"`
}

type HandoffPacket struct {
	ID             string            `json:"id"`
	Revision       int               `json:"revision"`
	CreatedAt      string            `json:"created_at"`
	Repository     string            `json:"repository"`
	Input          HandoffInput      `json:"input"`
	Sources        []HandoffSource   `json:"sources"`
	Chapters       []HandoffChapter  `json:"chapters"`
	PlanID         string            `json:"plan_id"`
	PlanRevision   string            `json:"plan_revision"`
	PlanMarkdown   string            `json:"plan_markdown"`
	Outdated       bool              `json:"outdated"`
	OutdatedReason string            `json:"outdated_reason"`
	Note           HandoffNote       `json:"note"`
	Comparison     HandoffComparison `json:"comparison"`
}

type HandoffComparison struct {
	PreviousRevision  int      `json:"previous_revision"`
	Safe              bool     `json:"safe"`
	Reason            string   `json:"reason"`
	ChangedConcepts   []string `json:"changed_concepts"`
	ChangedFiles      []string `json:"changed_files"`
	EvidenceToRecheck []string `json:"evidence_to_recheck"`
}

type HandoffView struct {
	TourID    string          `json:"tour_id"`
	CheckedAt string          `json:"checked_at"`
	Packets   []HandoffPacket `json:"packets"`
}

// Export requires explicit selection. Personal notes and source excerpts are
// excluded unless the caller opts in. Empty ConceptIDs selects all concepts.
type HandoffExportInput struct {
	PacketID       string   `json:"packet_id"`
	Format         string   `json:"format"`
	ConceptIDs     []string `json:"concept_ids"`
	IncludeSources bool     `json:"include_sources"`
	IncludeNotes   bool     `json:"include_notes"`
}

type HandoffExport struct {
	Markdown string   `json:"markdown"`
	Filename string   `json:"filename"`
	Warnings []string `json:"warnings"`
}
