package billingexpr

import (
	"fmt"
	"math"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

const maxCacheSize = 256

var (
	cacheMu sync.RWMutex
	cache   = make(map[string]*vm.Program, 64)
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
	if prog, ok := cache[hash]; ok {
		cacheMu.RUnlock()
		return prog, nil
	}
	cacheMu.RUnlock()

	prog, err := expr.Compile(exprStr, expr.Env(compileEnvPrototype), expr.AsFloat64())
	if err != nil {
		return nil, fmt.Errorf("expr compile error: %w", err)
	}

	cacheMu.Lock()
	if len(cache) >= maxCacheSize {
		cache = make(map[string]*vm.Program, 64)
	}
	cache[hash] = prog
	cacheMu.Unlock()

	return prog, nil
}

// InvalidateCache clears the compiled-expression cache.
// Called when billing rules are updated.
func InvalidateCache() {
	cacheMu.Lock()
	cache = make(map[string]*vm.Program, 64)
	cacheMu.Unlock()
}
