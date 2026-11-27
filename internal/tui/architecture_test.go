package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const enginePrefix = "github.com/cpave3/legato/internal/engine/"

// engineValues are the engine symbols the TUI may name: value types,
// constants and pure functions of values (analytics.Today and its kin
// read the clock, nothing else), audited one by one (Cameron's
// decision, task i95qqzwb). Engine behaviour (the event bus, IPC, the
// store's queries, clients) reaches the TUI through service interfaces.
//
// Limits: the check sees package-qualified names (events.Bus) in non-test
// files. It does not see a method called on a value of an allowed type, so
// a type is allowed only when none of its methods does I/O or holds state
// shared with other goroutines; review that when adding one.
var engineValues = map[string]map[string]bool{
	"analytics": set("TimeRange", "PeriodType", "PeriodDay", "PeriodWeek", "PeriodMonth", "Today", "ThisWeek", "ThisMonth", "Previous", "Next"),
	"git":       set("FileDiff", "FileBinary", "FileRenamed", "Line", "LineAdded", "LineDeleted"),
	"github":    set("PRStatus"),
	"store":     set("WorkspaceView", "ViewAll", "ViewUnassigned", "ViewWorkspace", "PRMeta", "TaskWorktree", "PlanComment", "ReviewHunkNote", "ReviewStep"),
	"workflow":  set("RepositoryPreset"),
	// events: the Event value, its EventType constants (Event…) and payload
	// types (…Payload), checked by eventsValue; never Bus or New.
	"events": set("Event", "EventType"),
}

func set(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

func eventsValue(name string) bool {
	return engineValues["events"][name] || strings.HasSuffix(name, "Payload") ||
		(strings.HasPrefix(name, "Event") && name != "EventBus")
}

func TestTUIUsesOnlyEngineValueTypesAndPureHelpers(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		local := map[string]string{} // name in this file → engine package
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(value, enginePrefix) {
				continue
			}
			pkg := strings.TrimPrefix(value, enginePrefix)
			if _, ok := engineValues[pkg]; !ok {
				t.Errorf("%s imports %s, which is not an audited engine value package", path, value)
				continue
			}
			name := filepath.Base(pkg)
			if imported.Name != nil {
				name = imported.Name.Name
			}
			local[name] = pkg
		}
		ast.Inspect(file, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			pkg, ok := local[id.Name]
			if !ok {
				return true
			}
			allowed := engineValues[pkg][sel.Sel.Name]
			if pkg == "events" {
				allowed = eventsValue(sel.Sel.Name)
			}
			if !allowed {
				t.Errorf("%s uses %s.%s; the TUI may use engine value types and pure helpers only (reach behaviour through a service interface)", path, pkg, sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
