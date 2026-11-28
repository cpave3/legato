package server

import (
	"net/http"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// workflowMapNode is a call graph node as GET /api/workflows/graph gives
// it: its triggers as this Legato has them, on or paused here.
type workflowMapNode struct {
	workflowengine.CallGraphNode
	Triggers []service.WorkflowTriggerState `json:"triggers,omitempty"`
}

// workflowMapHandler serves GET /api/workflows/graph (LEG-710): every
// workflow as a node, built-ins and broken files included, and an edge for
// each workflow a workflow's workflow.run steps may run. The Workflows
// page's map draws it.
func (s *Server) workflowMapHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		graph := workflowengine.BuildCallGraph(s.discoverWorkflowsIn(r.Context()))
		var states map[string][]service.WorkflowTriggerState
		if s.workflowTriggers != nil {
			var err error
			if states, err = s.workflowTriggers.TriggerStates(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		nodes := make([]workflowMapNode, 0, len(graph.Nodes))
		for _, node := range graph.Nodes {
			out := workflowMapNode{CallGraphNode: node}
			if here, ok := states[node.ID]; ok && !node.Broken {
				out.Triggers = here
			} else {
				for _, trigger := range node.Triggers {
					out.Triggers = append(out.Triggers, service.WorkflowTriggerState{TriggerInfo: trigger, Enabled: trigger.EnabledByDefault})
				}
			}
			nodes = append(nodes, out)
		}
		s.writeJSON(w, http.StatusOK, struct {
			Nodes []workflowMapNode              `json:"nodes"`
			Edges []workflowengine.CallGraphEdge `json:"edges"`
		}{nodes, graph.Edges})
	}
}
