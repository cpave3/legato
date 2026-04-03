package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureTokenCreatesOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	token, err := EnsureToken(dir)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64 hex chars", len(token))
	}

	// File should exist.
	data, err := os.ReadFile(filepath.Join(dir, tokenFile))
	if err != nil {
		t.Fatalf("reading token file: %v", err)
	}
	if got := string(data); got != token+"\n" {
		t.Errorf("file content = %q, want %q", got, token+"\n")
	}
}

func TestEnsureTokenReusesExisting(t *testing.T) {
	dir := t.TempDir()
	first, err := EnsureToken(dir)
	if err != nil {
		t.Fatalf("first EnsureToken: %v", err)
	}
	second, err := EnsureToken(dir)
	if err != nil {
		t.Fatalf("second EnsureToken: %v", err)
	}
	if first != second {
		t.Errorf("tokens differ: %q vs %q", first, second)
	}
}

func TestRegenerateTokenReturnsNewToken(t *testing.T) {
	dir := t.TempDir()
	original, err := EnsureToken(dir)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	regenerated, err := RegenerateToken(dir)
	if err != nil {
		t.Fatalf("RegenerateToken: %v", err)
	}
	if original == regenerated {
		t.Error("regenerated token should differ from original")
	}
	if len(regenerated) != 64 {
		t.Errorf("regenerated token length = %d, want 64", len(regenerated))
	}

	// EnsureToken should now return the regenerated token.
	current, err := EnsureToken(dir)
	if err != nil {
		t.Fatalf("EnsureToken after regenerate: %v", err)
	}
	if current != regenerated {
		t.Errorf("EnsureToken returned %q, want %q", current, regenerated)
	}
}

func TestReadTokenMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadToken(dir)
	if err == nil {
		t.Error("ReadToken should fail when file doesn't exist")
	}
}

func TestReadTokenAfterEnsure(t *testing.T) {
	dir := t.TempDir()
	ensured, err := EnsureToken(dir)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	read, err := ReadToken(dir)
	if err != nil {
		t.Fatalf("ReadToken: %v", err)
	}
	if read != ensured {
		t.Errorf("ReadToken = %q, want %q", read, ensured)
	}
}
