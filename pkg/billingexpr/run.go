package billingexpr

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/tidwall/gjson"
)

// RunExpr compiles (with cache) and executes an expression string.
// The environment exposes:
//   - p, c             - prompt / completion tokens (auto-excluding separately-priced sub-categories)
//   - len              - total input context length for tier conditions (never reduced by sub-category exclusion)
//   - cr, cc, cc1h     - cache read / creation / creation-1h tokens
//   - tier(name, value) - trace callback that records which tier matched
//   - max, min, abs, ceil, floor - standard math helpers
//
// Returns the resulting float64 quota (before group ratio) and a TraceResult
// with side-channel info captured by tier() during execution.
func RunExpr(exprStr string, params TokenParams) (float64, TraceResult, error) {
	return RunExprWithRequest(exprStr, params, RequestInput{})
}

func RunExprWithRequest(exprStr string, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	prog, err := CompileFromCache(exprStr)
	if err != nil {
		return 0, TraceResult{}, err
	}
	return runProgram(prog, params, request)
}

// RunExprByHash is like RunExpr but accepts a pre-computed hash for the cache
// lookup, avoiding a redundant SHA-256 computation when the caller already
// holds BillingSnapshot.ExprHash.
func RunExprByHash(exprStr, hash string, params TokenParams) (float64, TraceResult, error) {
	return RunExprByHashWithRequest(exprStr, hash, params, RequestInput{})
}

func RunExprByHashWithRequest(exprStr, hash string, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	prog, err := CompileFromCacheByHash(exprStr, hash)
	if err != nil {
		return 0, TraceResult{}, err
	}
	return runProgram(prog, params, request)
}

func runProgram(prog *vm.Program, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	trace := TraceResult{}
	headers, err := NormalizeHeaders(request.Headers)
	if err != nil {
		return 0, trace, fmt.Errorf("normalize request headers: %w", err)
	}

	env := map[string]interface{}{
		"p":     params.P,
		"c":     params.C,
		"len":   params.Len,
		"cr":    params.CR,
		"cc":    params.CC,
		"cc1h":  params.CC1h,
		"img":   params.Img,
		"img_o": params.ImgO,
		"ai":    params.AI,
		"ao":    params.AO,
		"tier": func(name string, value float64) float64 {
			trace.MatchedTier = name
			trace.Cost = value
			return value
		},
		"header": func(key string) string {
			return headers[strings.ToLower(strings.TrimSpace(key))]
		},
		"param": func(path string) interface{} {
			path = strings.TrimSpace(path)
			if path == "" || len(request.Body) == 0 {
				return nil
			}
			result := gjson.GetBytes(request.Body, path)
			if !result.Exists() {
				return nil
			}
			return result.Value()
		},
		"has": func(source interface{}, substr string) bool {
			if source == nil || substr == "" {
				return false
			}
			return strings.Contains(fmt.Sprint(source), substr)
		},
		"hour":    func(tz string) int { return timeInZone(tz).Hour() },
		"minute":  func(tz string) int { return timeInZone(tz).Minute() },
		"weekday": func(tz string) int { return int(timeInZone(tz).Weekday()) },
		"month":   func(tz string) int { return int(timeInZone(tz).Month()) },
		"day":     func(tz string) int { return timeInZone(tz).Day() },
		"year":    func(tz string) int { return timeInZone(tz).Year() },
		"max":     math.Max,
		"min":     math.Min,
		"abs":     math.Abs,
		"ceil":    math.Ceil,
		"floor":   math.Floor,
	}

	out, err := expr.Run(prog, env)
	if err != nil {
		return 0, trace, fmt.Errorf("expr run error: %w", err)
	}
	f, ok := out.(float64)
	if !ok {
		return 0, trace, fmt.Errorf("expr result is %T, want float64", out)
	}
	return f, trace, nil
}

func timeInZone(tz string) time.Time {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return time.Now().UTC()
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Now().UTC()
	}
	return time.Now().In(loc)
}

// NormalizeHeaders canonicalizes request header names for billing expressions.
// Header maps can be assembled from more than one source, so accepting two
// non-identical keys that differ only by case would make header() depend on Go's
// randomized map iteration order. Reject the ambiguity instead.
func NormalizeHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return map[string]string{}, nil
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	normalized := make(map[string]string, len(headers))
	seen := make(map[string]string, len(headers))
	for _, key := range keys {
		k := strings.ToLower(strings.TrimSpace(key))
		if k == "" {
			continue
		}
		if IsSensitiveHeaderName(k) {
			// Billing expressions may influence quota, but they must not be able
			// to inspect credentials or session material supplied by the client.
			// Keep this filtering at the expression boundary so the original
			// headers remain available to the upstream adapter.
			continue
		}
		if previousKey, exists := seen[k]; exists {
			return nil, fmt.Errorf("request headers contain duplicate names %q and %q", previousKey, key)
		}
		seen[k] = key
		v := strings.TrimSpace(headers[key])
		if v == "" {
			continue
		}
		normalized[k] = v
	}
	return normalized, nil
}

// IsSensitiveHeaderName reports whether a request header must be hidden from
// billing expressions. Header names are normalized case-insensitively before
// this check. Provider feature flags such as anthropic-beta remain available;
// only credentials, cookies, and authentication/signature material are
// excluded.
func IsSensitiveHeaderName(headerName string) bool {
	headerName = strings.ToLower(strings.TrimSpace(headerName))
	if headerName == "" {
		return false
	}
	for _, marker := range []string{
		"authorization",
		"auth-key",
		"auth_key",
		"authkey",
		"cookie",
		"api-key",
		"api_key",
		"apikey",
		"access-token",
		"access_token",
		"refresh-token",
		"refresh_token",
		"id-token",
		"id_token",
		"session-token",
		"session_token",
		"secret",
		"credential",
		"signature",
		"private-key",
		"private_key",
	} {
		if strings.Contains(headerName, marker) {
			return true
		}
	}
	return headerName == "token" || strings.HasSuffix(headerName, "-token") || strings.HasSuffix(headerName, "_token")
}
