package main

import (
	"github.com/cpave3/legato/config"
	gh "github.com/cpave3/legato/internal/engine/github"
)

func buildGitHubClient(cfg *config.Config, command gh.TokenCommand) (*gh.Client, gh.CredentialSource, error) {
	return gh.NewAuthenticatedClient(gh.AuthOptions{
		Mode:    gh.AuthMode(cfg.GitHub.Auth),
		Token:   cfg.GitHub.Token,
		Command: command,
	})
}
