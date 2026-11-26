package service

import (
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// approvalStandsLine says that legato-reviews' approval still stands on
// GitHub (spec LEG-559 story 20, AC 36), at the head each open layer was
// approved at: "legato-reviews' approval at abc1234 still stands", or, for
// a stack, "... at abc1234 (#21), def5678 (#22) still stands". "" when no
// open layer has an approved head. The run page shows it while a steward is
// held or stuck; the steward's Attention items say it too.
func approvalStandsLine(layers []store.ReviewLoopPR) string {
	var heads []store.ReviewLoopPR
	for _, layer := range layers {
		if layer.CleanHead != "" && (layer.State == "" || layer.State == "open") {
			heads = append(heads, layer)
		}
	}
	switch len(heads) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("legato-reviews' approval at %s still stands", short(heads[0].CleanHead))
	}
	parts := make([]string, 0, len(heads))
	for _, layer := range heads {
		parts = append(parts, fmt.Sprintf("%s (#%d)", short(layer.CleanHead), layer.Number))
	}
	return "legato-reviews' approval at " + strings.Join(parts, ", ") + " still stands"
}
