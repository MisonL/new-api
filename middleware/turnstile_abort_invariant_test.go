package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TurnstileCheck must abort after every rejection it writes. Writing a response
// and returning without Abort leaves the chain running, so the protected
// handler (login, register) still executes -- the human check would be
// skippable by anyone who can make the session save fail.
//
// This asserts the invariant on the real function rather than on a copy of its
// logic, so it fails if a future branch forgets the Abort.
func TestTurnstileCheckAbortsAfterEveryRejection(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "turnstile-check.go", nil, 0)
	if err != nil {
		t.Fatalf("parse turnstile-check.go: %v", err)
	}

	var fn *ast.FuncDecl
	ast.Inspect(file, func(n ast.Node) bool {
		if d, ok := n.(*ast.FuncDecl); ok && d.Name.Name == "TurnstileCheck" {
			fn = d
		}
		return true
	})
	if fn == nil {
		t.Fatal("TurnstileCheck not found")
	}

	var jsonCalls int
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "JSON" {
			return true
		}
		jsonCalls++

		// Find the enclosing block and require a following c.Abort() before
		// the next return in that same statement list.
		aborted := false
		ast.Inspect(fn, func(inner ast.Node) bool {
			block, ok := inner.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, stmt := range block.List {
				expr, ok := stmt.(*ast.ExprStmt)
				if !ok {
					continue
				}
				innerCall, ok := expr.X.(*ast.CallExpr)
				if !ok || innerCall != call {
					continue
				}
				for _, next := range block.List[i+1:] {
					if isAbortCall(next) {
						aborted = true
						return false
					}
					if _, isReturn := next.(*ast.ReturnStmt); isReturn {
						return false
					}
				}
			}
			return true
		})

		if !aborted {
			pos := fset.Position(call.Pos())
			t.Errorf("%s: c.JSON rejection is not followed by c.Abort() before returning",
				pos)
		}
		return true
	})

	if jsonCalls == 0 {
		t.Fatal("no c.JSON call found in TurnstileCheck; the test is not exercising it")
	}
}

func isAbortCall(stmt ast.Stmt) bool {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return strings.EqualFold(sel.Sel.Name, "Abort")
}
