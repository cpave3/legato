package workflow

import (
	"fmt"
	"strings"
)

// The executor catalog is where each step type declares its contract: the
// with fields it reads, how they combine, and the outputs it produces.
// Validate checks steps against it, and the JSON Schema, the web builder and
// the editor are made from it, so a new executor is added here first.

// FieldKind is what a with field holds, for the schema and the builder.
type FieldKind string

const (
	KindString     FieldKind = "string"
	KindText       FieldKind = "text" // a multi-line string, such as a prompt
	KindInteger    FieldKind = "integer"
	KindBoolean    FieldKind = "boolean"
	KindTask       FieldKind = "task"       // a whole task reference: ${{ workflow.task }} or a task input
	KindTaskOrID   FieldKind = "task_or_id" // a task reference, or a task ID such as a step's output
	KindRepository FieldKind = "repository" // a GitHub repository, owner/name
	KindPath       FieldKind = "path"       // a file or directory on this machine
	KindColumn     FieldKind = "column"     // a board column's name
	KindMacro      FieldKind = "macro"      // a macro's name, resolved when the step runs
	KindAgent      FieldKind = "agent"      // an agent tool, such as claude-code
	KindTier       FieldKind = "tier"       // an agent tool's tier
	KindJSON       FieldKind = "json"       // any structured value
)

// Field is one with field of a step type.
type Field struct {
	Name        string
	Kind        FieldKind
	Description string
	// Required fields must be in with for the file to load.
	Required bool
	// RequiredAtRun fields are needed by the executor: the file loads
	// without one, and the step fails when it runs.
	RequiredAtRun bool
	Default       any
	Enum          []string
	// Literal fields take a value as written: ${{ }} is not allowed.
	Literal bool
	// Checked fields have their kind checked when the file loads (Invalid
	// says what the value must be); the others are checked when the step
	// runs.
	Checked bool
	Invalid string
}

// ConstraintKind says how the fields of a Constraint combine.
type ConstraintKind string

const (
	ExactlyOne ConstraintKind = "exactly_one"
	AtLeastOne ConstraintKind = "at_least_one"
	Excludes   ConstraintKind = "excludes" // not both
)

// Constraint ties several with fields together. Checked constraints are
// checked when the file loads; the others when the step runs, on the
// resolved values.
type Constraint struct {
	Kind    ConstraintKind
	Fields  []string
	Checked bool
}

// OutputSpec is an output an executor produces, which later steps can read
// as steps.<id>.outputs.<name> once the step declares it.
type OutputSpec struct {
	Name        string
	Type        string // string, integer, boolean or json
	Description string
}

// ExecutorSpec is one step type's contract.
type ExecutorSpec struct {
	Type        string
	Title       string
	Description string
	// Internal executors are Legato's own (the learning workflows): files
	// may use them, and the builder does not offer them.
	Internal    bool
	With        []Field
	Constraints []Constraint
	Outputs     []OutputSpec
	// DeclaredOutputs executors produce whatever outputs the step declares:
	// an agent completes the step with them.
	DeclaredOutputs bool
}

// Field is the spec's with field of that name.
func (s ExecutorSpec) Field(name string) (Field, bool) {
	for _, field := range s.With {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

// Output is the spec's output of that name.
func (s ExecutorSpec) Output(name string) (OutputSpec, bool) {
	for _, output := range s.Outputs {
		if output.Name == name {
			return output, true
		}
	}
	return OutputSpec{}, false
}

// Catalog lists every step type schema v1 accepts, in the order the builder
// offers them. The result is a copy.
func Catalog() []ExecutorSpec {
	out := make([]ExecutorSpec, len(catalog))
	for i, spec := range catalog {
		out[i] = spec.clone()
	}
	return out
}

// LookupExecutor is the contract of one step type.
func LookupExecutor(stepType string) (ExecutorSpec, bool) {
	if spec, ok := catalogByType[stepType]; ok {
		return spec.clone(), true
	}
	return ExecutorSpec{}, false
}

func (s ExecutorSpec) clone() ExecutorSpec {
	s.With = append([]Field(nil), s.With...)
	for i := range s.With {
		s.With[i].Enum = append([]string(nil), s.With[i].Enum...)
	}
	s.Constraints = append([]Constraint(nil), s.Constraints...)
	for i := range s.Constraints {
		s.Constraints[i].Fields = append([]string(nil), s.Constraints[i].Fields...)
	}
	s.Outputs = append([]OutputSpec(nil), s.Outputs...)
	return s
}

// validateStepContract checks a step's with against its type's contract:
// the checked constraints, then the checked fields' values, then the
// required fields.
func validateStepContract(step Step, inputs map[string]Input) error {
	spec, ok := catalogByType[step.Type]
	if !ok {
		return fmt.Errorf("unsupported type %q", step.Type)
	}
	for _, constraint := range spec.Constraints {
		if !constraint.Checked {
			continue
		}
		if err := constraint.check(step.With); err != nil {
			return within("with", err)
		}
	}
	for _, field := range spec.With {
		value, ok := step.With[field.Name]
		if !field.Checked || !ok {
			continue
		}
		if !field.accepts(value, inputs) {
			return within("with."+field.Name, fmt.Errorf("with %s %s", field.Name, field.Invalid))
		}
	}
	for _, field := range spec.With {
		if _, ok := step.With[field.Name]; field.Required && !ok {
			return within("with", fmt.Errorf("with requires %s", field.Name))
		}
	}
	return nil
}

func (c Constraint) check(with map[string]any) error {
	present := 0
	for _, name := range c.Fields {
		if _, ok := with[name]; ok {
			present++
		}
	}
	names := strings.Join(c.Fields, " or ")
	switch {
	case c.Kind == ExactlyOne && present > 1, c.Kind == Excludes && present > 1:
		return fmt.Errorf("with takes %s, not both", names)
	case (c.Kind == ExactlyOne || c.Kind == AtLeastOne) && present == 0:
		return fmt.Errorf("with requires %s", names)
	}
	return nil
}

// accepts reports whether a value is of the field's kind, as far as the
// file alone can tell.
func (f Field) accepts(value any, inputs map[string]Input) bool {
	text, isText := value.(string)
	expression := isText && !f.Literal && expressionPattern.MatchString(text)
	switch f.Kind {
	case KindTask:
		return wholeTaskReference(value, inputs)
	case KindBoolean:
		_, boolean := value.(bool)
		return boolean || expression
	case KindInteger:
		switch value.(type) {
		case int, int64, uint64:
			return true
		}
		return expression
	case KindMacro:
		return isText && strings.TrimSpace(text) != ""
	case KindColumn:
		return isText && (expression || strings.TrimSpace(text) != "")
	case KindJSON:
		return true
	default:
		return isText
	}
}

var catalogByType = func() map[string]ExecutorSpec {
	byType := make(map[string]ExecutorSpec, len(catalog))
	for _, spec := range catalog {
		if _, duplicate := byType[spec.Type]; duplicate {
			panic("workflow catalog lists " + spec.Type + " twice")
		}
		byType[spec.Type] = spec
	}
	return byType
}()

// Fields several executors share.
var (
	promptField = Field{Name: "prompt", Kind: KindText, Description: "What the agent is asked to do"}
	macroField  = Field{Name: "macro", Kind: KindMacro, Description: "A macro to send in place of a prompt, by name; resolved when the step runs, so an edited macro applies to the next run",
		Checked: true, Invalid: "must be the name of a macro"}
	agentField   = Field{Name: "agent", Kind: KindAgent, Description: "Agent tool, such as claude-code, codex or chimera; the macro's, else the default, when empty"}
	tierField    = Field{Name: "tier", Kind: KindTier, Description: "The agent tool's tier; the macro's when empty"}
	prRepoField  = Field{Name: "repo", Kind: KindRepository, Description: "With macro: the GitHub repository (owner/name) of the pull request the macro is started on"}
	prNumberFld  = Field{Name: "pr_number", Kind: KindInteger, Description: "With macro and repo: the number of the pull request the macro is started on"}
	promptMacro  = Constraint{Kind: ExactlyOne, Fields: []string{"prompt", "macro"}, Checked: true}
	wholeTask    = Field{Name: "task", Kind: KindTask, Description: "The task: ${{ workflow.task }} or a task input", Required: true, Checked: true, Invalid: "must be a whole task reference"}
	literalFalse = func(name, description string) Field {
		return Field{Name: name, Kind: KindBoolean, Description: description, Default: false, Literal: true, Checked: true, Invalid: "must be true or false"}
	}
)

var catalog = []ExecutorSpec{
	{
		Type: "agent.run", Title: "Run an agent",
		Description: "Start an agent of the workflow on a prompt or a macro, and wait for it to complete the step with the outputs the step declares.",
		With: []Field{
			promptField, macroField, agentField, tierField,
			{Name: "name", Kind: KindString, Description: "The agent's name, shown beside it"},
			{Name: "working_dir", Kind: KindPath, Description: "Directory the agent works in"},
			{Name: "repo", Kind: KindRepository, Description: "GitHub repository (owner/name) the agent works in a fresh clone of; with macro, also the pull request's repository"},
			{Name: "ref", Kind: KindString, Description: "With repo: the branch, tag or commit the clone starts from"},
			{Name: "branch", Kind: KindString, Description: "With repo: the branch the agent works on"},
			{Name: "engram", Kind: KindString, Description: "An engram for the agent to be; the step fails when engram does not list it", Checked: true, Invalid: "must be the name of an engram"},
			literalFalse("keep_agent", "Leave the agent running once its outputs are in, so the operator can go on talking to it"),
			prNumberFld,
		},
		Constraints:     []Constraint{promptMacro},
		DeclaredOutputs: true,
	},
	{
		Type: "agent.dispatch", Title: "Send to the task's agent",
		Description: "Send a prompt or a macro to a task's agent, starting one when the task has none. The step does not wait for the agent.",
		With: []Field{
			wholeTask, promptField, macroField, agentField, tierField,
			{Name: "working_dir", Kind: KindPath, Description: "Where a new agent starts; a running agent is messaged wherever it is"},
			prRepoField, prNumberFld,
		},
		Constraints: []Constraint{promptMacro},
		Outputs: []OutputSpec{
			{Name: "agent_session_id", Type: "integer", Description: "The agent's session"},
			{Name: "created", Type: "boolean", Description: "Whether the step started the agent"},
			{Name: "working_dir", Type: "string", Description: "The working_dir the step was given"},
		},
	},
	{
		Type: "agent.stop", Title: "Stop the task's agent",
		Description: "Stop a task's agent.",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Description: "The task, or a task ID", Required: true},
		},
	},
	{
		Type: "repo.ensure", Title: "Prepare a checkout",
		Description: "Make Legato's copy of a GitHub repository ready for an agent to be cloned from, or pass a directory through when no repository is given.",
		With: []Field{
			{Name: "repo", Kind: KindRepository, Description: "GitHub repository (owner/name); empty to use working_dir"},
			{Name: "ref", Kind: KindString, Description: "Branch, tag or commit to start from; the default branch when empty"},
			{Name: "branch", Kind: KindString, Description: "Branch to work on"},
			{Name: "working_dir", Kind: KindPath, Description: "Directory to work in when no repository is given"},
		},
		Constraints: []Constraint{{Kind: Excludes, Fields: []string{"repo", "working_dir"}}},
		Outputs: []OutputSpec{
			{Name: "working_dir", Type: "string", Description: "The directory given, when no repository is"},
			{Name: "repo", Type: "string"}, {Name: "ref", Type: "string"}, {Name: "branch", Type: "string"},
		},
	},
	{
		Type: "task.context", Title: "Read a task's context",
		Description: "Export a task's full context (title, description, notes, pull requests) as text.",
		With:        []Field{wholeTask},
		Outputs:     []OutputSpec{{Name: "content", Type: "string", Description: "The task's context"}},
	},
	{
		Type: "task.working_dir.resolve", Title: "Find a task's directory",
		Description: "The task's worktree when it has one, else the fallback.",
		With: []Field{
			wholeTask,
			{Name: "fallback", Kind: KindPath, Description: "Directory to use when the task has no worktree", Required: true},
		},
		Outputs: []OutputSpec{
			{Name: "path", Type: "string"},
			{Name: "source", Type: "string", Description: "worktree or repository"},
		},
	},
	{
		Type: "task.note", Title: "Add a note to a task",
		Description: "Add a durable note to a task.",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Description: "The task, or a task ID such as a step's linked_task output", RequiredAtRun: true},
			{Name: "body", Kind: KindText, Description: "The note"},
			{Name: "body_path", Kind: KindPath, Description: "A file to take the note from, such as a step's file output; wins over body"},
			literalFalse("optional", "Skip the step, rather than fail, when there is no task"),
		},
		Constraints: []Constraint{{Kind: AtLeastOne, Fields: []string{"body", "body_path"}}},
		Outputs: []OutputSpec{
			{Name: "note_id", Type: "string"}, {Name: "task_id", Type: "string"},
			{Name: "skipped", Type: "boolean", Description: "true when optional and there was no task"},
			{Name: "away_on", Type: "string", Description: "The worker the task is away on, which won't see the note"},
			{Name: "warning", Type: "string"},
		},
	},
	{
		Type: "task.metadata.set", Title: "Set task metadata",
		Description: "Set values in a task's metadata.",
		With: []Field{
			{Name: "task", Kind: KindTask, Description: "The task: ${{ workflow.task }} or a task input", RequiredAtRun: true},
			{Name: "values", Kind: KindJSON, Description: "A non-empty object of metadata paths and their values", RequiredAtRun: true},
		},
	},
	{
		Type: "task.move", Title: "Move a task",
		Description: "Move a task to a column of the board, and its ticket with it.",
		With: []Field{
			wholeTask,
			{Name: "column", Kind: KindColumn, Description: "The column's name", Required: true, Checked: true, Invalid: "must be a non-empty string"},
		},
	},
	{
		Type: "github.pr.prepare", Title: "Check out a pull request",
		Description: "Fetch a pull request's exact head into a new worktree, and link the workflow task to it. Needs Yggdrasil worktrees.",
		With: []Field{
			{Name: "repo", Kind: KindRepository, Description: "GitHub repository (owner/name)", RequiredAtRun: true},
			{Name: "number", Kind: KindInteger, Description: "The pull request's number", RequiredAtRun: true},
			{Name: "primary_dir", Kind: KindPath, Description: "The repository's checkout to make the worktree from", RequiredAtRun: true},
			{Name: "base_branch", Kind: KindString, Description: "The branch the pull request merges into", Default: "main"},
		},
		Outputs: []OutputSpec{
			{Name: "worktree_path", Type: "string"}, {Name: "branch", Type: "string"},
			{Name: "base_branch", Type: "string"}, {Name: "head_sha", Type: "string"},
			{Name: "metadata", Type: "json", Description: "number, title, url, head_repo and head_branch"},
		},
	},
	{
		Type: "github.pr.review_passes", Title: "Review a pull request in passes",
		Description: "Review an open pull request (or its stack) in passes posted as GitHub reviews from Legato's reviewer App, until a clean pass approves it. Needs review passes turned on.",
		With: []Field{
			{Name: "repo", Kind: KindRepository, Description: "GitHub repository (owner/name)", Required: true},
			{Name: "pr_number", Kind: KindInteger, Description: "The pull request's number", Required: true},
			{Name: "agent_author", Kind: KindBoolean, Description: "Have the agent of the board task the pull request belongs to fix and answer the findings", Default: false, Checked: true, Invalid: "must be true or false"},
			{Name: "max_passes", Kind: KindInteger, Description: "Most passes before the loop goes stuck; 0 for this Legato's setting", Default: 0},
			{Name: "reviewer", Kind: KindAgent, Description: "Agent tool that reviews, or tool:tier; this Legato's setting when empty"},
			{Name: "scope", Kind: KindString, Description: "stack: every open pull request of its stack; pr: this pull request only", Default: "stack", Enum: []string{"stack", "pr"}},
		},
		Outputs: []OutputSpec{
			{Name: "outcome", Type: "string", Description: "approved, stuck, closed or cancelled"},
			{Name: "passes", Type: "integer"}, {Name: "findings", Type: "integer"},
			{Name: "reviews", Type: "json", Description: "The reviews posted, with their links"},
			{Name: "summary_path", Type: "string", Description: "A Markdown summary of the loop"},
			{Name: "linked_task", Type: "string", Description: "The board task the pull request belongs to, or empty"},
		},
	},
	{
		Type: "worktree.remove", Title: "Remove the worktree",
		Description: "Remove the workflow task's Yggdrasil worktree, when it is at the path expected.",
		With: []Field{
			{Name: "expected_path", Kind: KindPath, Description: "Where the worktree must be", RequiredAtRun: true},
			{Name: "force", Kind: KindBoolean, Description: "Remove it with changes in it; a manual step triggered with force sets this", Default: false},
		},
		Outputs: []OutputSpec{
			{Name: "removed", Type: "boolean"}, {Name: "removed_path", Type: "string"}, {Name: "removed_branch", Type: "string"},
		},
	},
	{
		Type: "file.remove", Title: "Remove a file",
		Description: "Remove one regular file inside a directory; a file that is not there is not an error.",
		With: []Field{
			{Name: "working_dir", Kind: KindPath, Description: "The directory", RequiredAtRun: true},
			{Name: "path", Kind: KindPath, Description: "The file, relative to working_dir", RequiredAtRun: true},
		},
		Outputs: []OutputSpec{{Name: "removed", Type: "boolean"}},
	},
	{
		Type: "chimera.session.remove", Title: "Remove the Chimera session",
		Description: "Remove the saved Chimera session of a task's agent.",
		With:        []Field{wholeTask},
		Outputs:     []OutputSpec{{Name: "removed", Type: "boolean"}, {Name: "metadata_cleared", Type: "boolean"}},
	},
	{
		Type: "agent.interview", Title: "Ask the agent to reflect", Internal: true,
		Description: "Ask a task's running agent for a reflection, for Legato Learn.",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Description: "The task, or a task ID", RequiredAtRun: true},
			{Name: "prompt", Kind: KindText, Description: "The question; Legato's reflection prompt when empty"},
		},
		Outputs: []OutputSpec{
			{Name: "available", Type: "boolean"}, {Name: "request_id", Type: "string"},
			{Name: "reason", Type: "string", Description: "Why the agent could not be asked"},
		},
	},
	{
		Type: "learning.packet.prepare", Title: "Prepare a learning packet", Internal: true,
		Description: "Gather the evidence for a curation, for Legato Learn.",
		With: []Field{
			{Name: "task", Kind: KindTask, Description: "The task", Required: true},
			{Name: "project", Kind: KindJSON, Description: "key and label of the project"},
			{Name: "limits", Kind: KindJSON, Description: "max_items and max_bytes", Required: true},
		},
		Outputs: []OutputSpec{
			{Name: "generation_id", Type: "string"}, {Name: "packet_digest", Type: "string"}, {Name: "packet", Type: "json"},
		},
	},
	{
		Type: "learning.proposals.submit", Title: "Submit learning proposals", Internal: true,
		Description: "Submit a curator's operations, for Legato Learn.",
		With: []Field{
			{Name: "attempt_id", Kind: KindString, Description: "The curator's attempt"},
			{Name: "generation", Kind: KindJSON, Description: "An attempt reference, in place of attempt_id"},
			{Name: "operations", Kind: KindJSON, Description: "The curator's operations document", Required: true},
		},
		Constraints: []Constraint{{Kind: AtLeastOne, Fields: []string{"attempt_id", "generation"}, Checked: true}},
		Outputs: []OutputSpec{
			{Name: "generation_id", Type: "string"}, {Name: "packet_digest", Type: "string"}, {Name: "status", Type: "string"},
			{Name: "replayed", Type: "boolean"}, {Name: "summary", Type: "json"},
		},
	},
}
