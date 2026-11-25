package workflow

import (
	"fmt"
	"regexp"
	"slices"
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
	KindChatBot    FieldKind = "chat_bot"   // a chat bot set up in Settings › Integrations, such as slack
	// KindChatButtons is a list of buttons, each {label, value, style}.
	KindChatButtons FieldKind = "chat_buttons"
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
	// Pattern, when set, is what a text value must match (a regular
	// expression), and Reserved the values it may not take.
	Pattern  string
	Reserved []string
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
	Requires   ConstraintKind = "requires" // the first field needs the rest
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
	With        []Field
	Constraints []Constraint
	Outputs     []OutputSpec
	// DeclaredOutputs executors produce whatever outputs the step declares:
	// an agent completes the step with them. Their Outputs are those Legato
	// fills beside them (an agent step's, LEG-485).
	DeclaredOutputs bool
	// Summary names the fields a step row is summed up by: the first of
	// them with a value, such as the macro or else the prompt.
	Summary []string
	// Group is where the step picker lists the type: one of StepGroups.
	Group string
}

// StepGroups are the step picker's groups, in its order.
var StepGroups = []string{"Agents", "Code", "Board", "Chat", "Learn", "Cleanup", "Control"}

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
		s.With[i].Reserved = append([]string(nil), s.With[i].Reserved...)
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
		if field.Kind == KindChatButtons {
			if err := checkChatButtons(value); err != nil {
				return within("with."+field.Name, err)
			}
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
	if err := checkWorkflowRunStep(step, inputs); err != nil {
		return err
	}
	return checkAgentStep(step)
}

func (c Constraint) check(with map[string]any) error {
	present := 0
	for _, name := range c.Fields {
		if _, ok := with[name]; ok {
			present++
		}
	}
	if c.Kind == Requires {
		if _, ok := with[c.Fields[0]]; !ok {
			return nil
		}
		for _, name := range c.Fields[1:] {
			if _, ok := with[name]; !ok {
				return fmt.Errorf("with %s requires %s", c.Fields[0], name)
			}
		}
		return nil
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
	expression := isText && !f.Literal && HasExpression(text)
	if len(f.Enum) > 0 && !expression {
		return isText && slices.Contains(f.Enum, text)
	}
	if f.Pattern != "" && !expression && (!isText || !regexp.MustCompile(f.Pattern).MatchString(text)) {
		return false
	}
	if isText && slices.Contains(f.Reserved, text) {
		return false
	}
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
		if f.Name == "options" && f.Checked {
			return gateOptionsValid(value)
		}
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
	ticketProviderField = Field{Name: "provider", Kind: KindString, Label: "Provider", Description: "jira or burnlite; the only one set up when empty",
		Enum: []string{"jira", "burnlite"}, Checked: true, Invalid: "must be jira or burnlite", Essential: true, Order: 1}
	promptField = Field{Name: "prompt", Kind: KindText, Label: "What it should do", Description: "What the agent is asked to do"}
	macroField  = Field{Name: "macro", Kind: KindMacro, Label: "Macro", Description: "A macro to send in place of a prompt, by name; resolved when the step runs, so an edited macro applies to the next run",
		Checked: true, Invalid: "must be the name of a macro"}
	agentField  = Field{Name: "agent", Kind: KindAgent, Label: "Agent tool", Description: "Agent tool, such as claude-code, codex or chimera; the macro's, else the default, when empty"}
	tierField   = Field{Name: "tier", Kind: KindTier, Label: "Tier", Description: "The agent tool's tier; the macro's when empty"}
	prRepoField = Field{Name: "repo", Kind: KindRepository, Label: "Pull request's repository", Description: "With macro: the GitHub repository (owner/name) of the pull request the macro is started on"}
	prNumberFld = Field{Name: "pr_number", Kind: KindInteger, Label: "Pull request number", Description: "With macro and repo: the number of the pull request the macro is started on"}
	promptMacro = Constraint{Kind: ExactlyOne, Fields: []string{"prompt", "macro"}, Checked: true}
	// The fields every chat step shares (LEG-633).
	chatBot            = Field{Name: "bot", Kind: KindChatBot, Label: "Bot", Description: "The bot, by name: slack or discord", Required: true, Checked: true, Pattern: ChatBotNamePattern, Invalid: "must be a bot's name, such as slack or discord", Essential: true, Order: 1}
	chatChannel        = Field{Name: "channel", Kind: KindString, Label: "Channel", Description: "The channel's ID; the bot's default channel when empty", RequiredAtRun: true, Essential: true, Order: 2}
	chatReactionFields = []Field{
		chatBot,
		chatChannel,
		{Name: "message_id", Kind: KindString, Label: "Message", Description: "The message's ID: a chat trigger's ${{ event.message_id }} (with its ${{ event.channel }}), an earlier chat.message.send's message_id output, or any message's ID in the channel", Required: true, Essential: true, Order: 3},
		{Name: "emoji", Kind: KindString, Label: "Emoji", Description: "An emoji's name without colons, such as eyes or white_check_mark, or the emoji itself; on Discord a custom emoji as name:id", Required: true, Essential: true, Order: 4},
	}
	// agentFromField names the agent an earlier step used: {from: <step>}.
	agentFromField = Field{Name: "agent", Kind: KindJSON, Label: "Or the agent of a step", Description: "{from: <step>}: the agent an earlier agent.run, agent.message or agent.dispatch step used; checked when the file loads"}
	agentOrTask    = Constraint{Kind: ExactlyOne, Fields: []string{"task", "agent"}, Checked: true}
	wholeTask      = Field{Name: "task", Kind: KindTask, Label: "Task", Description: "The task: ${{ workflow.task }} or a task input", Required: true, Checked: true, Invalid: "must be a whole task reference"}
	// agentFilledOutputs are what Legato fills on an agent step when its
	// wait ends (spec LEG-482 §5.6, LEG-485), beside the outputs the step
	// declares, which the agent fills and which win on a clash.
	agentFilledOutputs = []OutputSpec{
		{Name: "signal", Type: "string", Description: "What ended the wait: complete, finished, ended or none"},
		{Name: "stage", Type: "string", Description: "The agent's stage then"},
		{Name: "stage_reason", Type: "string", Description: "Why it is at that stage: the agent's note, or what moved it"},
		{Name: "last_message", Type: "string", Description: "The agent's last message, at most 4 KiB; empty when Legato cannot read its conversation"},
		{Name: "session_id", Type: "integer", Description: "The agent's session"},
		{Name: "task", Type: "string", Description: "The task whose agent it is"},
		{Name: "working_dir", Type: "string", Description: "The folder the agent works in"},
		{Name: "clone_id", Type: "integer", Description: "The job clone it works in, for a later step's clone; 0 for none"},
		{Name: "branch", Type: "string", Description: "The branch its folder has checked out"},
		{Name: "top_branch", Type: "string", Description: "The top of the clone's stack, or branch"},
		{Name: "stack", Type: "json", Description: "The clone's stack, bottom first: [{name, parent, state, head_sha}]"},
		{Name: "prs", Type: "json", Description: "The pull requests of the clone's branches: [{repo, number, url, state, draft, branch}]"},
	}
	literalFalse = func(name, label, description string) Field {
		return Field{Name: name, Kind: KindBoolean, Label: label, Description: description, Default: false, Literal: true, Checked: true, Invalid: "must be true or false"}
	}
)

// GateStepType is the step that asks the operator to choose (LEG-486).
const GateStepType = "gate"

// GateStop is the gate option that ends the run as stopped, in any case.
const GateStop = "Stop"

// DefaultGateOptions are a gate's options when it names none.
var DefaultGateOptions = []string{"Continue", GateStop}

// gateOptionsValid reports whether a gate's options are a list of
// different, non-empty names, or a ${{ }}.
func gateOptionsValid(value any) bool {
	if text, ok := value.(string); ok {
		return HasExpression(text)
	}
	list, ok := value.([]any)
	if !ok {
		return false
	}
	seen := map[string]bool{}
	for _, item := range list {
		name, ok := item.(string)
		name = strings.TrimSpace(name)
		if !ok || name == "" || seen[strings.ToLower(name)] {
			return false
		}
		seen[strings.ToLower(name)] = true
	}
	return true
}

// SlotNamePattern is what a named agent slot of a card must match, and
// ReservedSlots the named slots agent.run may not take: the review loop's.
// The agent service keeps the same rules; a test there keeps them equal.
const SlotNamePattern = `^[a-z][a-z0-9-]{0,31}$`

var ReservedSlots = []string{"reviewer"}

var catalog = []ExecutorSpec{
	{
		Type: "agent.run", Group: "Agents", Title: "Run an agent",
		Description: "Start an agent of the workflow on a prompt or a macro, and wait for it to complete the step with the outputs the step declares.",
		With: []Field{
			promptField.at(1, true), macroField.at(2, true), agentField.at(5, false), tierField.at(6, false),
			{Name: "name", Kind: KindString, Label: "Agent's name", Description: "The agent's name in lists of agents. It does not rename the run or its task", Order: 7},
			{Name: "working_dir", Kind: KindPath, Label: "Works in", Description: "Directory the agent works in", Essential: true, Order: 3},
			{Name: "repo", Kind: KindRepository, Label: "Or a clone of", Description: "GitHub repository (owner/name) the agent works in a fresh clone of; with macro, also the pull request's repository", Essential: true, Order: 4},
			{Name: "ref", Kind: KindString, Label: "Clone starts from", Description: "With repo: the branch, tag or commit the clone starts from", Order: 8},
			{Name: "branch", Kind: KindString, Label: "Branch to work on", Description: "With repo: the branch the agent works on", Order: 9},
			{Name: "engram", Kind: KindString, Label: "Engram", Description: "An engram for the agent to be; the step fails when engram does not list it", Checked: true, Invalid: "must be the name of an engram", Order: 10},
			literalFalse("keep_agent", "Keep the agent running", "Leave the agent running once its outputs are in, so the operator can go on talking to it").at(11, false),
			prNumberFld.at(12, false),
			literalFalse("scratch", "Work in a scratch folder", "The work needs no folder of its own: the agent works in a new folder Legato removes when the step completes (kept when it fails or keeps its agent); anything to keep goes in the step's outputs").at(13, false),
			{Name: "task", Kind: KindTaskOrID, Label: "As the task's own agent", Description: "A task, or a task ID: the agent is that task's own, on its card, in a fresh clone of repo on the task's branch; a macro is sent as one started on the card is. The step fails when the task already has an agent running in the slot", Order: 14},
			{Name: "slot", Kind: KindString, Label: "In the card's slot", Description: "With task: the card's named agent slot the agent runs in, such as triage, plan or review, beside the card's own agent, in a fresh clone of its own; main (the default) is the card's own agent, and reviewer is the review loop's",
				Literal: true, Checked: true, Pattern: SlotNamePattern, Reserved: ReservedSlots, Order: 15,
				Invalid: "must be main or a slot name: lowercase letters, digits and dashes, starting with a letter, at most 32; reviewer is the review loop's"},
			{Name: "on", Kind: KindTaskOrID, Label: "Where the agent lives", Description: "run (the default): an agent of the workflow, stopped once its outputs are in unless keep is true. Or a task, or a task ID: a plain agent in that task's main slot, on its card, which keeps running unless stop is true", Order: 16},
			literalFalse("keep", "Keep the agent running", "With on: run: leave the agent running once its outputs are in, so the operator can go on talking to it").at(17, false),
			literalFalse("reuse", "Message a running agent", "With on: <task>: when the task's agent is running, message it instead of failing the step").at(18, false),
			literalFalse("stop", "Stop the agent after", "With on: <task>: stop the task's agent once its outputs are in").at(19, false),
			{Name: "clone", Kind: KindJSON, Label: "Work in the clone of", Description: "{from: <step>}: the job clone an earlier agent step worked in, or a clone ID such as ${{ steps.x.outputs.clone_id }} (null for a fresh one). In a version 2 file, a step with repo and no clone gets a fresh clone, never one its task has", Order: 20},
		},
		Constraints: []Constraint{promptMacro, {Kind: Excludes, Fields: []string{"scratch", "working_dir"}, Checked: true}, {Kind: Excludes, Fields: []string{"scratch", "repo"}, Checked: true},
			{Kind: Excludes, Fields: []string{"task", "working_dir"}, Checked: true}, {Kind: Excludes, Fields: []string{"task", "scratch"}, Checked: true}, {Kind: Excludes, Fields: []string{"task", "name"}, Checked: true},
			{Kind: Requires, Fields: []string{"slot", "task"}, Checked: true}, {Kind: Excludes, Fields: []string{"keep", "keep_agent"}, Checked: true}},
		DeclaredOutputs: true,
		Summary:         []string{"macro", "prompt"},
		Outputs:         agentFilledOutputs,
	},
	{
		Type: "agent.message", Group: "Agents", Title: "Message an agent",
		Description: "Send a prompt or a macro to a running agent: the one an earlier step used (agent: {from: <step>}), or a task's own. A prompt reaches it tagged [legato workflow]; a macro is sent as the operator's own words. The step does not wait for the agent unless it says wait_for, or declares outputs (complete).",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Label: "The task's agent", Description: "A task, or a task ID, whose own agent (its main slot) to message", Essential: true, Order: 1}, agentFromField.at(2, true),
			promptField.at(3, true), macroField.at(4, true), prRepoField.at(5, false), prNumberFld.at(6, false),
		},
		Constraints:     []Constraint{agentOrTask, promptMacro},
		Summary:         []string{"macro", "prompt"},
		DeclaredOutputs: true,
		Outputs:         agentFilledOutputs,
	},
	{
		Type: "agent.dispatch", Group: "Agents", Title: "Send to the task's agent",
		Description: "Send a prompt or a macro to a task's agent, starting one when the task has none. The step does not wait for the agent. It is read as agent.run with on: <task> and reuse: true that does not wait; new workflows use agent.run or agent.message.",
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
		Description: "Stop a task's agent: the one an earlier step used (agent: {from: <step>}), or a task's own (its main slot). With wait_for: [ended], wait for the agent to end first (the operator closes it), then go on.",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Label: "The task's agent", Description: "The task, or a task ID, whose own agent (its main slot) to stop", Essential: true, Order: 1},
			agentFromField.at(2, true),
		},
		Constraints: []Constraint{agentOrTask},
		Summary:     []string{"task"},
	},
	{
		Type: "agent.compact", Group: "Agents", Title: "Compact the task's agent",
		Description: "Compact a task's running agent with its tool's own compaction, once its turn ends, and wait for the compaction to finish. The step fails when the agent's tool cannot be compacted from Legato, or the agent is still working after 15 minutes.",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Label: "Task", Description: "The task whose agent to compact, or a task ID: ${{ workflow.task }} for the agent an agent.run step kept", Required: true, Essential: true, Order: 1},
			{Name: "instructions", Kind: KindText, Label: "What the summary should keep", Description: "Focus instructions for the compaction; Claude Code only, and the step fails when another tool is given them", Essential: true, Order: 2},
		},
		Summary: []string{"instructions", "task"},
		Outputs: []OutputSpec{
			{Name: "tokens_before", Type: "integer", Description: "The conversation's size before the compaction, when the agent's tool reports it"},
			{Name: "tokens_after", Type: "integer", Description: "The conversation's size after the compaction, when the agent's tool reports it"},
		},
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
			{Name: "reviews", Type: "json", Description: "The reviews posted: pass, pr, url, event (APPROVE or COMMENT), approved, and approval_withheld with withheld_reason when a clean pass commented instead of approving"},
			{Name: "approval_withheld", Type: "boolean", Description: "true when the loop is approved but a pull request got a COMMENT instead of an APPROVE, because its author is outside the repository"},
			{Name: "summary_path", Type: "string", Description: "A Markdown summary of the loop"},
			{Name: "linked_task", Type: "string", Description: "The board task the pull request belongs to, or empty"},
		},
	},
	{
		Type: "burnlite.task.close", Group: "Board", Title: "Close the Burnlite ticket",
		Description: "Close the Burnlite ticket a task was imported from, with a reason and a summary. Needs Legato connected to Burnlite with write access.",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Label: "Task", Description: "The task, or a task ID, imported from Burnlite", Required: true, Essential: true, Order: 1},
			{Name: "summary", Kind: KindString, Label: "Resolution summary", Description: "One line on how it was resolved, at most 200 characters", Essential: true, Order: 2},
			{Name: "reason", Kind: KindString, Label: "Closed as", Description: "completed: the work is done; not_planned: it will not be done", Default: "completed", Enum: []string{"completed", "not_planned"}, Checked: true, Invalid: "must be completed or not_planned", Order: 3},
			literalFalse("optional", "Skip a task not from Burnlite", "A task that is not a Burnlite ticket is skipped (skipped: true) instead of failing the step").at(4, false),
		},
		Summary: []string{"task"},
		Outputs: []OutputSpec{
			{Name: "key", Type: "string", Description: "The ticket's key, or empty when skipped"},
			{Name: "closed", Type: "boolean", Description: "Whether the step closed a ticket"},
			{Name: "skipped", Type: "boolean", Description: "Whether the task was not a Burnlite ticket"},
		},
	},
	{
		Type: "ticket.create", Group: "Board", Title: "Create a ticket",
		Description: "Create a Jira issue or a Burnlite task, with Legato's own Jira settings (jira.base_url, jira.email, jira.api_token) or its Burnlite connection: no agent sees the token. A retry returns the ticket the step already created instead of creating another.",
		With: []Field{
			ticketProviderField,
			{Name: "project", Kind: KindString, Label: "Project", Description: "The Jira project key (AG) or the Burnlite project (LEG)", Required: true, Essential: true, Order: 2},
			{Name: "summary", Kind: KindString, Label: "Summary", Description: "The ticket's title, one line of at most 255 characters", Required: true, Essential: true, Order: 3},
			{Name: "description", Kind: KindText, Label: "Description", Description: "Markdown; sent to Jira as its rich text, keeping code blocks, lists, links and tables", Essential: true, Order: 4},
			{Name: "labels", Kind: KindJSON, Label: "Labels", Description: "A list of labels; Jira's hold no spaces", Order: 5},
			{Name: "type", Kind: KindString, Label: "Issue type", Description: "Jira only: the issue type, such as Bug or Task; Bug when empty", Order: 6},
			{Name: "priority", Kind: KindString, Label: "Priority", Description: "Jira only: the priority's name, such as High", Order: 7},
			{Name: "fields", Kind: KindJSON, Label: "More Jira fields", Description: "Jira only: more fields as Jira names them, such as {customfield_10020: 7}; not the fields above", Order: 8},
			{Name: "kind", Kind: KindString, Label: "Kind", Description: "Burnlite only: work, or meta for planning; work when empty", Enum: []string{"work", "meta"}, Checked: true, Invalid: "must be work or meta", Order: 9},
			{Name: "size", Kind: KindString, Label: "Size", Description: "Burnlite only: a Work task's size", Enum: []string{"Small", "Medium", "Large", "X-Large"}, Checked: true, Invalid: "must be Small, Medium, Large or X-Large", Order: 10},
			{Name: "uncertainty", Kind: KindString, Label: "Uncertainty", Description: "Burnlite only: how unsure a Work task's estimate is", Enum: []string{"Low", "Medium", "High", "Extreme"}, Checked: true, Invalid: "must be Low, Medium, High or Extreme", Order: 11},
			{Name: "milestone", Kind: KindString, Label: "Milestone", Description: "Burnlite only: the Milestone's key, such as LEG-3", Order: 12},
		},
		Summary: []string{"summary"},
		Outputs: []OutputSpec{
			{Name: "key", Type: "string", Description: "The ticket's key, such as AG-12"},
			{Name: "url", Type: "string", Description: "The ticket's page"},
			{Name: "id", Type: "string", Description: "The provider's ID of the ticket"},
			{Name: "provider", Type: "string", Description: "jira or burnlite"},
			{Name: "created", Type: "boolean", Description: "false when a retry returned the ticket an earlier attempt created"},
		},
	},
	{
		Type: "ticket.comment", Group: "Board", Title: "Comment on a ticket",
		Description: "Add a Markdown comment to a Jira issue, or a Note to a Burnlite task, with Legato's own settings or connection. A retry does not comment twice.",
		With: []Field{
			ticketProviderField,
			{Name: "key", Kind: KindString, Label: "Ticket", Description: "The ticket's key, such as AG-12", Required: true, Essential: true, Order: 2},
			{Name: "body", Kind: KindText, Label: "Comment", Description: "The comment, in Markdown", Required: true, Essential: true, Order: 3},
		},
		Summary: []string{"key"},
		Outputs: []OutputSpec{
			{Name: "url", Type: "string", Description: "The comment's address, or the Burnlite task's"},
			{Name: "id", Type: "string", Description: "The provider's ID of the comment or Note"},
			{Name: "key", Type: "string", Description: "The ticket's key"},
		},
	},
	{
		Type: "chat.message.send", Group: "Chat", Title: "Send a chat message",
		Description: "Post a message as a Slack or Discord bot set up in Settings › Integrations. Fails at once when the bot is not set up or cannot send.",
		With: []Field{
			chatBot,
			chatChannel,
			{Name: "text", Kind: KindText, Label: "Message", Description: "The message: plain text or the platform's markdown", Required: true, Essential: true, Order: 3},
			{Name: "thread_id", Kind: KindString, Label: "Reply to", Description: "A message or thread to reply to: a chat trigger's ${{ event.thread_id }} or ${{ event.message_id }}, an earlier step's message_id or thread_id, or any message's ID in the channel. On Slack it replies in that message's thread (give a thread's parent, which event.thread_id is); on Discord it is a reply to that message", Order: 4},
			{Name: "buttons", Kind: KindChatButtons, Label: "Buttons", Description: "Up to 5 buttons under the message, each a label (at most 75 characters), a value (at most 100, its own) and a style (primary or danger); a click starts the workflows whose chat.button trigger matches", Checked: true, Invalid: "must be a list of buttons", Order: 5},
		},
		Summary: []string{"text"},
		Outputs: []OutputSpec{
			{Name: "message_id", Type: "string", Description: "The sent message's ID"},
			{Name: "channel", Type: "string", Description: "The channel it was sent to"},
			{Name: "thread_id", Type: "string", Description: "What thread_id takes to reply in the same thread"},
		},
	},
	{
		Type: "chat.reaction.add", Group: "Chat", Title: "React to a chat message",
		Description: "Add a chat bot's emoji reaction to a message. A reaction the bot has already left is fine. The Slack app needs the reactions:write scope; the Discord bot needs Add Reactions and Read Message History.",
		With:        chatReactionFields,
		Summary:     []string{"emoji"},
		Outputs:     []OutputSpec{{Name: "ok", Type: "boolean", Description: "True once the reaction is there"}},
	},
	{
		Type: "chat.reaction.remove", Group: "Chat", Title: "Remove a chat reaction",
		Description: "Remove a chat bot's own emoji reaction from a message. A reaction that is not there is fine. Needs the same scope and permissions as adding one.",
		With:        chatReactionFields,
		Summary:     []string{"emoji"},
		Outputs:     []OutputSpec{{Name: "ok", Type: "boolean", Description: "True once the reaction is gone"}},
	},
	{
		Type: "chat.thread.read", Group: "Chat", Title: "Read a chat thread",
		Description: "Read the thread a chat message is in, as a Slack or Discord bot set up in Settings › Integrations, for a later step: a Slack thread, a Discord thread, or the replies a Discord message ends. A message in no thread is a thread of one.",
		With: []Field{
			chatBot,
			chatChannel,
			{Name: "thread_id", Kind: KindString, Label: "Thread", Description: "The thread or message to read, such as ${{ event.thread_id }} of a chat trigger", Required: true, Essential: true, Order: 3},
			{Name: "limit", Kind: KindInteger, Label: "Most messages", Description: "How many messages to keep, the parent included: the parent and the newest replies (1 to 200)", Default: 50, Order: 4},
			literalFalse("include_bots", "Include bots' messages", "Keep messages other bots posted, the bot's own included; the thread's parent is always kept").at(5, false),
		},
		Summary: []string{"thread_id"},
		Outputs: []OutputSpec{
			{Name: "transcript", Type: "string", Description: "The thread as plain text, oldest first: each message's author and time, then its text with what it forwards quoted; at most 16 KiB, the oldest replies left out first"},
			{Name: "messages", Type: "json", Description: "The messages in the transcript: id, author_id, author_name, at, text, forwarded, forwarded_url, parent and from_bot"},
			{Name: "count", Type: "integer", Description: "How many messages the transcript holds"},
		},
	},
	{
		Type: "github.pr.wait", Group: "Code", Title: "Wait for a pull request to merge",
		Description: "Wait until a pull request merges, or closes. Legato reads its state from GitHub when the step starts, after each refresh of a card's pull requests, and every few minutes.",
		With: []Field{
			{Name: "repo", Kind: KindRepository, Label: "Repository", Description: "GitHub repository (owner/name)", Required: true, Essential: true, Order: 1},
			{Name: "pr_number", Kind: KindInteger, Label: "Pull request number", Description: "The pull request's number", Required: true, Essential: true, Order: 2},
			{Name: "until", Kind: KindString, Label: "Wait until it is", Description: "merged: the step completes when the pull request merges, and fails when it closes without merging; closed: it completes when the pull request merges or closes", Default: "merged", Enum: []string{"merged", "closed"}, Checked: true, Invalid: "must be merged or closed", Order: 3},
			{Name: "task", Kind: KindTaskOrID, Label: "The card", Description: "The task, or a task ID, whose pull request it is: its needs-you item is on that card, and Resume agent starts that card's agent. The run's task input when empty", Order: 4},
			{Name: "notify", Kind: KindBoolean, Label: "Ask me to review and merge", Description: "While it waits, a needs-you item asks the operator to review and merge the pull request, and says when it conflicts or CI fails", Default: true, Literal: true, Checked: true, Invalid: "must be true or false", Order: 5},
		},
		Summary: []string{"repo"},
		Outputs: []OutputSpec{
			{Name: "state", Type: "string", Description: "merged or closed"},
			{Name: "merged", Type: "boolean", Description: "Whether the pull request merged"},
			{Name: "merged_at", Type: "string", Description: "When it merged (RFC 3339), or empty"},
			{Name: "url", Type: "string", Description: "The pull request's address"},
		},
	},
	workflowRunSpec,
	{
		Type: GateStepType, Group: "Control", Title: "Ask the operator",
		Description: "Hold the run until the operator chooses one of the options, from a needs-you item or the run page. The step's choice output is the option chosen; Stop ends the run as stopped.",
		With: []Field{
			{Name: "title", Kind: KindString, Label: "Question", Description: "What the operator is asked, the needs-you item's title", Required: true, Essential: true, Order: 1},
			{Name: "body", Kind: KindText, Label: "Details", Description: "More for the operator to read before choosing: Markdown", Essential: true, Order: 2},
			{Name: "body_path", Kind: KindPath, Label: "Details from a file", Description: "A file whose Markdown is the details: an earlier step's file output, ${{ steps.<id>.outputs.<file>.path }}; at most 64 KiB is read", Order: 3},
			{Name: "options", Kind: KindJSON, Label: "Options", Description: "The choices, a list of names; Continue and Stop when empty. Stop, in any case, ends the run as stopped", Order: 4,
				Checked: true, Invalid: "must be a list of different, non-empty names"},
		},
		Constraints: []Constraint{{Kind: Excludes, Fields: []string{"body", "body_path"}, Checked: true}},
		Summary:     []string{"title"},
		Outputs:     []OutputSpec{{Name: "choice", Type: "string", Description: "The option the operator chose"}},
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
		Type: "agent.interview", Group: "Learn", Title: "Ask the agent to reflect",
		Description: "Ask a task's running agent what it learned, and keep its answer as evidence for Legato Learn. The agent must be running.",
		With: []Field{
			{Name: "task", Kind: KindTaskOrID, Label: "Task", Description: "The task, or a task ID", RequiredAtRun: true, Essential: true, Order: 1},
			{Name: "prompt", Kind: KindText, Label: "Question", Description: "The question; Legato's reflection prompt when empty", Order: 2},
		},
		Summary: []string{"task"},
		Outputs: []OutputSpec{
			{Name: "available", Type: "boolean"}, {Name: "request_id", Type: "string"},
			{Name: "reason", Type: "string", Description: "Why the agent could not be asked"},
		},
	},
	{
		Type: "learning.packet.prepare", Group: "Learn", Title: "Prepare a learning packet",
		Description: "Gather a task's evidence and the learnings it may touch into a bounded packet, for a curator agent to read.",
		With: []Field{
			{Name: "task", Kind: KindJSON, Label: "Task", Description: "The task: a task reference, or an object with its id", Required: true, Essential: true, Order: 1},
			{Name: "limits", Kind: KindJSON, Label: "Limits", Description: "max_items and max_bytes", Required: true, Essential: true, Order: 2},
			{Name: "project", Kind: KindJSON, Label: "Project", Description: "key and label of the project; found from the evidence when absent", Order: 3},
		},
		Summary: []string{"task"},
		Outputs: []OutputSpec{
			{Name: "generation_id", Type: "string"}, {Name: "packet_digest", Type: "string"}, {Name: "packet", Type: "json"},
		},
	},
	{
		Type: "learning.proposals.submit", Group: "Learn", Title: "Submit learning proposals",
		Description: "Check a curator's operations against its packet and keep them as candidates on the Learning page. Nothing is accepted for you.",
		With: []Field{
			{Name: "operations", Kind: KindJSON, Label: "Operations", Description: "The curator's operations document", Required: true, Essential: true, Order: 1},
			{Name: "generation", Kind: KindJSON, Label: "Packet", Description: "The packet step's generation_id, in place of attempt_id", Essential: true, Order: 2},
			{Name: "attempt_id", Kind: KindString, Label: "Curator's attempt", Description: "The curator's attempt", Order: 3},
		},
		Summary:     []string{"generation"},
		Constraints: []Constraint{{Kind: AtLeastOne, Fields: []string{"attempt_id", "generation"}, Checked: true}},
		Outputs: []OutputSpec{
			{Name: "generation_id", Type: "string"}, {Name: "packet_digest", Type: "string"}, {Name: "status", Type: "string"},
			{Name: "replayed", Type: "boolean"}, {Name: "summary", Type: "json"},
		},
	},
}
