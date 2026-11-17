package service

import (
	"context"
	"log"
	"slices"
	"sort"

	"github.com/cpave3/legato/internal/engine/version"
)

// The handoff features a build supports, by name. A main reads them from a
// worker's /api/meta (handoff_features) and its advert, and refuses or
// degrades per feature, instead of the protocol version changing with each
// (see docs/ai/integrations.md § What a worker advertises).
const (
	// HandoffFeaturePushedBranch: a handoff may start from a branch
	// already on GitHub (kind pushed-branch).
	HandoffFeaturePushedBranch = "pushed_branch"
	// HandoffFeatureTicket: a handoff may carry its ticket's text (ticket).
	HandoffFeatureTicket = "ticket"
	// HandoffFeatureReviewOverride: a handoff may override the worker's
	// review passes (review).
	HandoffFeatureReviewOverride = "review_override"
	// HandoffFeatureReturn: Return, Recall and Archive bring a task back.
	HandoffFeatureReturn = "return"
	// HandoffFeatureDrop: Drop, Leave and Take back.
	HandoffFeatureDrop = "drop"
	// HandoffFeatureEvents: the live Events stream and Pull.
	HandoffFeatureEvents = "events"
	// HandoffFeatureCapabilities: the capabilities route, this advert.
	HandoffFeatureCapabilities = "capabilities"
)

// HandoffFeatures are the handoff features this build supports.
func HandoffFeatures() []string {
	return []string{
		HandoffFeatureCapabilities, HandoffFeatureDrop, HandoffFeatureEvents, HandoffFeaturePushedBranch,
		HandoffFeatureReturn, HandoffFeatureReviewOverride, HandoffFeatureTicket,
	}
}

// HandoffCapabilitiesPath is the worker's route for its advert. Only a
// paired main's handoff token reaches it: the lists in it are not for a
// stranger on the network.
const HandoffCapabilitiesPath = "/api/integrations/handoff/capabilities"

// HandoffAdvert is what a worker says it can do for a handoff: what the
// agent a main hands it work for will have. A main keeps it on the worker's
// record (Worker.Advert), read at pairing and on each connection.
type HandoffAdvert struct {
	// Version is the worker's version (vX.Y.Z, or dev), and Commit its
	// commit when the build knows it.
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	// Features are the handoff features it supports (HandoffFeatures).
	Features []string `json:"features"`
	// Sandbox says whether its agents can run in Legato's sandbox; a
	// worker without it refuses every handoff.
	Sandbox bool `json:"sandbox"`
	// Agents are the agent tools it knows, and DefaultAgent the one a
	// handoff that names none gets.
	Agents       []HandoffAdvertAgent `json:"agents"`
	DefaultAgent string               `json:"default_agent,omitempty"`
	// MCPTools says whether its agents get Legato's tools
	// (agents.mcp.enabled): add_task_note, submit_review and the rest.
	MCPTools bool `json:"mcp_tools"`
	// MCPConnections names its MCP connections that are on.
	MCPConnections []string `json:"mcp_connections"`
	// Skills names its Legato-managed skills that are on and usable.
	Skills []string `json:"skills"`
	// Repositories are the owner/name patterns its GitHub credentials
	// list (settings.repositories): what it can clone.
	Repositories []string `json:"repositories"`
}

// HandoffAdvertAgent is one agent tool a worker knows: whether its command
// is installed on the host, its tiers, and whether it can run with its
// permission prompts off, which a handed-off agent needs.
type HandoffAdvertAgent struct {
	Name       string   `json:"name"`
	Installed  bool     `json:"installed"`
	Tiers      []string `json:"tiers"`
	PromptsOff bool     `json:"prompts_off"`
}

// Has says whether the advert lists a feature.
func (a *HandoffAdvert) Has(feature string) bool {
	return a != nil && slices.Contains(a.Features, feature)
}

// HandoffAdvertSources are where a worker's advert comes from, besides its
// agent service and sandbox. Each is asked at every advert; a nil one adds
// nothing (an agent tool counts as installed when Installed is nil).
type HandoffAdvertSources struct {
	// Installed says whether an agent tool's command is on this host.
	Installed func(kind string) bool
	// Tiers are an agent tool's configured tiers.
	Tiers func(kind string) []string
	// MCPTools says whether agents get Legato's tools.
	MCPTools func() bool
	// MCPConnections names the MCP connections that are on.
	MCPConnections func(ctx context.Context) ([]string, error)
	// Skills names the Legato-managed skills that are on and usable.
	Skills func(ctx context.Context) ([]string, error)
	// Repositories are the patterns the GitHub credentials list.
	Repositories func() []string
}

// SetAdvertSources sets where the advert's lists come from.
func (s *HandoffWorkerService) SetAdvertSources(src HandoffAdvertSources) {
	s.advertMu.Lock()
	defer s.advertMu.Unlock()
	s.advert = src
}

// Advert is what this worker can do for a handoff, as it stands now. A
// source that fails is logged and left out: the advert is never refused.
func (s *HandoffWorkerService) Advert(ctx context.Context) HandoffAdvert {
	s.advertMu.Lock()
	src := s.advert
	s.advertMu.Unlock()
	advert := HandoffAdvert{
		Version: version.String(), Commit: version.Commit(), Features: HandoffFeatures(),
		Sandbox: s.opts.Sandbox != nil && s.opts.Sandbox(),
		Agents:  []HandoffAdvertAgent{}, MCPConnections: []string{}, Skills: []string{}, Repositories: []string{},
	}
	if s.opts.Agents != nil {
		advert.DefaultAgent = s.opts.Agents.DefaultAdapter()
		names := s.opts.Agents.RegisteredAdapters()
		if d := advert.DefaultAgent; d != "" && !slices.Contains(names, d) {
			names = append(names, d)
		}
		for _, name := range names {
			agent := HandoffAdvertAgent{Name: name, Installed: src.Installed == nil || src.Installed(name), Tiers: []string{}}
			_, agent.PromptsOff = s.opts.Agents.AdapterFor(name).(PromptsOffAdapter)
			if src.Tiers != nil {
				agent.Tiers = sortedCopy(src.Tiers(name))
			}
			advert.Agents = append(advert.Agents, agent)
		}
		sort.Slice(advert.Agents, func(i, j int) bool { return advert.Agents[i].Name < advert.Agents[j].Name })
	}
	if src.MCPTools != nil {
		advert.MCPTools = src.MCPTools()
	}
	advert.MCPConnections = advertList(ctx, "mcp connections", src.MCPConnections)
	advert.Skills = advertList(ctx, "skills", src.Skills)
	if src.Repositories != nil {
		advert.Repositories = sortedCopy(src.Repositories())
	}
	return advert
}

func advertList(ctx context.Context, what string, list func(context.Context) ([]string, error)) []string {
	if list == nil {
		return []string{}
	}
	names, err := list(ctx)
	if err != nil {
		log.Printf("handoff advert: %s: %v", what, err)
		return []string{}
	}
	return sortedCopy(names)
}

func sortedCopy(list []string) []string {
	out := append([]string{}, list...)
	sort.Strings(out)
	return slices.Compact(out)
}
