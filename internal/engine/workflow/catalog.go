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
	// Essential fields are the ones the inspector shows first; the rest go
	// under More options.
	Essential bool
	// Label is the field's name in plain words, such as "What it should do".
	Label string
	// Order is where the inspector shows the field, from 1.
	Order int
}

// at is the field placed in a step type's form: its order, whether it is
// essential, and its label there when not its own.
func (f Field) at(order int, essential bool, label ...string) Field {
	f.Order, f.Essential = order, essential
	if len(label) > 0 {
		f.Label = label[0]
	}
	return f
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
	// Summary names the fields a step row is summed up by: the first of
	// them with a value, such as the macro or else the prompt.
	Summary []string
	// Group is where the step picker lists the type: one of StepGroups.
	Group string
}

// StepGroups are the step picker's groups, in its order.
var StepGroups = []string{"Agents", "Code", "Board", "Cleanup"}

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
	s.Summary = append([]string(nil), s.Summary...)
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
	promptField = Field{Name: "prompt", Kind: KindText, Label: "What it should do", Description: "What the agent is asked to do"}
	macroField  = Field{Name: "macro", Kind: KindMacro, Label: "Macro", Description: "A macro to send in place of a prompt, by name; resolved when the step runs, so an edited macro applies to the next run",
		Checked: true, Invalid: "must be the name of a macro"}
	agentField   = Field{Name: "agent", Kind: KindAgent, Label: "Agent tool", Description: "Agent tool, such as claude-code, codex or chimera; the macro's, else the default, when empty"}
	tierField    = Field{Name: "tier", Kind: KindTier, Label: "Tier", Description: "The agent tool's tier; the macro's when empty"}
	prRepoField  = Field{Name: "repo", Kind: KindRepository, Label: "Pull request's repository", Description: "With macro: the GitHub repository (owner/name) of the pull request the macro is started on"}
	prNumberFld  = Field{Name: "pr_number", Kind: KindInteger, Label: "Pull request number", Description: "With macro and repo: the number of the pull request the macro is started on"}
	promptMacro  = Constraint{Kind: ExactlyOne, Fields: []string{"prompt", "macro"}, Checked: true}
	wholeTask    = Field{Name: "task", Kind: KindTask, Label: "Task", Description: "The task: ${{ workflow.task }} or a task input", Required: true, Checked: true, Invalid: "must be a whole task reference"}
	literalFalse = func(name, label, description string) Field {
		return Field{Name: name, Kind: KindBoolean, Label: label, Description: description, Default: false, Literal: true, Checked: true, Invalid: "must be true or false"}
	}
)

var catalog = []ExecutorSpec{
	{
		Type: "agent.run", Group: "Agents", Title: "Run an agent",
		Description: "Start an agent of the workflow on a prompt or a macro, and wait for it to complete the step with the outputs the step declares.",
		With: []Field{
			promptField.at(1, true), macroField.at(2, true), agentField.at(5, false), tierField.at(6, false),
			{Name: "name", Kind: KindString, Label: "Agent's name", Description: "The agent's name, shown beside it", Order: 7},
			{Name: "working_dir", Kind: KindPath, Label: "Works in", Description: "Directory the agent works in", Essential: true, Order: 3},
			{Name: "repo", Kind: KindRepository, Label: "Or a clone of", Description: "GitHub repository (owner/name) the agent works in a fresh clone of; with macro, also the pull request's repository", Essential: true, Order: 4},
			{Name: "ref", Kind: KindString, Label: "Clone starts from", Description: "With repo: the branch, tag or commit the clone starts from", Order: 8},
			{Name: "branch", Kind: KindString, Label: "Branch to work on", Description: "With repo: the branch the agent works on", Order: 9},
			{Name: "engram", Kind: KindString, Label: "Engram", Description: "An engram for the agent to be; the step fails when engram does not list it", Checked: true, Invalid: "must be the name of an engram", Order: 10},
			literalFalse("keep_agent", "Keep the agent running", "Leave the agent running once its outputs are in, so the operator can go on talking to it").at(11, false),
			prNumberFld.at(12, false),
			literalFalse("scratch", "Work in a scratch folder", "The work needs no folder of its own: the agent works in a new folder Legato removes when the step completes (kept when it fails or keeps its agent); anything to keep goes in the step's outputs").at(13, false),
		},
		Constraints:     []Constraint{promptMacro, {Kind: Excludes, Fields: []string{"scratch", "working_dir"}, Checked: true}, {Kind: Excludes, Fields: []string{"scratch", "repo"}, Checked: true}},
		DeclaredOutputs: true,
		Summary:         []string{"macro", "prompt"},
	},
	{
		Type: "agent.dispatch", Group: "Agents", Title: "Send to the task's agent",
		Description: "Send a prompt or a macro to a task's agent, starting one when the task has none. The step does not wait for the agent.",
		With: []Field{
			wholeTask.at(1, true), promptField.at(2, true), macroField.at(3, true), agentField.at(4, false), tierField.at(5, false),
			{Name: "working_dir", Kind: KindPath, Label: "A new agent starts in", Description: "Where a new agent starts; a running agent is messaged wherever it is", Order: 6},
			prRepoField.at(7, false), prNumberFld.at(8, false),
		},
		Constraints: []Constraint{promptMacro},
		Summary:     []string{"macro", "prompt"},
		Outputs: []OutputSpec{
			{Name: "agent_session_id", Type: "integer", Description: "The agent's session"},
			{Name: "created", Type: "boolean", Description: "Whether the step started the agent"},
			{Name: "working_dir", Type: "string", Description: "The working_dir the step was given"},
		},
	},
	{
		Type: "agent.stop", Group: "Agents", Title: "Stop the task's agent",
		Description: "Stop a task's agent.",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Label: "Task", Description: "The task, or a task ID", Required: true, Essential: true, Order: 1},
		},
		Summary: []string{"task"},
	},
	{
		Type: "repo.ensure", Group: "Code", Title: "Prepare a checkout",
		Description: "Make Legato's copy of a GitHub repository ready for an agent to be cloned from, or pass a directory through when no repository is given.",
		With: []Field{
			{Name: "repo", Kind: KindRepository, Label: "Repository", Description: "GitHub repository (owner/name); empty to use working_dir", Essential: true, Order: 1},
			{Name: "ref", Kind: KindString, Label: "Starts from", Description: "Branch, tag or commit to start from; the default branch when empty", Order: 3},
			{Name: "branch", Kind: KindString, Label: "Branch to work on", Description: "Branch to work on", Order: 4},
			{Name: "working_dir", Kind: KindPath, Label: "Or a directory", Description: "Directory to work in when no repository is given", Essential: true, Order: 2},
		},
		Constraints: []Constraint{{Kind: Excludes, Fields: []string{"repo", "working_dir"}}},
		Summary:     []string{"repo", "working_dir"},
		Outputs: []OutputSpec{
			{Name: "working_dir", Type: "string", Description: "The directory given, when no repository is"},
			{Name: "repo", Type: "string"}, {Name: "ref", Type: "string"}, {Name: "branch", Type: "string"},
		},
	},
	{
		Type: "task.context", Group: "Board", Title: "Read a task's context",
		Description: "Export a task's full context (title, description, notes, pull requests) as text.",
		With:        []Field{wholeTask.at(1, true)},
		Outputs:     []OutputSpec{{Name: "content", Type: "string", Description: "The task's context"}},
		Summary:     []string{"task"},
	},
	{
		Type: "task.working_dir.resolve", Group: "Board", Title: "Find a task's directory",
		Description: "The task's worktree when it has one, else the fallback.",
		With: []Field{
			wholeTask.at(1, true),
			{Name: "fallback", Kind: KindPath, Label: "Otherwise use", Description: "Directory to use when the task has no worktree", Required: true, Essential: true, Order: 2},
		},
		Summary: []string{"task"},
		Outputs: []OutputSpec{
			{Name: "path", Type: "string"},
			{Name: "source", Type: "string", Description: "worktree or repository"},
		},
	},
	{
		Type: "task.note", Group: "Board", Title: "Add a note to a task",
		Description: "Add a durable note to a task.",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Label: "Task", Description: "The task, or a task ID such as a step's linked_task output", RequiredAtRun: true, Essential: true, Order: 1},
			{Name: "body", Kind: KindText, Label: "Note", Description: "The note", Essential: true, Order: 2},
			{Name: "body_path", Kind: KindPath, Label: "Or take it from a file", Description: "A file to take the note from, such as a step's file output; wins over body", Order: 3},
			literalFalse("optional", "Skip when there is no task", "Skip the step, rather than fail, when there is no task").at(4, false),
		},
		Constraints: []Constraint{{Kind: AtLeastOne, Fields: []string{"body", "body_path"}}},
		Summary:     []string{"body", "body_path"},
		Outputs: []OutputSpec{
			{Name: "note_id", Type: "string"}, {Name: "task_id", Type: "string"},
			{Name: "skipped", Type: "boolean", Description: "true when optional and there was no task"},
			{Name: "away_on", Type: "string", Description: "The worker the task is away on, which won't see the note"},
			{Name: "warning", Type: "string"},
		},
	},
	{
		Type: "task.metadata.set", Group: "Board", Title: "Set task metadata",
		Description: "Set values in a task's metadata.",
		With: []Field{
			{Name: "task", Kind: KindTask, Label: "Task", Description: "The task: ${{ workflow.task }} or a task input", RequiredAtRun: true, Essential: true, Order: 1},
			{Name: "values", Kind: KindJSON, Label: "Values", Description: "A non-empty object of metadata paths and their values", RequiredAtRun: true, Essential: true, Order: 2},
		},
		Summary: []string{"values"},
	},
	{
		Type: "task.move", Group: "Board", Title: "Move a task",
		Description: "Move a task to a column of the board, and its ticket with it.",
		With: []Field{
			wholeTask.at(1, true),
			{Name: "column", Kind: KindColumn, Label: "To column", Description: "The column's name", Required: true, Checked: true, Invalid: "must be a non-empty string", Essential: true, Order: 2},
		},
		Summary: []string{"column"},
	},
	{
		Type: "github.pr.prepare", Group: "Code", Title: "Check out a pull request",
		Description: "Fetch a pull request's exact head into a new worktree, and link the workflow task to it. Needs Yggdrasil worktrees.",
		With: []Field{
			{Name: "repo", Kind: KindRepository, Label: "Repository", Description: "GitHub repository (owner/name)", RequiredAtRun: true, Essential: true, Order: 1},
			{Name: "number", Kind: KindInteger, Label: "Pull request number", Description: "The pull request's number", RequiredAtRun: true, Essential: true, Order: 2},
			{Name: "primary_dir", Kind: KindPath, Label: "Make the worktree from", Description: "The repository's checkout to make the worktree from", RequiredAtRun: true, Essential: true, Order: 3},
			{Name: "base_branch", Kind: KindString, Label: "Merges into", Description: "The branch the pull request merges into", Default: "main", Order: 4},
		},
		Summary: []string{"repo"},
		Outputs: []OutputSpec{
			{Name: "worktree_path", Type: "string"}, {Name: "branch", Type: "string"},
			{Name: "base_branch", Type: "string"}, {Name: "head_sha", Type: "string"},
			{Name: "metadata", Type: "json", Description: "number, title, url, head_repo and head_branch"},
		},
	},
	{
		Type: "github.pr.review_passes", Group: "Code", Title: "Review a pull request in passes",
		Description: "Review an open pull request (or its stack) in passes posted as GitHub reviews from Legato's reviewer App, until a clean pass approves it. Needs review passes turned on.",
		With: []Field{
			{Name: "repo", Kind: KindRepository, Label: "Repository", Description: "GitHub repository (owner/name)", Required: true, Essential: true, Order: 1},
			{Name: "pr_number", Kind: KindInteger, Label: "Pull request number", Description: "The pull request's number", Required: true, Essential: true, Order: 2},
			{Name: "agent_author", Kind: KindBoolean, Label: "The author's agent fixes findings", Description: "Have the agent of the board task the pull request belongs to fix and answer the findings", Default: false, Checked: true, Invalid: "must be true or false", Essential: true, Order: 3},
			{Name: "max_passes", Kind: KindInteger, Label: "Most passes", Description: "Most passes before the loop goes stuck; 0 for this Legato's setting", Default: 0, Order: 5},
			{Name: "reviewer", Kind: KindAgent, Label: "Reviewer", Description: "Agent tool that reviews, or tool:tier; this Legato's setting when empty", Order: 6},
			{Name: "scope", Kind: KindString, Label: "Reviews", Description: "stack: every open pull request of its stack; pr: this pull request only", Default: "stack", Enum: []string{"stack", "pr"}, Order: 4},
		},
		Summary: []string{"repo"},
		Outputs: []OutputSpec{
			{Name: "outcome", Type: "string", Description: "approved, stuck, closed or cancelled"},
			{Name: "passes", Type: "integer"}, {Name: "findings", Type: "integer"},
			{Name: "reviews", Type: "json", Description: "The reviews posted, with their links"},
			{Name: "summary_path", Type: "string", Description: "A Markdown summary of the loop"},
			{Name: "linked_task", Type: "string", Description: "The board task the pull request belongs to, or empty"},
		},
	},
	{
		Type: "worktree.remove", Group: "Cleanup", Title: "Remove the worktree",
		Description: "Remove the workflow task's Yggdrasil worktree, when it is at the path expected.",
		With: []Field{
			{Name: "expected_path", Kind: KindPath, Label: "The worktree at", Description: "Where the worktree must be", RequiredAtRun: true, Essential: true, Order: 1},
			{Name: "force", Kind: KindBoolean, Label: "Even with changes in it", Description: "Remove it with changes in it; a manual step triggered with force sets this", Default: false, Order: 2},
		},
		Summary: []string{"expected_path"},
		Outputs: []OutputSpec{
			{Name: "removed", Type: "boolean"}, {Name: "removed_path", Type: "string"}, {Name: "removed_branch", Type: "string"},
		},
	},
	{
		Type: "file.remove", Group: "Cleanup", Title: "Remove a file",
		Description: "Remove one regular file inside a directory; a file that is not there is not an error.",
		With: []Field{
			{Name: "working_dir", Kind: KindPath, Label: "In directory", Description: "The directory", RequiredAtRun: true, Essential: true, Order: 2},
			{Name: "path", Kind: KindPath, Label: "File", Description: "The file, relative to working_dir", RequiredAtRun: true, Essential: true, Order: 1},
		},
		Summary: []string{"path"},
		Outputs: []OutputSpec{{Name: "removed", Type: "boolean"}},
	},
	{
		Type: "chimera.session.remove", Group: "Cleanup", Title: "Remove the Chimera session",
		Description: "Remove the saved Chimera session of a task's agent.",
		With:        []Field{wholeTask.at(1, true)},
		Outputs:     []OutputSpec{{Name: "removed", Type: "boolean"}, {Name: "metadata_cleared", Type: "boolean"}},
		Summary:     []string{"task"},
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
			{Name: "task", Kind: KindJSON, Description: "The task: a task reference, or an object with its id", Required: true},
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
