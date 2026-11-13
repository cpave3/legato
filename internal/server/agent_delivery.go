package server

import (
	"encoding/json"
	"log"

	"github.com/cpave3/legato/internal/engine/chimera"
)

// How much of what failed is written to the log: enough of the message to
// tell which it was, and the reason the agent's side gave, each cut so the
// agent's side cannot fill the log.
const (
	deliveryLogWords  = 60
	deliveryLogReason = 300
)

// logDeliveryFailure writes to Legato's log a message a native agent's
// session would not take (message_failed). The Claude Code mod reports it as
// a live event only, so it is read from the streams that follow every
// running agent, whether or not a page is open on it (LEG-151).
func logDeliveryFailure(taskID string, event chimera.EventEnvelope) {
	if event.Type != "message_failed" {
		return
	}
	var failed struct {
		Content string `json:"content"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(event.Data, &failed)
	log.Printf("agent %s: message not delivered: %q (message: %q)", taskID, cutRunes(failed.Error, deliveryLogReason), cutRunes(failed.Content, deliveryLogWords))
}

// cutRunes keeps the first limit characters of text, marking a cut with an
// ellipsis.
func cutRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
