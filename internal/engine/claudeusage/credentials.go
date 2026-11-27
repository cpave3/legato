package claudeusage

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/cpave3/legato/internal/leaf/regularfile"
)

// credentialsLimit bounds the credentials file, which is a few kilobytes.
const credentialsLimit = 1 << 20

// CredentialsPath is the file Claude Code keeps its login in on Linux:
// $CLAUDE_CONFIG_DIR/.credentials.json, else ~/.claude/.credentials.json.
func CredentialsPath(getenv func(string) string, home string) string {
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".credentials.json")
	}
	return filepath.Join(home, ".claude", ".credentials.json")
}

// FileCredentials reads the login from Claude Code's credentials file. It
// only ever reads the file.
type FileCredentials struct {
	Path string
}

// Login reads the file's Claude.ai login. The folder is one a sandboxed
// Claude Code agent can write, so only a regular file is read.
func (f FileCredentials) Login() (Login, error) {
	file, err := regularfile.Open(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Login{}, ErrNotSignedIn
	}
	if err != nil {
		return Login{}, errors.New("claude: reading the credentials file failed")
	}
	defer file.Close()
	return loginFromReader(file)
}

func loginFromReader(file io.Reader) (Login, error) {
	data, err := io.ReadAll(io.LimitReader(file, credentialsLimit))
	if err != nil {
		return Login{}, errors.New("claude: reading the credentials file failed")
	}
	return parseLogin(data)
}

// parseLogin reads claudeAiOauth's token and expiry, ignoring the rest.
func parseLogin(data []byte) (Login, error) {
	var creds struct {
		ClaudeAiOauth *struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return Login{}, errors.New("claude: the credentials are not JSON")
	}
	if creds.ClaudeAiOauth == nil || creds.ClaudeAiOauth.AccessToken == "" {
		return Login{}, ErrNotSignedIn
	}
	login := Login{AccessToken: creds.ClaudeAiOauth.AccessToken}
	if creds.ClaudeAiOauth.ExpiresAt > 0 {
		login.ExpiresAt = time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
	}
	return login, nil
}
