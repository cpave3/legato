package main

import (
	"github.com/cpave3/legato/config"
	gh "github.com/cpave3/legato/internal/engine/github"
)

func buildGitHubClient(cfg *config.Config, command gh.TokenCommand) (*gh.Client, gh.CredentialSource, error) {
	credential, err := gh.ResolveCredential(gh.AuthOptions{
		Mode:    gh.AuthMode(cfg.GitHub.Auth),
		Token:   cfg.GitHub.Token,
		Command: command,
	})
	if err != nil {
		return nil, "", err
	}
	client, err := gh.New(gh.Options{Token: credential.Token})
	if err != nil {
		return nil, "", err
	}
	return client, credential.Source, nil
}
