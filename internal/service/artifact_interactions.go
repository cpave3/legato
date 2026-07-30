package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

var ErrInteractionOffline = errors.New("agent interaction route is unavailable")

type ArtifactInteractionService struct {
	store  *store.Store
	tmux   TmuxManager
	agents AgentService
}

func NewArtifactInteractionService(s *store.Store, tmux TmuxManager) *ArtifactInteractionService {
	return &ArtifactInteractionService{store: s, tmux: tmux}
}

// SetAgentService wires the agent service for Chimera-aware delivery.
// When set, artifact interactions for Chimera agents are delivered via
// the HTTP API instead of tmux send-keys.
func (d *ArtifactInteractionService) SetAgentService(a AgentService) {
	d.agents = a
}

func (d *ArtifactInteractionService) Claim(ctx context.Context, kind, artifactID, claimantTaskID string) (*store.ArtifactClaim, *store.ArtifactClaim, error) {
	if claimantTaskID == "" {
		return nil, nil, fmt.Errorf("claim requires an agent task ID")
	}
	if _, err := d.store.GetTask(ctx, claimantTaskID); err != nil {
		return nil, nil, err
	}
	return d.store.ClaimArtifact(ctx, kind, artifactID, claimantTaskID)
}

func (d *ArtifactInteractionService) Enqueue(ctx context.Context, kind, artifactID, revisionID, threadID, interactionKind, body, fallbackTaskID string) (*store.ArtifactInteraction, bool, error) {
	claim, err := d.store.GetArtifactClaim(ctx, kind, artifactID)
	if err != nil {
		if err != store.ErrNotFound {
			return nil, false, err
		}
		claim, _, err = d.store.ClaimArtifact(ctx, kind, artifactID, fallbackTaskID)
		if err != nil {
			return nil, false, err
		}
	}
	interaction := store.ArtifactInteraction{
		ID: randomPlanID("pi-"), ArtifactKind: kind, ArtifactID: artifactID,
		RevisionID: revisionID, ThreadID: threadID, Kind: interactionKind, Body: body,
		ClaimantTaskID: claim.ClaimantTaskID, ClaimGeneration: claim.Generation,
	}
	if err := d.store.InsertArtifactInteraction(ctx, interaction); err != nil {
		return nil, false, err
	}
	delivered, deliveryErr := d.Deliver(ctx, &interaction)
	if errors.Is(deliveryErr, ErrInteractionOffline) {
		deliveryErr = nil
	}
	return &interaction, delivered, deliveryErr
}

func (d *ArtifactInteractionService) Deliver(ctx context.Context, interaction *store.ArtifactInteraction) (bool, error) {
	// For Chimera agents, use the HTTP API via AgentService.SendMessage.
	if d.agents != nil && d.agents.HasChimeraEndpoint(ctx, interaction.ClaimantTaskID) {
		if err := d.agents.SendMessage(ctx, interaction.ClaimantTaskID, interaction.Body); err != nil {
			_ = d.store.MarkArtifactInteractionDelivery(ctx, interaction.ID, false, err.Error())
			return false, err
		}
		_ = d.store.MarkArtifactInteractionDelivery(ctx, interaction.ID, true, "")
		return true, nil
	}
	if d.tmux == nil {
		_ = d.store.MarkArtifactInteractionDelivery(ctx, interaction.ID, false, "tmux unavailable")
		return false, ErrInteractionOffline
	}
	session, err := d.store.GetAgentSessionByTaskID(ctx, interaction.ClaimantTaskID)
	if err != nil {
		_ = d.store.MarkArtifactInteractionDelivery(ctx, interaction.ID, false, err.Error())
		return false, ErrInteractionOffline
	}
	alive, err := d.tmux.IsAlive(session.TmuxSession)
	if err != nil || !alive {
		_ = d.store.MarkArtifactInteractionDelivery(ctx, interaction.ID, false, "agent session unavailable")
		return false, ErrInteractionOffline
	}
	if err := d.tmux.SendKeysLine(session.TmuxSession, interaction.Body); err != nil {
		_ = d.store.MarkArtifactInteractionDelivery(ctx, interaction.ID, false, err.Error())
		return false, err
	}
	_ = d.store.MarkArtifactInteractionDelivery(ctx, interaction.ID, true, "")
	return true, nil
}

func (d *ArtifactInteractionService) Retry(ctx context.Context) {
	interactions, err := d.store.ListUndeliveredArtifactInteractions(ctx)
	if err != nil {
		return
	}
	for i := range interactions {
		_, _ = d.Deliver(ctx, &interactions[i])
	}
}

func (d *ArtifactInteractionService) Start(ctx context.Context, interval time.Duration) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		d.Retry(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.Retry(ctx)
			}
		}
	}()
	return cancel
}

func (d *ArtifactInteractionService) Inbox(ctx context.Context, claimantTaskID string, acknowledge bool) ([]store.ArtifactInteraction, error) {
	items, err := d.store.ListArtifactInteractionsForClaimant(ctx, claimantTaskID, true)
	if err != nil {
		return nil, err
	}
	if acknowledge {
		ids := make([]string, len(items))
		for i := range items {
			ids[i] = items[i].ID
		}
		if err := d.store.AcknowledgeArtifactInteractions(ctx, claimantTaskID, ids); err != nil {
			return nil, err
		}
	}
	if items == nil {
		items = []store.ArtifactInteraction{}
	}
	return items, nil
}
