package github

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type AuthMode string

const (
	AuthAuto      AuthMode = "auto"
	AuthGitHubCLI AuthMode = "gh"
	AuthToken     AuthMode = "token"
)

type CredentialSource string

const (
	CredentialConfigured CredentialSource = "configured"
	CredentialGitHubCLI  CredentialSource = "gh"
)

type credential struct {
	token  string
	source CredentialSource
}

type TokenCommand interface {
	Run(name string, args ...string) ([]byte, error)
}

type AuthOptions struct {
	Mode    AuthMode
	Token   string
	Command TokenCommand
}

type execTokenCommand struct{}

// tokenCommandTimeout bounds `gh auth token`, so a gh stuck on a prompt or
// in a loop does not hold up what needed the token.
var tokenCommandTimeout = 15 * time.Second

func (execTokenCommand) Run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tokenCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// Do not wait for children that outlive gh and hold its output open.
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

// ResolveCredential returns one in-memory credential without exposing command
// output in failures. Auto mode prefers explicit configuration over gh.
func resolveCredential(options AuthOptions) (credential, error) {
	mode := options.Mode
	if mode == "" {
		mode = AuthAuto
	}
	token := strings.TrimSpace(options.Token)
	switch mode {
	case AuthToken:
		if token == "" {
			return credential{}, errors.New("GitHub auth mode token requires github.token")
		}
		return credential{token: token, source: CredentialConfigured}, nil
	case AuthGitHubCLI:
		return resolveGitHubCLICredential(options.Command)
	case AuthAuto:
		if token != "" {
			return credential{token: token, source: CredentialConfigured}, nil
		}
		return resolveGitHubCLICredential(options.Command)
	default:
		return credential{}, fmt.Errorf("invalid GitHub auth mode %q; expected auto, gh, or token", mode)
	}
}

// NewAuthenticatedClient resolves one credential and constructs the read-only
// client without exposing secret-bearing credential values to callers.
func NewAuthenticatedClient(options AuthOptions) (*Client, CredentialSource, error) {
	resolved, err := resolveCredential(options)
	if err != nil {
		return nil, "", err
	}
	client, err := New(Options{Token: resolved.token})
	if err != nil {
		return nil, "", err
	}
	return client, resolved.source, nil
}

func resolveGitHubCLICredential(command TokenCommand) (credential, error) {
	if command == nil {
		command = execTokenCommand{}
	}
	output, err := command.Run("gh", "auth", "token")
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return credential{}, errors.New("GitHub authentication unavailable; run gh auth login or configure github.token")
	}
	return credential{token: strings.TrimSpace(string(output)), source: CredentialGitHubCLI}, nil
}
