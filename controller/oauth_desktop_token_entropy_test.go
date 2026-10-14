package controller

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The handoff token and state are bearer secrets: whoever presents the handoff
// token to /api/oauth/desktop/poll receives the session. They must come from a
// cryptographic source. A seeded pseudo-random generator is reproducible from
// its seed, so a token drawn from one is predictable.
//
// This pins the source rather than the output: it fails if the desktop token
// path draws from math/rand (directly or through a helper such as
// common.GetRandomString -> lo.RandomString).
func TestDesktopOAuthTokensUseCryptoRandomSource(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	// common.GetRandomString is the helper the desktop tokens were drawn from.
	// It must not resolve to a math/rand-based generator.
	src, err := os.ReadFile("../common/str.go")
	if err != nil {
		t.Fatalf("read common/str.go: %v", err)
	}
	body := string(src)
	start := strings.Index(body, "func GetRandomString(")
	if start < 0 {
		t.Fatal("GetRandomString not found in common/str.go")
	}
	end := strings.Index(body[start:], "\n}")
	if end < 0 {
		t.Fatal("GetRandomString body not terminated")
	}
	impl := body[start : start+end]
	if strings.Contains(impl, "lo.RandomString") {
		t.Fatalf("GetRandomString still draws from lo.RandomString (math/rand):\n%s", impl)
	}
	// The generator may live in a helper; require that helper to be crypto-based
	// and free of the math/rand-backed lo generator.
	if strings.Contains(impl, "randomStringFromCrypto") {
		utils, err := os.ReadFile("../common/utils.go")
		if err != nil {
			t.Fatalf("read common/utils.go: %v", err)
		}
		hStart := strings.Index(string(utils), "func randomStringFromCrypto(")
		if hStart < 0 {
			t.Fatal("randomStringFromCrypto not found in common/utils.go")
		}
		hEnd := strings.Index(string(utils)[hStart:], "\n}")
		helper := string(utils)[hStart : hStart+hEnd]
		if !strings.Contains(helper, "crand") && !strings.Contains(helper, "crypto/rand") {
			t.Fatalf("randomStringFromCrypto does not use a crypto source:\n%s", helper)
		}
		return
	}
	if !strings.Contains(impl, "crand") && !strings.Contains(impl, "crypto/rand") {
		t.Fatalf("GetRandomString does not use a crypto source:\n%s", impl)
	}
}
