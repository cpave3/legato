package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/config"
)

func TestTLSDisabledByDefault(t *testing.T) {
	cfg := &config.Config{}
	if tlsEnabled(cfg) {
		t.Fatal("TLS should be opt-in")
	}
	cert, key, ca := resolveTLS(cfg)
	if cert != "" || key != "" || ca != "" {
		t.Fatalf("resolveTLS() = %q, %q, %q; want HTTP", cert, key, ca)
	}
}

func TestExplicitCertificateEnablesTLS(t *testing.T) {
	cfg := &config.Config{Web: config.WebConfig{TLS: config.TLSConfig{
		Cert: "server.pem",
		Key:  "server-key.pem",
	}}}
	if !tlsEnabled(cfg) {
		t.Fatal("an explicit certificate and key should enable TLS")
	}
	cert, key, ca := resolveTLS(cfg)
	if cert != "server.pem" || key != "server-key.pem" || ca != "" {
		t.Fatalf("resolveTLS() = %q, %q, %q", cert, key, ca)
	}
}

func TestEnabledTLSGeneratesCertificate(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{
		DB:  config.DBConfig{Path: filepath.Join(dataDir, "legato.db")},
		Web: config.WebConfig{TLS: config.TLSConfig{Enabled: true}},
	}
	cert, key, ca := resolveTLS(cfg)
	for _, path := range []string{cert, key, ca} {
		if path == "" {
			t.Fatal("enabled TLS returned an empty certificate path")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("generated TLS file %q: %v", path, err)
		}
	}
}
