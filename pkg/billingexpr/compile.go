package billingexpr

import (
	"fmt"
	"math"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
)

const maxCacheSize = 256

type cachedEntry struct {
	prog     *vm.Program
	usedVars map[string]bool
}

var (
	cacheMu sync.RWMutex
	cache   = make(map[string]*cachedEntry, 64)
)

// compileEnvPrototype is the type-checking prototype used at compile time.
// It declares the shape of the environment that RunExpr will provide.
// The tier() function is a no-op placeholder here; the real one with
// side-channel tracing is injected at runtime.
var compileEnvPrototype = map[string]interface{}{
	"p":                      float64(0),
	"c":                      float64(0),
	"cr":                     float64(0),
	"cc":                     float64(0),
	"cc1h":                   float64(0),
	"prompt_tokens":          float64(0),
	"completion_tokens":      float64(0),
	"cache_read_tokens":      float64(0),
	"cache_create_tokens":    float64(0),
	"cache_create_1h_tokens": float64(0),
	"img":                    float64(0),
	"ai":                     float64(0),
	"ao":                     float64(0),
	"image_tokens":           float64(0),
	"audio_input_tokens":     float64(0),
	"audio_output_tokens":    float64(0),
	"tier":                   func(string, float64) float64 { return 0 },
	"header":                 func(string) string { return "" },
	"param":                  func(string) interface{} { return nil },
	"has":                    func(interface{}, string) bool { return false },
	"hour":                   func(string) int { return 0 },
	"minute":                 func(string) int { return 0 },
	"weekday":                func(string) int { return 0 },
	"month":                  func(string) int { return 0 },
	"day":                    func(string) int { return 0 },
	"max":                    math.Max,
	"min":                    math.Min,
	"abs":                    math.Abs,
	"ceil":                   math.Ceil,
	"floor":                  math.Floor,
}

// CompileFromCache compiles an expression string, using a cached program when
// available. The cache is keyed by the SHA-256 hex digest of the expression.
func CompileFromCache(exprStr string) (*vm.Program, error) {
	return compileFromCacheByHash(exprStr, ExprHashString(exprStr))
}

// CompileFromCacheByHash is like CompileFromCache but accepts a pre-computed
// hash, useful when the caller already has the BillingSnapshot.ExprHash.
func CompileFromCacheByHash(exprStr, hash string) (*vm.Program, error) {
	return compileFromCacheByHash(exprStr, hash)
}

func compileFromCacheByHash(exprStr, hash string) (*vm.Program, error) {
	cacheMu.RLock()
	if entry, ok := cache[hash]; ok {
		cacheMu.RUnlock()
		return entry.prog, nil
	}
	cacheMu.RUnlock()

	prog, err := expr.Compile(exprStr, expr.Env(compileEnvPrototype), expr.AsFloat64())
	if err != nil {
		return nil, fmt.Errorf("expr compile error: %w", err)
	}

	vars := extractUsedVars(prog)

	cacheMu.Lock()
	if len(cache) >= maxCacheSize {
		cache = make(map[string]*cachedEntry, 64)
	}
	cache[hash] = &cachedEntry{prog: prog, usedVars: vars}
	cacheMu.Unlock()

	return prog, nil
}

func extractUsedVars(prog *vm.Program) map[string]bool {
	vars := make(map[string]bool)
	node := prog.Node()
	ast.Find(node, func(n ast.Node) bool {
		if id, ok := n.(*ast.IdentifierNode); ok {
			vars[id.Value] = true
		}
		return false
	})
	return vars
}

// UsedVars returns the set of identifier names referenced by an expression.
// The result is cached alongside the compiled program. Returns nil for empty input.
func UsedVars(exprStr string) map[string]bool {
	if exprStr == "" {
		return nil
	}
	hash := ExprHashString(exprStr)
	cacheMu.RLock()
	if entry, ok := cache[hash]; ok {
		cacheMu.RUnlock()
		return entry.usedVars
	}
	cacheMu.RUnlock()

	// Compile (and cache) to populate usedVars
	if _, err := compileFromCacheByHash(exprStr, hash); err != nil {
		return nil
	}
	cacheMu.RLock()
	entry, ok := cache[hash]
	cacheMu.RUnlock()
	if ok {
		return entry.usedVars
	}
	return nil
}

// InvalidateCache clears the compiled-expression cache.
// Called when billing rules are updated.
func InvalidateCache() {
	cacheMu.Lock()
	cache = make(map[string]*cachedEntry, 64)
	cacheMu.Unlock()
}
