package claudeusage

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
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
	login, err := n.loginFile()
	if n.darwin && errors.Is(err, ErrNotSignedIn) {
		return Login{}, ErrNamedKeychainUnavailable
	}
	return login, err
}

func (n namedCredentials) loginFile() (Login, error) {
	root, err := os.OpenRoot(filepath.Dir(n.file.Path))
	if errors.Is(err, os.ErrNotExist) {
		return Login{}, ErrNotSignedIn
	}
	if err != nil {
		return Login{}, errors.New("claude: named credentials directory is unavailable")
	}
	defer root.Close()
	file, err := root.OpenFile(filepath.Base(n.file.Path), os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return Login{}, ErrNotSignedIn
	}
	if err != nil {
		return Login{}, errors.New("claude: named credentials must be an account-local regular file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Login{}, errors.New("claude: named credentials must be an account-local regular file")
	}
	return loginFromReader(file)
}
