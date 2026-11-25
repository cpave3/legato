package service

import (
	"fmt"
	"sort"
)

// GitHubActorLine is the line a GitHub App profile's jwt_exchange.lookup
// needs: the App's slug, whose bot, <slug>[bot], is who its posts are by.
const GitHubActorLine = `actor: "$.app_slug"`

// gitHubAppProfiles are the shipped profiles whose credentials post review
// passes and their answers, and so must name their App.
var gitHubAppProfiles = map[string]bool{"github": true, "github-reviewer": true}

// NamesNoApp reports whether p is a jwt-exchange profile whose lookup does
// not say which App its tokens act as.
func (p CredentialProfile) NamesNoApp() bool {
	return p.JWTExchange != nil && (p.JWTExchange.Lookup == nil || p.JWTExchange.Lookup.Actor == "")
}

// origin says where p was read from, for a person: its file, or Legato.
func (p CredentialProfile) origin() string {
	if p.Source == "" || p.Source == "shipped" {
		return "shipped with Legato"
	}
	return p.Source
}

// AppProblem says, for a profile that names no App, what breaks and the
// line to add, and where; "" for one that names its App.
func (p CredentialProfile) AppProblem() string {
	if !p.NamesNoApp() {
		return ""
	}
	where := "an override of it in the profiles folder"
	if p.Source != "" && p.Source != "shipped" {
		where = p.Source
	}
	return fmt.Sprintf("the %s profile (%s) names no App (no jwt_exchange.lookup.actor): review passes and their answers cannot post, as Legato cannot tell its App's own posts from a copy. Add %s under jwt_exchange.lookup in %s",
		p.Name, p.origin(), GitHubActorLine, where)
}

// ProfilesNamingNoApp lists, by name, the operator's overrides of the
// github and github-reviewer profiles that name no App: posting with
// their credentials fails until each gains GitHubActorLine.
func ProfilesNamingNoApp(profiles map[string]CredentialProfile) []CredentialProfile {
	var out []CredentialProfile
	for name, p := range profiles {
		if gitHubAppProfiles[name] && p.Source != "shipped" && p.NamesNoApp() {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
