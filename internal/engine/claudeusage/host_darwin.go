//go:build darwin

package claudeusage

import (
	"context"
	"os/exec"
	"time"
)

// keychainService is the Keychain item Claude Code keeps its login in on
// macOS.
const keychainService = "Claude Code-credentials"

// HostCredentials is where Claude Code keeps its login on macOS: the
// Keychain, else the credentials file.
func HostCredentials(getenv func(string) string, home string) CredentialSource {
	return keychainCredentials{file: FileCredentials{Path: CredentialsPath(getenv, home)}}
}

type keychainCredentials struct {
	file FileCredentials
}

func (k keychainCredentials) Login() (Login, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", keychainService, "-w").Output()
	if err != nil || len(out) == 0 {
		return k.file.Login()
	}
	return parseLogin(out)
}
