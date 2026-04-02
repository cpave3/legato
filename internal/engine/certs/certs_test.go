package certs

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestEnsureCerts_GeneratesValidCerts(t *testing.T) {
	dir := t.TempDir()

	paths, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	for _, f := range []string{paths.CACert, paths.CAKey, paths.ServerCert, paths.ServerKey} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing file: %s", f)
		}
	}

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

	os.WriteFile(paths.ServerCert, []byte("invalid"), 0600)

	paths2, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}

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

func TestEnsureCerts_IncludesExtraDNS(t *testing.T) {
	dir := t.TempDir()

	paths, err := EnsureCerts(dir, "mybox.local")
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	cert := parseCert(t, paths.ServerCert)
	if !slices.Contains(cert.DNSNames, "mybox.local") {
		t.Errorf("cert DNSNames = %v, missing mybox.local", cert.DNSNames)
	}
	if !slices.Contains(cert.DNSNames, "localhost") {
		t.Errorf("cert DNSNames = %v, missing localhost", cert.DNSNames)
	}
}

func TestEnsureCerts_RegeneratesOnNewHostname(t *testing.T) {
	dir := t.TempDir()

	// Generate without extra DNS.
	paths1, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	info1, _ := os.Stat(paths1.ServerCert)

	// Request with extra DNS — should regenerate server cert.
	paths2, err := EnsureCerts(dir, "mybox.local")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	info2, _ := os.Stat(paths2.ServerCert)

	if info1.ModTime().Equal(info2.ModTime()) {
		t.Error("expected cert regeneration for new hostname, but cert was reused")
	}

	cert := parseCert(t, paths2.ServerCert)
	if !slices.Contains(cert.DNSNames, "mybox.local") {
		t.Errorf("regenerated cert missing mybox.local: %v", cert.DNSNames)
	}
}

func TestEnsureCerts_ReusesCAOnRegeneration(t *testing.T) {
	dir := t.TempDir()

	paths1, err := EnsureCerts(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	caInfo1, _ := os.Stat(paths1.CACert)

	// Force server cert regen by adding hostname.
	_, err = EnsureCerts(dir, "newhost.local")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	caInfo2, _ := os.Stat(paths1.CACert)

	if !caInfo1.ModTime().Equal(caInfo2.ModTime()) {
		t.Error("CA was regenerated — should be reused when only server cert needs updating")
	}
}

func parseCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cert: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parsing cert: %v", err)
	}
	return cert
}
