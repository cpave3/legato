package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// GraphVersion is the version of the workflow graph model. It changes when
// the model does, as SchemaVersion changes with the definition. Version 2
// (LEG-484) draws loops with their bodies, if: as branches, and names a
// step's start as start.
const GraphVersion = 2

// The kinds of node in a workflow graph.
const (
	NodeTrigger = "trigger"
	NodeStep    = "step"
	// NodeInner is a node inside a step's group, such as the review loop's
	// review pass; its Parent is the step's node.
	NodeInner = "inner"
)

// The groups a step is drawn as (GraphStep.Group).
const (
	// GroupReviewLoop is a github.pr.review_passes step: a review pass and
	// the author's fixes, round and round.
	GroupReviewLoop = "review_loop"
	// GroupLoop is a loop step: its body's steps are nodes whose Parent is
	// the loop's node.
	GroupLoop = "loop"
)

// The kinds of edge in a workflow graph.
const (
	// EdgeStart leads from a trigger to the step a run starts at.
	EdgeStart = "start"
	// EdgeOrder leads from a step to the step that runs after it.
	EdgeOrder = "order"
	// EdgeData leads from a step to a later step that uses its outputs.
	EdgeData = "data"
	// EdgeLoop joins the nodes of a group that repeats: a loop to its
	// pass's first step and its pass's last step back to it, or the review
	// loop's pass and fixes.
	EdgeLoop = "loop"
	// EdgeBranch leads into a step with if: (with its Condition), and past
	// it (Skip) to where the run goes when the step is skipped.
	EdgeBranch = "branch"
)

// The gates a step may be: a step that does not start by itself.
const (
	// GateManual waits for the operator.
	GateManual = "manual"
	// GateAgentEnded waits for the agent before it to end: a version 1
	// file's trigger: agent_ended.
	GateAgentEnded = TriggerAgentEnded
)

// TriggerManual is the trigger kind of the start node every workflow has:
// run by hand.
const TriggerManual = "manual"

// Graph is the workflow graph: the drawing of a workflow's triggers and
// steps, built from its definition. It is read-only; the definition is the
// source of truth.
type Graph struct {
	Version int         `json:"version"`
	Nodes   []GraphNode `json:"nodes"`
	Edges   []GraphEdge `json:"edges"`
}

// GraphNode is one node: a trigger or a step. Its ID is stable for the same
// definition: "trigger:<kind>" or "step:<step path>", where a loop body's
// step's path is the loop's, a slash and its ID, without a pass
// (step:each/build).
type GraphNode struct {
	ID      string        `json:"id"`
	Kind    string        `json:"kind"`
	Trigger *GraphTrigger `json:"trigger,omitempty"`
	Step    *GraphStep    `json:"step,omitempty"`
	Inner   *GraphInner   `json:"inner,omitempty"`
	// Parent is, for a node inside a group, the group's node: a loop's for
	// its body's steps.
	Parent string `json:"parent,omitempty"`
}

// GraphInner is a node inside a group.
type GraphInner struct {
	// Role is what it stands for in its group: pass or fixes in the review
	// loop.
	Role  string `json:"role"`
	Title string `json:"title"`
}

// GraphTrigger is a trigger node: one way the workflow starts.
type GraphTrigger struct {
	Kind string `json:"kind"`
	// Summary says when it fires, in words: "Card enters Triage".
	Summary string `json:"summary"`
	// Enabled is what the file says, before any switch in a Legato.
	Enabled bool `json:"enabled"`
	// Confirm is whether it asks the operator before each run.
	Confirm bool `json:"confirm,omitempty"`
	// MaxRunsPerDay is the loop stop's limit, the default when the file
	// says none; 0 for the manual start, which has no loop stop.
	MaxRunsPerDay int `json:"max_runs_per_day,omitempty"`
	// Definition is the trigger as the definition's on: declares it.
	Definition any `json:"definition,omitempty"`
}

// GraphStep is a step node.
type GraphStep struct {
	ID string `json:"id"`
	// Path is the step's path without a pass: its ID, or in a loop's body
	// the loop's path, a slash and its ID. A run's step instance at
	// each/2/build is the node of path each/build.
	Path  string `json:"path"`
	Title string `json:"title"`
	// Type is the step's executor, LoopStepType for a loop.
	Type string `json:"type"`
	// Start is how the step starts: automatic, manual, or from a version 1
	// file agent_ended.
	Start string `json:"start"`
	// Gate is GateManual or GateAgentEnded for a step that does not start
	// by itself, else empty.
	Gate string `json:"gate,omitempty"`
	// Group is GroupReviewLoop or GroupLoop for a step drawn as a group of
	// nodes, which keeps the step's own node ID.
	Group string `json:"group,omitempty"`
	// Loop says what a loop goes over.
	Loop *GraphLoop `json:"loop,omitempty"`
	// Key is the one value that says what the step does.
	Key GraphKey `json:"key"`
	// Definition is the step as its definition declares it.
	Definition Step `json:"definition"`
}

// GraphLoop is a loop step's loop: its form (over, next or repeat), its
// most passes, and what it goes over or ends on, in words.
type GraphLoop struct {
	Form    string `json:"form"`
	Max     int    `json:"max"`
	Summary string `json:"summary,omitempty"`
	Until   string `json:"until,omitempty"`
}

// GraphKey is a step's key value: the field it comes from in the step's
// with, and its text. Both are empty for a step with nothing to show.
type GraphKey struct {
	Field string `json:"field,omitempty"`
	Value string `json:"value,omitempty"`
}

// GraphEdge joins two nodes. Its ID is stable for the same definition.
type GraphEdge struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Target string `json:"target"`
	// Outputs is, on a data edge, the source step's output keys the target
	// uses, sorted.
	Outputs []string `json:"outputs,omitempty"`
	// Condition is, on a branch edge into a step, its if: in words.
	Condition string `json:"condition,omitempty"`
	// Skip marks the branch edge past a step with if:, taken when it is
	// skipped.
	Skip bool `json:"skip,omitempty"`
}

func triggerNodeID(kind string) string { return "trigger:" + kind }
func stepNodeID(path string) string    { return "step:" + path }

func edge(kind, source, target string) GraphEdge {
	return GraphEdge{ID: kind + ":" + source + ">" + target, Kind: kind, Source: source, Target: target}
}

// BuildGraph builds the workflow graph of a parsed definition.
func BuildGraph(d *Definition) *Graph {
	graph := &Graph{Version: GraphVersion, Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	manual := triggerNodeID(TriggerManual)
	graph.Nodes = append(graph.Nodes, GraphNode{ID: manual, Kind: NodeTrigger, Trigger: &GraphTrigger{Kind: TriggerManual, Summary: "Run by hand", Enabled: true}})
	triggers := []string{manual}
	for _, kind := range TriggerKinds {
		common, ok := d.On.Common(kind)
		if !ok {
			continue
		}
		summary, definition := triggerSummary(d.On, kind)
		id := triggerNodeID(kind)
		graph.Nodes = append(graph.Nodes, GraphNode{ID: id, Kind: NodeTrigger, Trigger: &GraphTrigger{
			Kind: kind, Summary: summary, Enabled: enabledByDefault(common.Enabled), Confirm: common.Confirm, MaxRunsPerDay: common.Limit(), Definition: definition,
		}})
		triggers = append(triggers, id)
	}
	builder := &graphBuilder{graph: graph, seen: map[string]bool{}}
	builder.scope(d.Steps, "", "", triggers, EdgeStart, "", map[string]string{})
	graph.Edges = append(graph.Edges, builder.flushData()...)
	return graph
}

type graphBuilder struct {
	graph *Graph
	seen  map[string]bool
	data  []GraphEdge
}

func (b *graphBuilder) add(e GraphEdge) {
	if b.seen[e.ID] {
		return
	}
	b.seen[e.ID] = true
	b.graph.Edges = append(b.graph.Edges, e)
}

// scope draws one steps: list at path (the run's own, or a loop's body),
// whose first step is reached from entries by edges of kind enter, and
// whose last leads to exit ("" for the run's own: the run ends). visible
// maps the IDs readable before it to their nodes, nearest first.
func (b *graphBuilder) scope(steps []Step, path, parent string, entries []string, enter, exit string, visible map[string]string) {
	visible = copyStrings(visible)
	previous, kind := entries, enter
	for i, step := range steps {
		node := stepNodeID(StepPath(path, step.ID))
		b.stepNode(step, StepPath(path, step.ID), parent)
		if step.If != nil {
			condition := conditionText(*step.If)
			next := exit
			if i+1 < len(steps) {
				next = stepNodeID(StepPath(path, steps[i+1].ID))
			}
			for _, source := range previous {
				into := edge(EdgeBranch, source, node)
				into.Condition = condition
				b.add(into)
				if next != "" {
					past := edge(EdgeBranch, source, next)
					past.Skip = true
					b.add(past)
				}
			}
		} else {
			for _, source := range previous {
				b.add(edge(kind, source, node))
			}
		}
		if step.Loop != nil {
			b.loopBody(step, StepPath(path, step.ID), node, visible)
		}
		b.dataInto(step, node, visible)
		visible[step.ID] = node
		previous, kind = []string{node}, EdgeOrder
	}
	if exit != "" && len(steps) > 0 {
		b.add(edge(EdgeLoop, previous[0], exit))
	}
}

// loopBody draws a loop's pass: its next: query, then its steps.
func (b *graphBuilder) loopBody(step Step, path, node string, visible map[string]string) {
	entries := []string{node}
	enter := EdgeLoop
	body := copyStrings(visible)
	if step.Loop.Next != nil {
		query := Step{ID: NextStepID, Title: "Next item", Type: step.Loop.Next.Type, With: step.Loop.Next.With, Start: StartAutomatic}
		queryPath := StepPath(path, NextStepID)
		b.stepNode(query, queryPath, node)
		b.add(edge(EdgeLoop, node, stepNodeID(queryPath)))
		b.dataInto(query, stepNodeID(queryPath), visible)
		entries, enter = []string{stepNodeID(queryPath)}, EdgeOrder
		body[NextStepID] = stepNodeID(queryPath)
	}
	b.scope(step.Steps, path, node, entries, enter, node, body)
	// The pass's result is mapped from the body's steps.
	inner := copyStrings(visible)
	for _, s := range step.Steps {
		inner[s.ID] = stepNodeID(StepPath(path, s.ID))
	}
	uses := map[string]map[string]bool{}
	for _, output := range step.Outputs {
		collectUses(output.Value, step.ID, uses)
	}
	b.dataEdges(uses, node, inner)
}

func (b *graphBuilder) stepNode(step Step, path, parent string) {
	start := step.Start
	if start == "" {
		start = StartAutomatic
	}
	gate := ""
	if start == GateManual || start == GateAgentEnded {
		gate = start
	}
	stepType := step.Type
	if step.Loop != nil {
		stepType = LoopStepType
	}
	id := stepNodeID(path)
	graphStep := &GraphStep{ID: step.ID, Path: path, Title: step.Title, Type: stepType, Start: start, Gate: gate, Key: stepKey(step), Definition: step}
	if step.Loop != nil {
		graphStep.Group = GroupLoop
		graphStep.Loop = graphLoop(*step.Loop)
	}
	b.graph.Nodes = append(b.graph.Nodes, GraphNode{ID: id, Kind: NodeStep, Step: graphStep, Parent: parent})
	if step.Type == "github.pr.review_passes" {
		graphStep.Group = GroupReviewLoop
		pass, fixes := id+"/pass", id+"/fixes"
		b.graph.Nodes = append(b.graph.Nodes,
			GraphNode{ID: pass, Kind: NodeInner, Parent: id, Inner: &GraphInner{Role: "pass", Title: "Review pass"}},
			GraphNode{ID: fixes, Kind: NodeInner, Parent: id, Inner: &GraphInner{Role: "fixes", Title: "Author fixes"}})
		b.add(edge(EdgeLoop, pass, fixes))
		b.add(edge(EdgeLoop, fixes, pass))
	}
}

func graphLoop(loop Loop) *GraphLoop {
	out := &GraphLoop{Max: loop.Limit()}
	switch {
	case loop.Next != nil:
		out.Form, out.Summary = "next", "Next item of "+loop.Next.Type
	case loop.Repeat:
		out.Form, out.Summary = "repeat", "Repeat"
	default:
		out.Form = "over"
		if text, ok := loop.Over.(string); ok {
			out.Summary = "Each of " + text
		} else if items, ok := loop.Over.([]any); ok {
			out.Summary = fmt.Sprintf("Each of %d items", len(items))
		}
	}
	if loop.Until != nil {
		out.Until = conditionText(*loop.Until)
	}
	return out
}

// dataInto draws the data edges into a step's node from the steps it
// reads, found by ID in visible (nearest first). A loop's own reads its
// over, query, key and until here; its pass outputs read its body's steps
// (loopBody).
func (b *graphBuilder) dataInto(step Step, node string, visible map[string]string) {
	b.dataEdges(ownOutputsUsed(step), node, visible)
}

func (b *graphBuilder) dataEdges(uses map[string]map[string]bool, node string, visible map[string]string) {
	sources := make([]string, 0, len(uses))
	for source := range uses {
		if visible[source] != "" && visible[source] != node {
			sources = append(sources, source)
		}
	}
	sort.Strings(sources)
	for _, source := range sources {
		keys := make([]string, 0, len(uses[source]))
		for key := range uses[source] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		data := edge(EdgeData, visible[source], node)
		data.Outputs = keys
		b.data = append(b.data, data)
	}
}

// flushData gives the data edges, drawn after the other edges.
func (b *graphBuilder) flushData() []GraphEdge {
	var out []GraphEdge
	for _, data := range b.data {
		if !b.seen[data.ID] {
			b.seen[data.ID] = true
			out = append(out, data)
		}
	}
	b.data = nil
	return out
}

func copyStrings(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// conditionText is a condition in words: "steps.a.outputs.choice equals
// Continue", all: joined by and, any: by or, not: as not (…).
func conditionText(c Condition) string {
	var parts []string
	group := func(items []Condition, join string) {
		var texts []string
		for _, item := range items {
			text := conditionText(item)
			if len(item.All)+len(item.Any) > 1 {
				text = "(" + text + ")"
			}
			texts = append(texts, text)
		}
		if len(texts) > 0 {
			parts = append(parts, strings.Join(texts, join))
		}
	}
	group(c.All, " and ")
	group(c.Any, " or ")
	if c.Not != nil {
		parts = append(parts, "not ("+conditionText(*c.Not)+")")
	}
	if c.Field != "" {
		value := func(v any) string {
			if items, ok := v.([]any); ok {
				var texts []string
				for _, item := range items {
					texts = append(texts, fmt.Sprint(item))
				}
				return strings.Join(texts, ", ")
			}
			return fmt.Sprint(v)
		}
		switch {
		case c.Exists != nil && *c.Exists:
			parts = append(parts, c.Field+" exists")
		case c.Exists != nil:
			parts = append(parts, c.Field+" does not exist")
		case c.Empty != nil && *c.Empty:
			parts = append(parts, c.Field+" is empty")
		case c.Empty != nil:
			parts = append(parts, c.Field+" is not empty")
		case c.Equals != nil:
			parts = append(parts, c.Field+" equals "+value(c.Equals))
		case c.NotEquals != nil:
			parts = append(parts, c.Field+" does not equal "+value(c.NotEquals))
		case c.In != nil:
			parts = append(parts, c.Field+" in "+value(c.In))
		case c.GT != nil:
			parts = append(parts, c.Field+" > "+value(c.GT))
		case c.LT != nil:
			parts = append(parts, c.Field+" < "+value(c.LT))
		default:
			parts = append(parts, c.Field)
		}
	}
	return strings.Join(parts, " and ")
}

// ownOutputsUsed is the outputs of other steps that a step reads itself,
// by step: from its with, output paths and if:, and for a loop from its
// over, query, key and until (not its body's or its pass outputs).
func ownOutputsUsed(step Step) map[string]map[string]bool {
	uses := map[string]map[string]bool{}
	for _, value := range step.With {
		collectUses(value, step.ID, uses)
	}
	for _, output := range step.Outputs {
		collectUses(output.Path, step.ID, uses)
	}
	// An if: reads its fields as bare paths, as does a chat button's.
	conditions := chatButtonConditions(step)
	if step.If != nil {
		conditions = append(conditions, *step.If)
	}
	if step.Loop != nil {
		collectUses(step.Loop.Over, step.ID, uses)
		collectUses(step.Loop.Key, step.ID, uses)
		if step.Loop.Next != nil {
			collectUses(step.Loop.Next.With, step.ID, uses)
		}
		if step.Loop.Until != nil {
			conditions = append(conditions, *step.Loop.Until)
		}
	}
	for _, condition := range conditions {
		condition.eachField(func(field string) {
			if path, err := ParsePath(field); err == nil {
				usePath(path, step.ID, uses)
			}
		})
	}
	return uses
}

// stepOutputsUsed is the outputs of other steps that a step references, by
// step, a loop's body's reads from outside it included.
func stepOutputsUsed(step Step) map[string]map[string]bool {
	uses := ownOutputsUsed(step)
	if step.Loop == nil {
		return uses
	}
	for _, output := range step.Outputs {
		collectUses(output.Value, step.ID, uses)
	}
	for _, inner := range step.Steps {
		for source, keys := range stepOutputsUsed(inner) {
			if uses[source] == nil {
				uses[source] = map[string]bool{}
			}
			for key := range keys {
				uses[source][key] = true
			}
		}
	}
	for _, inner := range step.Steps {
		delete(uses, inner.ID)
	}
	return uses
}

// collectUses adds the steps.<id>.outputs.<key> a value's ${{ }} read,
// read with the expression pattern validation reads them with.
func collectUses(value any, self string, uses map[string]map[string]bool) {
	switch nested := value.(type) {
	case map[string]any:
		for _, item := range nested {
			collectUses(item, self, uses)
		}
	case []any:
		for _, item := range nested {
			collectUses(item, self, uses)
		}
	case string:
		for _, match := range findExpressions(nested) {
			expression, err := ParseExpression(match.inner)
			if err != nil {
				continue
			}
			for _, operand := range expression.Operands {
				usePath(operand.Path, self, uses)
			}
		}
	}
}

func usePath(path []PathPart, self string, uses map[string]map[string]bool) {
	parts := pathNames(path)
	if len(parts) >= 4 && parts[0] == "steps" && parts[2] == "outputs" && parts[1] != self {
		if uses[parts[1]] == nil {
			uses[parts[1]] = map[string]bool{}
		}
		uses[parts[1]][parts[3]] = true
	}
}

// stepKey chooses the one value of a step's with that says what it does.
func stepKey(step Step) GraphKey {
	first := func(fields ...string) GraphKey {
		for _, field := range fields {
			if value, ok := step.With[field]; ok {
				if text := keyText(value); text != "" {
					return GraphKey{Field: field, Value: text}
				}
			}
		}
		return GraphKey{}
	}
	switch step.Type {
	case "agent.run", "agent.dispatch", "agent.message", "agent.interview":
		return first("macro", "prompt")
	case "agent.compact":
		return first("instructions")
	case "task.move":
		return first("column")
	case "github.pr.review_passes", "github.pr.prepare", "github.pr.wait":
		repo, number := keyText(step.With["repo"]), keyText(step.With["pr_number"])
		if number == "" {
			number = keyText(step.With["number"])
		}
		if repo != "" && number != "" {
			return GraphKey{Field: "pr", Value: repo + "#" + number}
		}
		return first("repo")
	case "repo.ensure":
		return first("repo")
	case "file.remove", "worktree.remove":
		return first("path", "working_dir")
	case "task.note":
		return first("body", "body_path")
	case "chat.message.send":
		return first("text", "channel")
	case "chat.reaction.add", "chat.reaction.remove":
		return first("emoji", "message_id")
	case "chat.thread.read":
		return first("thread_id", "channel")
	case "ticket.create":
		return first("summary", "project")
	case "ticket.comment":
		return first("key")
	case GateStepType:
		return first("title")
	case "task.metadata.set":
		if values, ok := step.With["values"].(map[string]any); ok && len(values) > 0 {
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			return GraphKey{Field: "values", Value: strings.Join(keys, ", ")}
		}
	}
	return GraphKey{}
}

// keyText is a with value as one line of text: the first line that is not
// blank, for a value of several lines.
func keyText(value any) string {
	switch value.(type) {
	case nil, map[string]any, []any:
		return ""
	}
	for _, line := range strings.Split(fmt.Sprint(value), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// triggerSummary says when a declared trigger fires, and gives the trigger
// as declared.
func triggerSummary(on Triggers, kind string) (string, any) {
	switch kind {
	case TriggerTaskEnteredColumn:
		return "Card enters " + on.TaskEnteredColumn.Column, on.TaskEnteredColumn
	case TriggerSchedule:
		return on.Schedule.Describe(), on.Schedule
	case TriggerPRChangesRequested:
		return "Changes requested on a PR", on.PRChangesRequested
	case TriggerChatMessage:
		return on.Chat(kind).describe(kind), on.ChatMessage
	case TriggerChatMention:
		return on.Chat(kind).describe(kind), on.ChatMention
	case TriggerChatButton:
		return on.ChatButton.describe(), on.ChatButton
	}
	return kind, nil
}

// Digest is a definition's digest as a run records it: "sha256:" and the
// hex SHA-256 of its JSON. A run whose digest differs from its workflow's
// current definition ran a copy that has since changed.
func Digest(d *Definition) string {
	data, _ := json.Marshal(d)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
