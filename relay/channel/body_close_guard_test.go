package channel

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 每个从 resp.Body 读到内存的响应处理函数都必须在入口 defer 关闭响应体。
// 只在成功路径显式关闭时，读失败/超限的分支会直接返回：没有任何调用方会再
// 替它关闭，连接与文件描述符泄漏。审计把这类读错误分支列为同型缺陷；这里
// 在整棵 relay/channel 树上断言"读 resp.Body ⇒ defer 关闭"的不变量，防止
// 新增渠道或改动分支时重新引入。
func TestResponseBodyReadersDeferClose(t *testing.T) {
	var violations []string
	fset := token.NewFileSet()

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			respNames := respParamNames(fn)
			if len(respNames) == 0 {
				return true
			}
			if !readsRespBody(fn.Body, respNames) {
				return true
			}
			for _, name := range respNames {
				if defersClose(fn.Body, name) {
					return true
				}
			}
			violations = append(violations, fset.Position(fn.Pos()).String()+" "+fn.Name.Name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk relay/channel: %v", err)
	}

	if len(violations) > 0 {
		t.Fatalf("functions read resp.Body without deferring CloseResponseBodyGracefully:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// respParamNames 返回类型为 *http.Response 的参数名。
func respParamNames(fn *ast.FuncDecl) []string {
	var names []string
	if fn.Type.Params == nil {
		return names
	}
	for _, field := range fn.Type.Params.List {
		star, ok := field.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Response" {
			continue
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "http" {
			continue
		}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

// readsRespBody 判断函数体是否把响应体整体读入内存（io.ReadAll /
// ReadAllWithLimit）。流式扫描器自行管理响应体生命周期，不在本不变量范围内。
func readsRespBody(body *ast.BlockStmt, respNames []string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "ReadAll", "ReadAllWithLimit":
		default:
			return true
		}
		if exprIsRespBody(call.Args[0], respNames) {
			found = true
			return false
		}
		return true
	})
	return found
}

// exprIsRespBody 判断表达式是否为 <resp>.Body。
func exprIsRespBody(expr ast.Expr, respNames []string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Body" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	for _, name := range respNames {
		if ident.Name == name {
			return true
		}
	}
	return false
}

// defersClose 判断函数体是否有 defer ...CloseResponseBodyGracefully(<resp>)。
func defersClose(body *ast.BlockStmt, respName string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		deferStmt, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		call := deferStmt.Call
		if call == nil || len(call.Args) != 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "CloseResponseBodyGracefully" {
			return true
		}
		ident, ok := call.Args[0].(*ast.Ident)
		if ok && ident.Name == respName {
			found = true
			return false
		}
		return true
	})
	return found
}
