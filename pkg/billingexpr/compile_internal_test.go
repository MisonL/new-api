package billingexpr

import (
	"fmt"
	"testing"
)

func TestCompileFromCacheEvictsPartialBatchWhenFull(t *testing.T) {
	InvalidateCache()
	defer InvalidateCache()

	initialHashes := make(map[string]struct{}, maxCacheSize)
	for i := 0; i < maxCacheSize; i++ {
		exprStr := fmt.Sprintf("p + %d", i)
		if _, err := CompileFromCache(exprStr); err != nil {
			t.Fatalf("compile initial expression %d: %v", i, err)
		}
		initialHashes[ExprHashString(exprStr)] = struct{}{}
	}

	cacheMu.RLock()
	if got := len(cache); got != maxCacheSize {
		cacheMu.RUnlock()
		t.Fatalf("cache size after warmup = %d, want %d", got, maxCacheSize)
	}
	cacheMu.RUnlock()

	if _, err := CompileFromCache("p + 9999"); err != nil {
		t.Fatalf("compile overflow expression: %v", err)
	}

	cacheMu.RLock()
	gotSize := len(cache)
	survivors := 0
	for hash := range initialHashes {
		if _, ok := cache[hash]; ok {
			survivors++
		}
	}
	cacheMu.RUnlock()

	wantSize := maxCacheSize - cacheEvictBatchSize + 1
	if gotSize != wantSize {
		t.Fatalf("cache size after eviction = %d, want %d", gotSize, wantSize)
	}
	if survivors == 0 {
		t.Fatal("cache eviction removed all previously compiled expressions")
	}
}
