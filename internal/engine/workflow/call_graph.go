package workflow

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// The call graph (LEG-710): every workflow as a node, and an edge from a
// workflow to each workflow its workflow.run steps may run, for the
// Workflows page's map. It is read from the files alone, as CallDiagnostics
// reads them.

// CallGraph is every workflow and the calls between them.
type CallGraph struct {
	Nodes []CallGraphNode `json:"nodes"`
	Edges []CallGraphEdge `json:"edges"`
}

// CallGraphNode is a workflow: a definition, a file that does not parse
// (Broken, with its Error), or a name a call names that nothing defines
// (Missing).
type CallGraphNode struct {
	// ID is the workflow's name; a broken file that shares a defined
	// workflow's name is "file:<path>".
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Title    string        `json:"title,omitempty"`
	Tags     []string      `json:"tags,omitempty"`
	Source   string        `json:"source,omitempty"`
	BuiltIn  bool          `json:"built_in,omitempty"`
	Broken   bool          `json:"broken,omitempty"`
	Error    string        `json:"error,omitempty"`
	Missing  bool          `json:"missing,omitempty"`
	Triggers []TriggerInfo `json:"triggers,omitempty"`
}

// CallGraphEdge is one workflow calling another: Calls steps of Source run
// Target. InLoop is set when one of them is in a loop's body; Dynamic when
// one names Target only as a choice of an input, so a run may call another.
type CallGraphEdge struct {
	ID      string   `json:"id"`
	Source  string   `json:"source"`
	Target  string   `json:"target"`
	Calls   int      `json:"calls"`
	InLoop  bool     `json:"in_loop,omitempty"`
	Dynamic bool     `json:"dynamic,omitempty"`
	Steps   []string `json:"steps"`
}

// BuildCallGraph is the call graph of discovered workflows: the
// definitions in their order, then the files that failed to parse, then
// the names called that none of them defines. A workflow that calls
// itself has no edge to itself.
func BuildCallGraph(result DiscoveryResult) CallGraph {
	b := callGraphBuilder{graph: CallGraph{Nodes: []CallGraphNode{}, Edges: []CallGraphEdge{}}, ids: map[string]string{}, edges: map[[2]string]int{}}
	for _, definition := range result.Definitions {
		if definition != nil {
			b.addDefinition(definition)
		}
	}
	for _, failure := range result.Errors {
		b.addBroken(failure)
	}
	for _, definition := range result.Definitions {
		if definition != nil {
			b.addCalls(definition)
		}
	}
	return b.graph
}

// callGraphBuilder holds a graph while it is built: the node ID of each
// name, and the edge of each caller and callee.
type callGraphBuilder struct {
	graph CallGraph
	ids   map[string]string
	edges map[[2]string]int
}

func (b *callGraphBuilder) addDefinition(definition *Definition) {
	b.ids[definition.Name] = definition.Name
	b.graph.Nodes = append(b.graph.Nodes, CallGraphNode{
		ID: definition.Name, Name: definition.Name, Title: definition.Title, Tags: definition.Tags,
		Source: definition.Source, BuiltIn: strings.HasPrefix(definition.Source, builtInSourcePrefix),
		Triggers: definition.On.List(),
	})
}

// addBroken adds a file that failed to parse; an error about a folder is
// not a workflow.
func (b *callGraphBuilder) addBroken(failure DiscoveryError) {
	if info, err := os.Stat(failure.Path); err == nil && info.IsDir() {
		return
	}
	name, title := brokenName(failure.Path)
	id := name
	if _, taken := b.ids[name]; taken {
		id = "file:" + failure.Path
	} else {
		b.ids[name] = id
	}
	b.graph.Nodes = append(b.graph.Nodes, CallGraphNode{ID: id, Name: name, Title: title, Source: failure.Path, Broken: true, Error: failure.Error})
}

func (b *callGraphBuilder) addCalls(definition *Definition) {
	eachCall(definition.Steps, "", false, func(step Step, path string, inLoop bool) {
		called, _ := CalledWorkflows(step, definition.Inputs)
		dynamic := HasExpression(step.With["workflow"].(string))
		for _, name := range called {
			if name != definition.Name {
				b.addCall(definition.Name, b.target(name), path, inLoop, dynamic)
			}
		}
	})
}

// target is the node a called name is: a definition's, a broken file's,
// or a missing workflow's, added the first time.
func (b *callGraphBuilder) target(name string) string {
	if id, known := b.ids[name]; known {
		return id
	}
	b.ids[name] = name
	b.graph.Nodes = append(b.graph.Nodes, CallGraphNode{ID: name, Name: name, Missing: true})
	return name
}

func (b *callGraphBuilder) addCall(source, target, path string, inLoop, dynamic bool) {
	key := [2]string{source, target}
	at, seen := b.edges[key]
	if !seen {
		at = len(b.graph.Edges)
		b.edges[key] = at
		b.graph.Edges = append(b.graph.Edges, CallGraphEdge{ID: source + "->" + target, Source: source, Target: target, Steps: []string{}})
	}
	edge := &b.graph.Edges[at]
	edge.Calls++
	edge.InLoop = edge.InLoop || inLoop
	edge.Dynamic = edge.Dynamic || dynamic
	edge.Steps = append(edge.Steps, path)
}

// eachCall calls f with every workflow.run step, loops' bodies included,
// with its path of step IDs (outer.inner.step) and whether it is in a loop.
func eachCall(steps []Step, prefix string, inLoop bool, f func(step Step, path string, inLoop bool)) {
	for _, step := range steps {
		path := prefix + step.ID
		if step.Loop != nil {
			eachCall(step.Steps, path+".", true, f)
			continue
		}
		if step.Type != WorkflowRunStepType {
			continue
		}
		if _, isText := step.With["workflow"].(string); isText {
			f(step, path, inLoop)
		}
	}
}

// brokenName is the name and title a file that does not parse gives
// itself, read loosely, else its file name without .workflow.yaml.
func brokenName(path string) (string, string) {
	var loose struct {
		Name  string `yaml:"name"`
		Title string `yaml:"title"`
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(data, &loose)
	}
	if name := strings.TrimSpace(loose.Name); name != "" {
		return name, strings.TrimSpace(loose.Title)
	}
	base := filepath.Base(path)
	for _, suffix := range []string{".workflow.yaml", ".workflow.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix), strings.TrimSpace(loose.Title)
		}
	}
	return base, strings.TrimSpace(loose.Title)
}
