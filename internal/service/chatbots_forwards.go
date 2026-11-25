package service

import (
	"strings"

	"github.com/cpave3/legato/internal/engine/discordbot"
	"github.com/cpave3/legato/internal/engine/slackbot"
)

// chatForward is one forwarded message, as quoted for a ChatEvent.
type chatForward struct {
	author, where, url, text string
}

// withForwards sets e's forward fields from the messages it forwards.
func (e ChatEvent) withForwards(forwards []chatForward) ChatEvent {
	if len(forwards) == 0 {
		return e
	}
	quotes := make([]string, 0, len(forwards))
	for _, f := range forwards {
		if e.ForwardedURL == "" {
			e.ForwardedURL = f.url
		}
		quotes = append(quotes, quoteForward(f))
	}
	e.Forwarded = true
	e.ForwardedText = strings.Join(quotes, "\n\n")
	return e
}

// quoteForward is a forwarded message as a Markdown quote under a line
// that says whose it was and where it came from, as far as is known.
func quoteForward(f chatForward) string {
	header := "> Forwarded message"
	if f.author != "" {
		header = "> Forwarded from " + f.author
	}
	if f.where != "" {
		if f.author != "" {
			header += " in " + f.where
		} else {
			header += " from " + f.where
		}
	}
	lines := []string{header + ":"}
	if f.text != "" {
		for _, line := range strings.Split(f.text, "\n") {
			lines = append(lines, strings.TrimRight("> "+line, " "))
		}
	}
	return strings.Join(lines, "\n")
}

// slackForwards are a Slack message's shares: by the author's name (else
// their ID), in the channel's name (else the message's link).
func slackForwards(shares []slackbot.Share) []chatForward {
	var out []chatForward
	for _, s := range shares {
		f := chatForward{author: s.AuthorName, where: s.FromURL, url: s.FromURL, text: s.Text}
		if f.author == "" && s.AuthorID != "" {
			f.author = "<@" + s.AuthorID + ">"
		}
		if s.ChannelName != "" {
			f.where = "#" + s.ChannelName
		}
		out = append(out, f)
	}
	return out
}

// discordForwards are a Discord message's forwards. A snapshot names no
// author, so the quote says where the message is: its link.
func discordForwards(forwards []discordbot.Forward) []chatForward {
	var out []chatForward
	for _, f := range forwards {
		url := discordMessageURL(f.GuildID, f.ChannelID, f.MessageID)
		out = append(out, chatForward{where: url, url: url, text: f.Content})
	}
	return out
}

// discordMessageURL is a message's link; "" without its channel and ID.
func discordMessageURL(guild, channel, message string) string {
	if channel == "" || message == "" {
		return ""
	}
	if guild == "" {
		guild = "@me"
	}
	return "https://discord.com/channels/" + guild + "/" + channel + "/" + message
}
