//go:build !darwin

package claudeusage

// HostCredentials is where Claude Code keeps its login on this system: the
// credentials file.
func HostCredentials(getenv func(string) string, home string) CredentialSource {
	return FileCredentials{Path: CredentialsPath(getenv, home)}
}
