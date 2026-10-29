package credential

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Master key sources, in the order "auto" tries them.
const (
	SourceSystemdCreds  = "systemd-creds"
	SourceSecretService = "secret-service"
	SourceKeychain      = "keychain"
	SourcePassphrase    = "passphrase"
	SourceFile          = "file"
)

// MasterKeySources lists what secrets.master_key may name besides "auto".
var MasterKeySources = []string{SourceSystemdCreds, SourceSecretService, SourceKeychain, SourcePassphrase, SourceFile}

// MasterKeyOptions says where Legato's own store gets its master key.
type MasterKeyOptions struct {
	DataDir string
	// Source is "auto" or one of MasterKeySources.
	Source string
	// Passphrase, when set, can make the key (LEGATO_SECRETS_PASSPHRASE).
	Passphrase string
	Runner     Runner
	// Available reports whether a command is installed. Default exec.LookPath.
	Available func(name string) bool
	// GOOS chooses the sources that apply. Default runtime.GOOS.
	GOOS string
}

// RecordedSource names the source that made the master key of the store in
// dataDir, or "" when no key was made yet. It makes no key.
func RecordedSource(dataDir string) string {
	data, err := os.ReadFile(filepath.Join(dataDir, "secrets.source"))
	if err != nil {
		return ""
	}
	var state keyState
	if json.Unmarshal(data, &state) != nil {
		return ""
	}
	return state.Source
}

// passphraseRounds stretches a passphrase into the master key.
const passphraseRounds = 600_000

// keyState is what <data_dir>/secrets.source holds: which source made the
// key, and a value sealed under it to check the key on later starts.
type keyState struct {
	Source string `json:"source"`
	Nonce  []byte `json:"nonce"`
	Check  []byte `json:"check"`
}

const checkText = "legato master key"

// LoadMasterKey returns the master key and the source that keeps it. The
// first start takes the first source that works (for "auto") and records
// it; later starts use that source and no other, and check the key against
// the value sealed under it.
func LoadMasterKey(ctx context.Context, opts MasterKeyOptions) ([]byte, string, error) {
	if opts.Runner == nil {
		opts.Runner = ExecRunner{}
	}
	if opts.Available == nil {
		opts.Available = func(name string) bool { _, err := exec.LookPath(name); return err == nil }
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.Source == "" {
		opts.Source = "auto"
	}
	if err := os.MkdirAll(opts.DataDir, 0o700); err != nil {
		return nil, "", err
	}
	statePath := filepath.Join(opts.DataDir, "secrets.source")
	if data, err := os.ReadFile(statePath); err == nil {
		var state keyState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, "", fmt.Errorf("%s is damaged: %w", statePath, err)
		}
		if opts.Source != "auto" && opts.Source != state.Source {
			return nil, "", fmt.Errorf("the secret store's key was made with %s; secrets.master_key says %s (remove %s and the stored secrets to start again)", state.Source, opts.Source, statePath)
		}
		key, err := readKey(ctx, opts, state.Source)
		if err != nil {
			return nil, "", fmt.Errorf("reading the secret store's key from %s: %w", state.Source, err)
		}
		if plain, err := Open(key, "check", TypeValue, state.Nonce, state.Check); err != nil || string(plain) != checkText {
			if state.Source == SourcePassphrase {
				return nil, "", errors.New("LEGATO_SECRETS_PASSPHRASE is not the passphrase the secret store was made with")
			}
			return nil, "", fmt.Errorf("the key %s gave does not open the secret store", state.Source)
		}
		return key, state.Source, nil
	}

	candidates := []string{opts.Source}
	if opts.Source == "auto" {
		candidates = MasterKeySources
	}
	var failures []string
	for _, source := range candidates {
		if !applies(opts, source) {
			if opts.Source != "auto" {
				return nil, "", fmt.Errorf("the master key source %s is not available here", source)
			}
			continue
		}
		key, err := createKey(ctx, opts, source)
		if err != nil {
			if opts.Source != "auto" {
				return nil, "", fmt.Errorf("making the secret store's key with %s: %w", source, err)
			}
			failures = append(failures, source+": "+err.Error())
			continue
		}
		nonce, check, err := Seal(key, "check", TypeValue, []byte(checkText))
		if err != nil {
			return nil, "", err
		}
		data, _ := json.Marshal(keyState{Source: source, Nonce: nonce, Check: check})
		if err := os.WriteFile(statePath, data, 0o600); err != nil {
			return nil, "", err
		}
		return key, source, nil
	}
	return nil, "", fmt.Errorf("no master key source worked: %s", strings.Join(failures, "; "))
}

func applies(opts MasterKeyOptions, source string) bool {
	switch source {
	case SourceSystemdCreds:
		return opts.GOOS == "linux" && opts.Available("systemd-creds")
	case SourceSecretService:
		return opts.GOOS != "darwin" && opts.Available("secret-tool")
	case SourceKeychain:
		return opts.GOOS == "darwin" && opts.Available("security")
	case SourcePassphrase:
		return opts.Passphrase != ""
	case SourceFile:
		return true
	}
	return false
}

func newKey() ([]byte, error) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	return key, err
}

func createKey(ctx context.Context, opts MasterKeyOptions, source string) ([]byte, error) {
	if source == SourcePassphrase {
		return passphraseKey(opts, true)
	}
	key, err := newKey()
	if err != nil {
		return nil, err
	}
	encoded := []byte(hex.EncodeToString(key))
	switch source {
	case SourceSystemdCreds:
		_, err = opts.Runner.Run(ctx, "systemd-creds", []string{"encrypt", "--user", "--name=legato-secrets", "-", credFile(opts)}, nil, encoded)
	case SourceSecretService:
		_, err = opts.Runner.Run(ctx, "secret-tool", []string{"store", "--label=Legato secret store", "service", "legato", "key", "master"}, nil, encoded)
	case SourceKeychain:
		// security takes the password on its command line; it is there for
		// the moment the command runs.
		_, err = opts.Runner.Run(ctx, "security", []string{"add-generic-password", "-s", "legato", "-a", "master-key", "-U", "-w", string(encoded)}, nil, nil)
	case SourceFile:
		err = os.WriteFile(filepath.Join(opts.DataDir, "secrets.key"), append(encoded, '\n'), 0o600)
	default:
		err = fmt.Errorf("unknown master key source %q", source)
	}
	if err != nil {
		return nil, err
	}
	// Read it back, so a store that took the key but cannot give it out is
	// found now, not at the next start.
	back, err := readKey(ctx, opts, source)
	if err != nil || !bytes.Equal(back, key) {
		return nil, fmt.Errorf("the key could not be read back")
	}
	return key, nil
}

func readKey(ctx context.Context, opts MasterKeyOptions, source string) ([]byte, error) {
	var out []byte
	var err error
	switch source {
	case SourcePassphrase:
		return passphraseKey(opts, false)
	case SourceSystemdCreds:
		out, err = opts.Runner.Run(ctx, "systemd-creds", []string{"decrypt", "--user", "--name=legato-secrets", credFile(opts), "-"}, nil, nil)
	case SourceSecretService:
		out, err = opts.Runner.Run(ctx, "secret-tool", []string{"lookup", "service", "legato", "key", "master"}, nil, nil)
	case SourceKeychain:
		out, err = opts.Runner.Run(ctx, "security", []string{"find-generic-password", "-s", "legato", "-a", "master-key", "-w"}, nil, nil)
	case SourceFile:
		out, err = os.ReadFile(filepath.Join(opts.DataDir, "secrets.key"))
	default:
		return nil, fmt.Errorf("unknown master key source %q", source)
	}
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil || len(key) != 32 {
		return nil, errors.New("the stored key is not 32 bytes of hex")
	}
	return key, nil
}

func credFile(opts MasterKeyOptions) string {
	return filepath.Join(opts.DataDir, "secrets.key.cred")
}

func passphraseKey(opts MasterKeyOptions, create bool) ([]byte, error) {
	if opts.Passphrase == "" {
		return nil, errors.New("LEGATO_SECRETS_PASSPHRASE is not set")
	}
	saltPath := filepath.Join(opts.DataDir, "secrets.salt")
	salt, err := os.ReadFile(saltPath)
	if errors.Is(err, os.ErrNotExist) && create {
		salt = make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		if err := os.WriteFile(saltPath, salt, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return pbkdf2.Key(sha256.New, opts.Passphrase, salt, passphraseRounds, 32)
}
