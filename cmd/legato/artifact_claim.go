package main

import (
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

func writeClaimResult(command string, claim, previous *store.ArtifactClaim, jsonMode bool) int {
	data := map[string]any{"claim": claim}
	if previous != nil {
		data["previous_claim"] = previous
	}
	text := fmt.Sprintf("Claimed %s %s for agent task %s (generation %d).\n",
		claim.ArtifactKind, claim.ArtifactID, claim.ClaimantTaskID, claim.Generation)
	return writeCommandSuccess(command, data, text, jsonMode)
}
