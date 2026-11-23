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
// the model does, as SchemaVersion changes with the definition.
const GraphVersion = 1

// The kinds of node in a workflow graph.
const (
	NodeTrigger = "trigger"
	NodeStep    = "step"
	// NodeInner is a node inside a step's group, such as the review loop's
	// review pass; its Parent is the step's node.
	NodeInner = "inner"
)

// GroupReviewLoop is the group a github.pr.review_passes step is drawn as:
// a review pass and the author's fixes, round and round.
const GroupReviewLoop = "review_loop"

// The kinds of edge in a workflow graph.
const (
	// EdgeStart leads from a trigger to the step a run starts at.
	EdgeStart = "start"
	// EdgeOrder leads from a step to the step that runs after it.
	EdgeOrder = "order"
	// EdgeData leads from a step to a later step that uses its outputs.
	EdgeData = "data"
	// EdgeLoop joins the nodes inside a group that repeats.
	EdgeLoop = "loop"
)

// The gates a step may be: a step that does not start by itself.
const (
	// GateManual waits for the operator.
	GateManual = "manual"
	// GateAgentEnded waits for the agent before it to end.
	GateAgentEnded = TriggerAgentEnded
)

// TriggerManual is the trigger kind of the start node every workflow has:
// run by hand.
const TriggerManual = "manual"

// Graph is the workflow graph: the drawing of a workflow's triggers and
// steps, built from its definition. It is read-only; the definition is the
// source of truth. Nothing in it assumes one chain of steps: branches and
// parallel steps would only add edges.
type Graph struct {
	Version int         `json:"version"`
	Nodes   []GraphNode `json:"nodes"`
	Edges   []GraphEdge `json:"edges"`
}

// GraphNode is one node: a trigger or a step. Its ID is stable for the same
// definition: "trigger:<kind>" or "step:<step id>".
type GraphNode struct {
	ID      string        `json:"id"`
	Kind    string        `json:"kind"`
	Trigger *GraphTrigger `json:"trigger,omitempty"`
	Step    *GraphStep    `json:"step,omitempty"`
	Inner   *GraphInner   `json:"inner,omitempty"`
	// Parent is, for a node inside a group, the group's node.
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
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
	// Trigger is how the step starts: automatic, manual or agent_ended.
	Trigger string `json:"trigger"`
	// Gate is GateManual or GateAgentEnded for a step that does not start
	// by itself, else empty.
	Gate string `json:"gate,omitempty"`
	// Group is GroupReviewLoop for a step drawn as a group of inner nodes,
	// which keeps the step's own node ID.
	Group string `json:"group,omitempty"`
	// Key is the one value that says what the step does.
	Key GraphKey `json:"key"`
	// Definition is the step as its definition declares it.
	Definition Step `json:"definition"`
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
}

func triggerNodeID(kind string) string { return "trigger:" + kind }
func stepNodeID(id string) string      { return "step:" + id }

func edge(kind, source, target string) GraphEdge {
	return GraphEdge{ID: kind + ":" + source + ">" + target, Kind: kind, Source: source, Target: target}
}

// BuildGraph builds the workflow graph of a parsed definition.
func BuildGraph(d *Definition) *Graph {
	graph := &Graph{Version: GraphVersion, Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	manual := triggerNodeID(TriggerManual)
	graph.Nodes = append(graph.Nodes, GraphNode{ID: manual, Kind: NodeTrigger, Trigger: &GraphTrigger{Kind: TriggerManual, Summary: "Run by hand", Enabled: true}})
	triggers := []string{manual}
	for _, kind := range []string{TriggerTaskEnteredColumn, TriggerSchedule, TriggerPRChangesRequested} {
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
	for i, step := range d.Steps {
		trigger := step.Start
		if trigger == "" {
			trigger = "automatic"
		}
		id := stepNodeID(step.ID)
		gate := ""
		if trigger == GateManual || trigger == GateAgentEnded {
			gate = trigger
		}
		graph.Nodes = append(graph.Nodes, GraphNode{ID: id, Kind: NodeStep, Step: &GraphStep{
			ID: step.ID, Title: step.Title, Type: step.Type, Trigger: trigger, Gate: gate, Key: stepKey(step), Definition: step,
		}})
		if step.Type == "github.pr.review_passes" {
			graph.Nodes[len(graph.Nodes)-1].Step.Group = GroupReviewLoop
			pass, fixes := id+"/pass", id+"/fixes"
			graph.Nodes = append(graph.Nodes,
				GraphNode{ID: pass, Kind: NodeInner, Parent: id, Inner: &GraphInner{Role: "pass", Title: "Review pass"}},
				GraphNode{ID: fixes, Kind: NodeInner, Parent: id, Inner: &GraphInner{Role: "fixes", Title: "Author fixes"}})
			graph.Edges = append(graph.Edges, edge(EdgeLoop, pass, fixes), edge(EdgeLoop, fixes, pass))
		}
		if i == 0 {
			for _, trigger := range triggers {
				graph.Edges = append(graph.Edges, edge(EdgeStart, trigger, id))
			}
		} else {
			graph.Edges = append(graph.Edges, edge(EdgeOrder, stepNodeID(d.Steps[i-1].ID), id))
		}
	}
	for _, step := range d.Steps {
		uses := stepOutputsUsed(step)
		sources := make([]string, 0, len(uses))
		for source := range uses {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		for _, source := range sources {
			keys := make([]string, 0, len(uses[source]))
			for key := range uses[source] {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			data := edge(EdgeData, stepNodeID(source), stepNodeID(step.ID))
			data.Outputs = keys
			graph.Edges = append(graph.Edges, data)
		}
	}
	return graph
}

// stepOutputsUsed is the outputs of other steps that a step's with and
// output paths reference (steps.<id>.outputs.<key>), by step, read with the
// expression pattern validation reads them with.
func stepOutputsUsed(step Step) map[string]map[string]bool {
	uses := map[string]map[string]bool{}
	var walk func(any)
	walk = func(value any) {
		switch nested := value.(type) {
		case map[string]any:
			for _, item := range nested {
				walk(item)
			}
		case []any:
			for _, item := range nested {
				walk(item)
			}
		case string:
			for _, match := range expressionPattern.FindAllStringSubmatch(nested, -1) {
				parts := strings.Split(strings.TrimSpace(match[1]), ".")
				if len(parts) >= 4 && parts[0] == "steps" && parts[2] == "outputs" && parts[1] != step.ID {
					if uses[parts[1]] == nil {
						uses[parts[1]] = map[string]bool{}
					}
					uses[parts[1]][parts[3]] = true
				}
			}
		}
	}
	for _, value := range step.With {
		walk(value)
	}
	for _, output := range step.Outputs {
		walk(output.Path)
	}
	return uses
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
	case "agent.run", "agent.dispatch", "agent.interview":
		return first("macro", "prompt")
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
