package claudeusage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNamedCredentialsNeverFallBackToDefaultIdentity(t *testing.T) {
	defaultFile := writeCredentials(t, sentinel, time.Now().Add(time.Hour))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Dir(defaultFile))
	for _, darwin := range []bool{false, true} {
		source := namedCredentials{file: FileCredentials{Path: filepath.Join(t.TempDir(), ".credentials.json")}, darwin: darwin}
		_, err := source.Login()
		want := ErrNotSignedIn
		if darwin {
			want = ErrNamedKeychainUnavailable
		}
		if !errors.Is(err, want) {
			t.Fatalf("darwin=%t: %v", darwin, err)
		}
		source.file.Path = writeCredentials(t, "fixture-named", time.Now().Add(time.Hour))
		login, err := source.Login()
		if err != nil || login.AccessToken != "fixture-named" {
			t.Fatal("named file-backed identity was not used")
		}
	}
}

func TestNamedCredentialsRefuseAFileLinkedOutsideTheAccount(t *testing.T) {
	dir := t.TempDir()
	outside := writeCredentials(t, sentinel, time.Now().Add(time.Hour))
	if err := os.Symlink(outside, filepath.Join(dir, ".credentials.json")); err != nil {
		t.Fatal(err)
	}
	_, err := NamedCredentials(dir).Login()
	if err == nil {
		t.Fatal("named reader followed another account's credential symlink")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatal("error disclosed credential")
	}
}
