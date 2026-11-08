package service

import (
	"fmt"
	"strings"
)

// A handoff's purpose and where its work starts. See GLOSSARY.md § Instances
// and handoff and docs/ai/integrations.md § Handing off a task.
const (
	// HandoffMakeChange: the agent changes the code and ends in draft PRs.
	HandoffMakeChange = "make-change"
	// HandoffFindOut: the agent answers in notes and pushes nothing.
	HandoffFindOut = "find-out"

	// HandoffNewBranch: the work starts on a new branch.
	HandoffNewBranch = "new-branch"
	// HandoffPushedBranch: the work starts on a branch already on GitHub.
	HandoffPushedBranch = "pushed-branch"
)

// HandoffCallPath is the worker's route for the Handoff call.
const HandoffCallPath = "/api/integrations/handoff/tasks"

// HandoffCall is the Handoff call a main sends a worker: one task, with what
// travels with it. HandoffID, made by the main and fresh per handoff, is the
// idempotency key: the same ID with the same body is answered with the card
// already made.
type HandoffCall struct {
	HandoffID  string `json:"handoff_id"`
	MainTaskID string `json:"main_task_id"`
	Title      string `json:"title"`
	// Details are the task's Legato details: a local task's description,
	// or what a provider-backed task has besides its ticket's text. They are
	// the one part the worker can edit, and the part a return brings back.
	Details string `json:"details"`
	// Ticket is set for a task from a ticket provider (Jira, Burnlite, ...),
	// and only for one: the ticket's text, read only on the worker, as is
	// the title.
	Ticket *HandoffTicket    `json:"ticket,omitempty"`
	Notes  []HandoffCallNote `json:"notes"`
	// Repo is the GitHub repository (owner/name) the work is in.
	Repo    string `json:"repo"`
	Purpose string `json:"purpose"`
	Kind    string `json:"kind"`
	// Branch is the branch the work starts on: for a new branch, the one
	// the worker's clone makes (legato/<main task id>).
	Branch string `json:"branch"`
	// AgentKind is the agent tool; empty for the worker's default.
	AgentKind string `json:"agent_kind,omitempty"`
	// Review overrides the worker's review passes for this handoff; nil
	// keeps its defaults.
	Review *ReviewPassOverride `json:"review,omitempty"`
}

// HandoffTicket is a provider-backed task's ticket: the main's provider
// ("jira", "burnlite"), the ticket's key, its text as the main last synced
// it, and where the person sees it (empty when the main has no address).
// The worker keeps it as a copy taken at handoff and never talks to the
// provider.
type HandoffTicket struct {
	Provider string `json:"provider"`
	Key      string `json:"key"`
	Text     string `json:"text"`
	URL      string `json:"url,omitempty"`
}

// handoffCopySuffix ends the provider of a worker's copy of a ticket.
const handoffCopySuffix = "-copy"

// HandoffCopyProvider is the provider of a worker's card for a task from
// provider ("jira-copy" for "jira"): the card keeps the ticket's text as a
// provider-backed task does (remote_description_md, read only, with its
// title), but no ticket source has this name, so nothing on the worker
// syncs it or moves a ticket.
func HandoffCopyProvider(provider string) string { return provider + handoffCopySuffix }

// HandoffCopyOf is the provider a worker's copy of a ticket came from
// ("burnlite" for "burnlite-copy"), or "" for a provider that is not a copy.
func HandoffCopyOf(provider string) string {
	if from, ok := strings.CutSuffix(provider, handoffCopySuffix); ok {
		return from
	}
	return ""
}

// IsHandoffCopy says whether provider is a worker's copy of a ticket.
func IsHandoffCopy(provider string) bool { return HandoffCopyOf(provider) != "" }

// ticketProviderLabels name the ticket providers Legato knows for people.
var ticketProviderLabels = map[string]string{"jira": "Jira", BurnliteIntegration: "Burnlite", "github": "GitHub"}

// TicketProviderLabel is a ticket provider's name for people ("Jira" for
// "jira"); a worker's copy is named by the provider it came from, and a
// provider Legato does not know by its own name.
func TicketProviderLabel(provider string) string {
	if from := HandoffCopyOf(provider); from != "" {
		provider = from
	}
	if label, ok := ticketProviderLabels[provider]; ok {
		return label
	}
	return provider
}

// HandoffCallNote is one of the main's notes on a handed-off task.
type HandoffCallNote struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at,omitempty"`
}

// HandoffAccepted is the worker's answer to a Handoff call.
type HandoffAccepted struct {
	HandoffID        string `json:"handoff_id"`
	WorkerTaskID     string `json:"worker_task_id"`
	WorkerInstanceID string `json:"worker_instance_id"`
	State            string `json:"state"`
	Failure          string `json:"failure,omitempty"`
}

// HandoffError is a handoff a worker refused, with the code its route
// answers with.
type HandoffError struct {
	Code    string
	Message string
}

func (e *HandoffError) Error() string { return e.Message }

func handoffErr(code, format string, args ...any) *HandoffError {
	return &HandoffError{Code: code, Message: fmt.Sprintf(format, args...)}
}
