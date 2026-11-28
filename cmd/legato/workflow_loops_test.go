package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// A runtime without workflows (buildWorkflowService failed) still calls
// startWorkflowLoops, with nothing to start.
func TestStartWorkflowLoopsWithoutWorkflowsStartsNothing(t *testing.T) {
	stop := startWorkflowLoops(nil, nil, nil)
	if stop == nil {
		t.Fatal("startWorkflowLoops returned no stop func")
	}
	stop()
}

// LEG-668: the workflow loops can launch agents the moment they start (a
// missed schedule is caught up synchronously). An agent launched before
// startApprovals gives sessions the broker and credential channels runs
// without them, so in both runtimes the loops start only after that wiring.
func TestWorkflowLoopsStartAfterTheBrokerInBothRuntimes(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	mustFollow := []string{"startApprovals", "giveRepositories", "grantWorkflowBranches", "trackAgentBranches"}
	for _, name := range []string{"runServeCmd", "runTUI"} {
		fn := findFunc(file, name)
		if fn == nil {
			t.Fatalf("%s not found in main.go", name)
		}
		calls := callPositions(fn)
		loops, ok := calls["startWorkflowLoops"]
		if !ok {
			t.Errorf("%s never calls startWorkflowLoops", name)
			continue
		}
		for _, before := range mustFollow {
			pos, ok := calls[before]
			if !ok {
				t.Errorf("%s never calls %s", name, before)
				continue
			}
			if loops < pos {
				t.Errorf("%s starts the workflow loops (%s) before %s (%s)", name, fset.Position(loops), before, fset.Position(pos))
			}
		}
	}

	// Nothing in main.go starts a workflow loop directly, outside the helper.
	for _, loop := range []string{"StartAgentEndedLoop", "StartAdvanceLoop", "StartPRWaitLoop"} {
		if pos, ok := callPositions(file)[loop]; ok {
			t.Errorf("main.go calls %s directly at %s; start it through startWorkflowLoops", loop, fset.Position(pos))
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "StartEventLoop" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "triggerCoordinator" {
			t.Errorf("main.go starts the trigger coordinator directly at %s; start it through startWorkflowLoops", fset.Position(call.Pos()))
		}
		return true
	})
}

func findFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

// callPositions maps each called function or method name in node to the
// position of its last call.
func callPositions(node ast.Node) map[string]token.Pos {
	calls := map[string]token.Pos{}
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			calls[fun.Name] = call.Pos()
		case *ast.SelectorExpr:
			calls[fun.Sel.Name] = call.Pos()
		}
		return true
	})
	return calls
}
