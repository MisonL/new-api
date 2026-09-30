package i18n

import (
	"testing"

	"github.com/nicksnyder/go-i18n/v2/i18n"
)

// Translate must not panic when Init has not run or failed. Init is allowed to
// fail without stopping the process (main.go logs the error and carries on),
// and every middleware and controller that calls T would otherwise panic while
// building its first error response.
//
// The test drives the uninitialized state itself instead of relying on Init not
// having been called yet: initOnce makes Init a no-op after the first call, so
// depending on package-level ordering would break under -shuffle.
func TestTranslateFallsBackToKeyWhenBundleIsMissing(t *testing.T) {
	mu.Lock()
	savedBundle := bundle
	savedLocalizers := localizers
	bundle = nil
	localizers = make(map[string]*i18n.Localizer)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		bundle = savedBundle
		localizers = savedLocalizers
		mu.Unlock()
	})

	const key = "i18n.test.missing.key"
	if got := Translate(LangEn, key); got != key {
		t.Fatalf("Translate without a bundle returned %q, want the key %q", got, key)
	}
	if got := T(nil, key); got != key {
		t.Fatalf("T without a bundle returned %q, want the key %q", got, key)
	}
	if loc := GetLocalizer(LangEn); loc != nil {
		t.Fatalf("GetLocalizer without a bundle returned %v, want nil", loc)
	}
}
