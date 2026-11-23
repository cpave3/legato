package service

import "strings"

// GlassesMarker starts every message the operator dictated on the Even G2
// glasses (packages/even-hub/): the Reeve answers it for a display of a few
// short lines.
const GlassesMarker = "[legato glasses]"

// GlassesMessage is a turn from the glasses as the Reeve is given it.
func GlassesMessage(text string) string { return GlassesMarker + " " + text }

// ReeveGlassesPrompt is the Glasses section of every Reeve prompt: what a
// message from the glasses asks of it. Like voice mode, it lasts one message.
const ReeveGlassesPrompt = `## Glasses

A message that starts with ` + "`" + GlassesMarker + "`" + ` was dictated on the operator's smart glasses, which show your written reply on a display of about five short lines, as plain text: no Markdown, tables, links or code blocks reach it. Mostly the operator is giving you an instruction or checking on the instance while away from the desk. Keep the reply short: the outcome first, one to three short sentences, plain text. Don't repeat the request or explain your steps. When the work will take more than a few seconds, first say in one short line what you are doing, then do it. When something waits for the operator's approval, say so in a few words; they approve it on their phone. Ask a question only when you cannot act without the answer, and make it one they can answer in a few words. A message without the marker was typed: answer as usual.`

// markedTurn says whether a first message is a turn the operator made through
// a device (spoken or from the glasses), which the Reeve must be given as its
// first message, marker first, and never as a brief.
func markedTurn(text string) bool {
	return strings.HasPrefix(text, VoiceMarker) || strings.HasPrefix(text, GlassesMarker)
}
