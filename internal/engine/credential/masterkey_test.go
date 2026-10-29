package credential

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scriptedRunner answers commands by their name and first argument, and
// remembers what was stored through it.
type scriptedRunner struct {
	stored  map[string][]byte // what each tool keeps
	fail    map[string]bool   // tools that do not work here
	calls   []string
}

func newScriptedRunner() *scriptedRunner {
	return &scriptedRunner{stored: map[string][]byte{}, fail: map[string]bool{}}
}

func (r *scriptedRunner) Run(_ context.Context, name string, args, _ []string, stdin []byte) ([]byte, error) {
	r.calls = append(r.calls, name+" "+args[0])
	if r.fail[name] {
		return nil, errors.New(name + " is not available")
	}
	switch name + " " + args[0] {
	case "systemd-creds encrypt":
		r.stored[name] = append([]byte{}, stdin...)
		return nil, os.WriteFile(args[len(args)-1], []byte("sealed by systemd"), 0o600)
	case "systemd-creds decrypt":
		if r.stored[name] == nil {
			return nil, errors.New("no credential")
		}
		return r.stored[name], nil
	case "secret-tool store":
		r.stored[name] = append([]byte{}, stdin...)
		return nil, nil
	case "secret-tool lookup":
		if r.stored[name] == nil {
			return nil, errors.New("exit status 1")
		}
		return r.stored[name], nil
	case "security add-generic-password":
		r.stored[name] = []byte(args[len(args)-1])
		return nil, nil
	case "security find-generic-password":
		if r.stored[name] == nil {
			return nil, errors.New("not found")
		}
		return r.stored[name], nil
	}
	return nil, errors.New("unexpected command " + name)
}

func options(t *testing.T, runner *scriptedRunner, goos string, available ...string) MasterKeyOptions {
	t.Helper()
	have := map[string]bool{}
	for _, name := range available {
		have[name] = true
	}
	return MasterKeyOptions{DataDir: t.TempDir(), Source: "auto", Runner: runner, GOOS: goos, Available: func(name string) bool { return have[name] }}
}

func TestAutoUsesSystemdCredsFirstOnLinux(t *testing.T) {
	runner := newScriptedRunner()
	opts := options(t, runner, "linux", "systemd-creds", "secret-tool")

	key, source, err := LoadMasterKey(context.Background(), opts)
	if err != nil || source != "systemd-creds" || len(key) != 32 {
		t.Fatalf("key %d bytes, source %q, err %v", len(key), source, err)
	}
	if _, err := os.Stat(filepath.Join(opts.DataDir, "secrets.key.cred")); err != nil {
		t.Fatalf("no sealed key file: %v", err)
	}
	again, source, err := LoadMasterKey(context.Background(), opts)
	if err != nil || source != "systemd-creds" || hex.EncodeToString(again) != hex.EncodeToString(key) {
		t.Fatalf("second load: source %q err %v same=%v", source, err, hex.EncodeToString(again) == hex.EncodeToString(key))
	}
}

func TestAutoFallsBackToTheSecretServiceAndKeepsUsingIt(t *testing.T) {
	runner := newScriptedRunner()
	runner.fail["systemd-creds"] = true
	opts := options(t, runner, "linux", "systemd-creds", "secret-tool")

	key, source, err := LoadMasterKey(context.Background(), opts)
	if err != nil || source != "secret-service" {
		t.Fatalf("source %q err %v", source, err)
	}
	// systemd-creds works now, but the key was made with the Secret Service.
	runner.fail["systemd-creds"] = false
	again, source, err := LoadMasterKey(context.Background(), opts)
	if err != nil || source != "secret-service" || string(again) != string(key) {
		t.Fatalf("second load: source %q err %v", source, err)
	}
}

func TestTheKeychainIsUsedOnMacOS(t *testing.T) {
	runner := newScriptedRunner()
	_, source, err := LoadMasterKey(context.Background(), options(t, runner, "darwin", "security", "secret-tool"))
	if err != nil || source != "keychain" {
		t.Fatalf("source %q err %v", source, err)
	}
}

func TestAPassphraseIsUsedBeforeAKeyFile(t *testing.T) {
	opts := options(t, newScriptedRunner(), "linux")
	opts.Passphrase = "correct horse"

	key, source, err := LoadMasterKey(context.Background(), opts)
	if err != nil || source != "passphrase" {
		t.Fatalf("source %q err %v", source, err)
	}
	again, _, err := LoadMasterKey(context.Background(), opts)
	if err != nil || string(again) != string(key) {
		t.Fatalf("the same passphrase gave another key: %v", err)
	}
	opts.Passphrase = "wrong horse"
	if _, _, err := LoadMasterKey(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("a wrong passphrase was accepted: %v", err)
	}
}

func TestTheKeyFileIsTheLastResort(t *testing.T) {
	opts := options(t, newScriptedRunner(), "linux")

	key, source, err := LoadMasterKey(context.Background(), opts)
	if err != nil || source != "file" || len(key) != 32 {
		t.Fatalf("source %q err %v", source, err)
	}
	info, err := os.Stat(filepath.Join(opts.DataDir, "secrets.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file %v, %v; want mode 0600", info, err)
	}
}

func TestANamedSourceIsUsedAndNoOther(t *testing.T) {
	runner := newScriptedRunner()
	runner.fail["secret-tool"] = true
	opts := options(t, runner, "linux", "secret-tool")
	opts.Source = "secret-service"

	if _, _, err := LoadMasterKey(context.Background(), opts); err == nil {
		t.Fatal("a named source that does not work fell back to another")
	}
}
