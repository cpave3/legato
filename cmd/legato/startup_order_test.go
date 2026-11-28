package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/version"
)

// LEG-689: on a restart, the Reeve's session was relaunched about two
// seconds before the new process rewrote the state plugin's hooks.json, so it
// loaded the old hooks. Each of serve and the TUI installs every agent
// tool's mod and hook files first, before the store opens and before
// anything that can start, resume or relaunch an agent session.
func TestServeAndTheTUIInstallAgentModsBeforeAnythingElse(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	later := []string{"openStore", "configuredAdapters", "NewAgentService", "startApprovals", "wireReeve", "newTicketSync", "startPRTrackingRuntime", "startMainHandoffs"}
	for _, fn := range []string{"runServeCmd", "runTUI"} {
		calls := callOrder(t, file, fn)
		install, ok := calls["installAgentMods"]
		if !ok {
			t.Fatalf("%s never calls installAgentMods", fn)
		}
		for _, name := range later {
			at, ok := calls[name]
			if !ok {
				t.Fatalf("%s no longer calls %s: update this test", fn, name)
			}
			if at < install {
				t.Errorf("%s calls %s before installAgentMods", fn, name)
			}
		}
	}
}

// callOrder is where each function a function body calls is first called.
func callOrder(t *testing.T, file *ast.File, fn string) map[string]token.Pos {
	t.Helper()
	for _, decl := range file.Decls {
		f, ok := decl.(*ast.FuncDecl)
		if !ok || f.Name.Name != fn {
			continue
		}
		calls := map[string]token.Pos{}
		ast.Inspect(f.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var name string
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			}
			if _, seen := calls[name]; name != "" && !seen {
				calls[name] = call.Pos()
			}
			return true
		})
		return calls
	}
	t.Fatalf("no function %s in main.go", fn)
	return nil
}

func TestInstallAgentModsWritesTheStatePluginsHooks(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	installAgentMods(cfg, "/opt/legato")
	hooks := filepath.Join(cfg.DataDir, "claude-mod", "legato-state", "hooks", "hooks.json")
	if _, err := os.Stat(hooks); err != nil {
		t.Fatalf("the state plugin's hooks were not installed: %v", err)
	}
}

// Several processes share the log file; each line says which one wrote it
// and with what binary, so a restart's order can be read from it.
func TestLogLinesNameTheProcessAndItsVersion(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	var stderr bytes.Buffer
	stop := startLogFile(cfg, &stderr)
	log.Printf("which process")
	stop()
	want := "[" + strconv.Itoa(os.Getpid()) + " " + version.String() + "] "
	if !strings.Contains(stderr.String(), want+"") || !strings.Contains(stderr.String(), "which process") {
		t.Fatalf("log line = %q, want it to carry %q", stderr.String(), want)
	}
}
