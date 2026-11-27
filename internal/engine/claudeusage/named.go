package claudeusage

import (
	"errors"
	"path/filepath"
	"runtime"
)

// ErrNamedKeychainUnavailable refuses to guess a named account's macOS
// Keychain service. A named reader must never probe the default's login.
var ErrNamedKeychainUnavailable = errors.New("claude: named Keychain usage is unavailable; file-backed credentials are required")

// NamedCredentials reads only this named directory's file. The default
// account continues to use HostCredentials and its unchanged platform rules.
func NamedCredentials(dir string) CredentialSource {
	return namedCredentials{file: FileCredentials{Path: filepath.Join(dir, ".credentials.json")}, darwin: runtime.GOOS == "darwin"}
}

type namedCredentials struct {
	file   FileCredentials
	darwin bool
}

func (n namedCredentials) Login() (Login, error) {
	login, err := n.file.Login()
	if n.darwin && errors.Is(err, ErrNotSignedIn) {
		return Login{}, ErrNamedKeychainUnavailable
	}
	return login, err
}
