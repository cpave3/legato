package certs

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCerts_GeneratesValidCerts(t *testing.T) {
	dir := t.TempDir()

	paths, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	// Verify all files exist.
	for _, f := range []string{paths.CACert, paths.CAKey, paths.ServerCert, paths.ServerKey} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing file: %s", f)
		}
	}

	// Verify the cert/key pair is valid.
	_, err = tls.LoadX509KeyPair(paths.ServerCert, paths.ServerKey)
	if err != nil {
		t.Fatalf("invalid cert/key pair: %v", err)
	}
}

func TestEnsureCerts_ReusesExisting(t *testing.T) {
	dir := t.TempDir()

	paths1, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Record modification time of server cert.
	info1, _ := os.Stat(paths1.ServerCert)

	paths2, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	info2, _ := os.Stat(paths2.ServerCert)

	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("expected certs to be reused, but server cert was regenerated")
	}
}

func TestEnsureCerts_RegeneratesExpired(t *testing.T) {
	dir := t.TempDir()

	paths, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	// Corrupt the cert file to simulate invalid/expired.
	os.WriteFile(paths.ServerCert, []byte("invalid"), 0600)

	paths2, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}

	// Should have regenerated a valid cert.
	_, err = tls.LoadX509KeyPair(paths2.ServerCert, paths2.ServerKey)
	if err != nil {
		t.Fatalf("regenerated cert invalid: %v", err)
	}
}

func TestEnsureCerts_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "path")

	paths, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	if _, err := os.Stat(paths.CACert); err != nil {
		t.Errorf("CA cert not created: %v", err)
	}
}
