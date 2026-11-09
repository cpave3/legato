package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

func TestServeLogsToTheFileAndStderr(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	var stderr bytes.Buffer
	stop := startLogFile(cfg, &stderr)
	log.Printf("serve says hello")
	stop()
	got, _ := os.ReadFile(filepath.Join(cfg.DataDir, "logs", "legato.log"))
	if !strings.Contains(string(got), "serve says hello") {
		t.Fatalf("log file = %q", got)
	}
	if !strings.Contains(stderr.String(), "serve says hello") {
		t.Fatalf("stderr = %q, want the line there too", stderr.String())
	}
}

func TestTUILogsOnlyToTheFile(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	stop := startLogFile(cfg, nil)
	log.Printf("tui says hello")
	stop()
	got, _ := os.ReadFile(filepath.Join(cfg.DataDir, "logs", "legato.log"))
	if !strings.Contains(string(got), "tui says hello") {
		t.Fatalf("log file = %q", got)
	}
}
