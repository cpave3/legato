package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// Kinds of link the graph service draws between two agents. The web UI
// draws the rest from the agent list itself: sub-agents, a swarm's
// conductor and workers, a task's reviewer slot and its author, and agents
// in one worktree.
const (
	// AgentLinkPeerMessage: one agent messaged another lately (legato agent
	// message, broadcast, message_peer).
	AgentLinkPeerMessage = "peer_message"
	// AgentLinkReviewLoop: a workflow run's reviewer and the agent whose
	// pull request it reviews.
	AgentLinkReviewLoop = "review_loop"
	// AgentLinkReeveWatch: the Reeve watches the agent.
	AgentLinkReeveWatch = "reeve_watch"
)

// Kinds of badge on an agent of the graph.
const (
	// AgentBadgeHandoff: the agent's task was handed to this Legato by a
	// main instance.
	AgentBadgeHandoff = "handoff"
	// AgentBadgeDelegation: an integration (Argus) delegated a case to the
	// agent's task.
	AgentBadgeDelegation = "delegation"
)

// PeerMessageWindow is how long a peer message keeps its link on the graph.
const PeerMessageWindow = 30 * time.Minute

// repoCacheFor is how long the repository of a directory is remembered.
const repoCacheFor = 5 * time.Minute

// AgentGraph is what the web UI's graph of agents needs that the agent list
// does not say: where each running agent works, and the links between them
// that only Legato's records show.
type AgentGraph struct {
	// Agents is each running agent's place, by agent ID (the agent list's
	// task_id).
	Agents map[string]AgentGraphPlace `json:"agents"`
	Links  []AgentLink                `json:"links"`
	// PeerWindowSeconds is how long a peer message's link stays, so the
	// web UI can fade it.
	PeerWindowSeconds int `json:"peer_window_seconds"`
}

// AgentGraphPlace is where an agent works.
type AgentGraphPlace struct {
	// Repo is the repository: owner/name when known, else the folder
	// name of its Git worktree; "" for an agent outside Git.
	Repo string `json:"repo,omitempty"`
	// Worktree is the root of its Git worktree; agents in one are peers.
	Worktree string       `json:"worktree,omitempty"`
	Badges   []AgentBadge `json:"badges,omitempty"`
}

// AgentBadge marks an agent with where its work came from.
type AgentBadge struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// AgentLink is a link between two running agents, by agent ID.
type AgentLink struct {
	Kind string `json:"kind"`
	From string `json:"from"`
	To   string `json:"to"`
	// Count and LastAt are a peer message link's: how many messages in
	// the window, and when the last was sent.
	Count  int    `json:"count,omitempty"`
	LastAt string `json:"last_at,omitempty"`
	// Label says more of the link, such as a review loop's pass.
	Label string `json:"label,omitempty"`
}

// AgentGraphAgents lists the agents the graph is of (AgentService).
type AgentGraphAgents interface {
	ListAgents(ctx context.Context) ([]AgentSession, error)
}

// AgentGraphOptions are what the graph service reads.
type AgentGraphOptions struct {
	Store  *store.Store
	Agents AgentGraphAgents
	// Related gives the open review loops; nil draws none.
	Related *RelatedAgentsService
	// Inspector reads a checkout's repository; nil leaves only job
	// clones' and worktree folders' names.
	Inspector CheckoutInspector
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// AgentGraphService builds the agent graph for the web UI.
type AgentGraphService struct {
	opts  AgentGraphOptions
	mu    sync.Mutex
	repos map[string]repoPlace
}

type repoPlace struct {
	repo, worktree string
	at             time.Time
}

// NewAgentGraphService reads agents and links through opts.
func NewAgentGraphService(opts AgentGraphOptions) *AgentGraphService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &AgentGraphService{opts: opts, repos: map[string]repoPlace{}}
}

// Graph is the running agents' places and the links between them.
func (s *AgentGraphService) Graph(ctx context.Context) (AgentGraph, error) {
	agents, err := s.opts.Agents.ListAgents(ctx)
	if err != nil {
		return AgentGraph{}, err
	}
	graph := AgentGraph{Agents: map[string]AgentGraphPlace{}, Links: []AgentLink{}, PeerWindowSeconds: int(PeerMessageWindow / time.Second)}
	running := map[string]bool{}
	for _, agent := range agents {
		if agent.Status != "running" {
			continue
		}
		running[agent.TaskID] = true
		place, err := s.place(ctx, agent)
		if err != nil {
			return AgentGraph{}, err
		}
		graph.Agents[agent.TaskID] = place
	}
	add := func(link AgentLink) {
		if link.From != link.To && running[link.From] && running[link.To] {
			graph.Links = append(graph.Links, link)
		}
	}
	for _, build := range []func(context.Context, func(AgentLink)) error{s.peerLinks, s.reviewLinks, s.reeveLinks} {
		if err := build(ctx, add); err != nil {
			return AgentGraph{}, err
		}
	}
	return graph, nil
}

// place is where an agent works and what marks it.
func (s *AgentGraphService) place(ctx context.Context, agent AgentSession) (AgentGraphPlace, error) {
	repo, worktree := s.repoOf(agent.WorkingDir)
	place := AgentGraphPlace{Repo: repo, Worktree: worktree}
	if agent.CloneRepo != "" {
		place.Repo = agent.CloneRepo
	}
	if IsNamedSlot(agent.Slot) || agent.CardTaskID == "" {
		return place, nil
	}
	handoff, err := s.opts.Store.OpenHandoffForTask(ctx, agent.CardTaskID)
	switch {
	case err == nil:
		label := "from main"
		if handoff.MainName != "" {
			label = "from " + handoff.MainName
		}
		place.Badges = append(place.Badges, AgentBadge{Kind: AgentBadgeHandoff, Label: label, Detail: handoff.MainTaskID})
	case !errors.Is(err, store.ErrNotFound):
		return AgentGraphPlace{}, err
	}
	delegations, err := s.opts.Store.ListDelegationsForTask(ctx, agent.CardTaskID)
	if err != nil {
		return AgentGraphPlace{}, err
	}
	for _, d := range delegations {
		if d.State == store.DelegationFailed {
			continue
		}
		place.Badges = append(place.Badges, AgentBadge{Kind: AgentBadgeDelegation, Label: integrationLabel(d.Integration) + " case", Detail: d.CaseID})
		break
	}
	return place, nil
}

// repoOf is a directory's repository and Git worktree root, remembered
// for a while: each costs a git run.
func (s *AgentGraphService) repoOf(dir string) (string, string) {
	if strings.TrimSpace(dir) == "" {
		return "", ""
	}
	now := s.opts.Now()
	s.mu.Lock()
	cached, ok := s.repos[dir]
	s.mu.Unlock()
	if ok && now.Sub(cached.at) < repoCacheFor {
		return cached.repo, cached.worktree
	}
	place := repoPlace{at: now}
	if root, err := gitWorktreeRoot(dir); err == nil {
		place.worktree, place.repo = root, filepath.Base(root)
		if s.opts.Inspector != nil {
			if code, err := s.opts.Inspector.InspectCodeContext(root); err == nil && code != nil && code.Repo != "" {
				place.repo = code.Repo
			}
		}
	}
	s.mu.Lock()
	s.repos[dir] = place
	s.mu.Unlock()
	return place.repo, place.worktree
}

// peerLinks are the peer messages of the window, one link for each sender
// and recipient.
func (s *AgentGraphService) peerLinks(ctx context.Context, add func(AgentLink)) error {
	rows, err := s.opts.Store.PeerMessageLinksSince(ctx, s.opts.Now().Add(-PeerMessageWindow))
	if err != nil {
		return err
	}
	for _, row := range rows {
		add(AgentLink{Kind: AgentLinkPeerMessage, From: row.SenderTaskID, To: row.RecipientTaskID, Count: row.Count, LastAt: row.LastAt})
	}
	return nil
}

// reviewLinks are the open workflow review loops: the run's reviewer to the
// agent that authors the pull request, or to the linked board task's
// agent when no agent authors it. A handed-off card's loop is between its
// own slots, which the web UI links from the agent list.
func (s *AgentGraphService) reviewLinks(ctx context.Context, add func(AgentLink)) error {
	if s.opts.Related == nil {
		return nil
	}
	loops, err := s.opts.Related.openRunLoops(ctx)
	if err != nil {
		return err
	}
	for _, loop := range sortedRunLoops(loops) {
		review, err := s.opts.Store.GetHandoffReview(ctx, loop.taskID)
		if err != nil {
			return err
		}
		if review == nil || review.Phase == ReviewPhaseEnded {
			continue
		}
		author := loop.run.AuthorTask
		if author == "" {
			author = loop.run.LinkedTask
		}
		if author == "" {
			continue
		}
		label := ""
		if review.Pass > 0 {
			label = fmt.Sprintf("pass %d", review.Pass)
		}
		add(AgentLink{Kind: AgentLinkReviewLoop, From: AgentSlotKey(loop.taskID, SlotReviewer), To: author, Label: label})
	}
	return nil
}

// reeveLinks are the agents the Reeve watches.
func (s *AgentGraphService) reeveLinks(ctx context.Context, add func(AgentLink)) error {
	watches, err := s.opts.Store.ListReeveWatches(ctx)
	if err != nil {
		return err
	}
	for _, watch := range watches {
		add(AgentLink{Kind: AgentLinkReeveWatch, From: watch.ReeveTaskID, To: watch.TaskID})
	}
	return nil
}

// integrationLabel is how an integration is named on a badge.
func integrationLabel(name string) string {
	if name == "" {
		return "Integration"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
