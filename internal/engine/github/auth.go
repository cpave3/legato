package github

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
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

type Credential struct {
	Token  string
	Source CredentialSource
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

func (execTokenCommand) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// ResolveCredential returns one in-memory credential without exposing command
// output in failures. Auto mode prefers explicit configuration over gh.
func ResolveCredential(options AuthOptions) (Credential, error) {
	mode := options.Mode
	if mode == "" {
		mode = AuthAuto
	}
	token := strings.TrimSpace(options.Token)
	switch mode {
	case AuthToken:
		if token == "" {
			return Credential{}, errors.New("GitHub auth mode token requires github.token")
		}
		return Credential{Token: token, Source: CredentialConfigured}, nil
	case AuthGitHubCLI:
		return resolveGitHubCLICredential(options.Command)
	case AuthAuto:
		if token != "" {
			return Credential{Token: token, Source: CredentialConfigured}, nil
		}
		return resolveGitHubCLICredential(options.Command)
	default:
		return Credential{}, fmt.Errorf("invalid GitHub auth mode %q; expected auto, gh, or token", mode)
	}
}

func resolveGitHubCLICredential(command TokenCommand) (Credential, error) {
	if command == nil {
		command = execTokenCommand{}
	}
	output, err := command.Run("gh", "auth", "token")
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return Credential{}, errors.New("GitHub authentication unavailable; run gh auth login or configure github.token")
	}
	return Credential{Token: strings.TrimSpace(string(output)), Source: CredentialGitHubCLI}, nil
}
