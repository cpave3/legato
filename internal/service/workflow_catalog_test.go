package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// Every executor this package defines has a catalog entry, and every
// catalog entry has an executor. The executors are found in the source (a
// Type method returning a step type), so a new one fails here until the
// catalog declares its contract.
func TestEveryWorkflowExecutorHasACatalogEntry(t *testing.T) {
	executors := executorTypesInSource(t)
	var catalog []string
	for _, spec := range workflowengine.Catalog() {
		catalog = append(catalog, spec.Type)
	}
	for _, executor := range executors {
		if !slices.Contains(catalog, executor) {
			t.Errorf("executor %q has no entry in the workflow catalog (internal/engine/workflow/catalog.go)", executor)
		}
	}
	for _, entry := range catalog {
		if !slices.Contains(executors, entry) {
			t.Errorf("catalog entry %q has no executor in internal/service", entry)
		}
	}
}

// The source check finds the executors wired at runtime, by building them.
func TestExecutorTypesInSourceFindsTheExecutors(t *testing.T) {
	found := executorTypesInSource(t)
	for _, executor := range []WorkflowExecutor{
		NewAgentRunExecutor(nil), NewAgentDispatchExecutor(nil), NewAgentMessageExecutor(nil, nil), NewAgentStopExecutor(nil),
		NewTaskNoteExecutor(nil), NewReviewPassesStep(), NewFileRemoveExecutor(),
	} {
		if !slices.Contains(found, executor.Type()) {
			t.Errorf("the source check misses %q", executor.Type())
		}
	}
}

// executorTypesInSource lists the step types returned by the Type methods
// of this package's non-test files: a string, or a string constant.
func executorTypesInSource(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]string{}
	var returned []ast.Expr
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl.Tok != token.CONST {
					continue
				}
				for _, spec := range decl.Specs {
					value := spec.(*ast.ValueSpec)
					for i, ident := range value.Names {
						if i < len(value.Values) {
							if lit, ok := value.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								constants[ident.Name], _ = strconv.Unquote(lit.Value)
							}
						}
					}
				}
			case *ast.FuncDecl:
				if decl.Recv == nil || decl.Name.Name != "Type" || decl.Body == nil || len(decl.Body.List) != 1 {
					continue
				}
				if ret, ok := decl.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
					returned = append(returned, ret.Results[0])
				}
			}
		}
	}
	var types []string
	for _, expr := range returned {
		var value string
		switch expr := expr.(type) {
		case *ast.BasicLit:
			value, _ = strconv.Unquote(expr.Value)
		case *ast.Ident:
			value = constants[expr.Name]
		}
		// A step type has a dot, or is one of the catalog's without (gate);
		// other Type methods (such as a profile's) return something else.
		_, listed := workflowengine.LookupExecutor(value)
		if (strings.Contains(value, ".") || listed) && !slices.Contains(types, value) {
			types = append(types, value)
		}
	}
	slices.Sort(types)
	return types
}
