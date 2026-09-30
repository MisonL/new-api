package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
)

func TestBuildInfoLinesNormalizeEmptyValues(t *testing.T) {
	oldVersion := Version
	oldCommit := BuildCommit
	oldDate := BuildDate
	oldSource := BuildSource
	t.Cleanup(func() {
		Version = oldVersion
		BuildCommit = oldCommit
		BuildDate = oldDate
		BuildSource = oldSource
	})

	Version = " v-test "
	BuildCommit = ""
	BuildDate = " 2026-05-01T00:00:00Z "
	BuildSource = " "

	lines := BuildInfoLines()
	expected := []string{
		"version=v-test",
		"commit=unknown",
		"date=2026-05-01T00:00:00Z",
		"source=unknown",
	}
	for i := range expected {
		if lines[i] != expected[i] {
			t.Fatalf("line %d: got %q, want %q", i, lines[i], expected[i])
		}
	}
}

func TestBuildSummaryUsesVersionOnlyWithoutMetadata(t *testing.T) {
	oldVersion := Version
	oldCommit := BuildCommit
	oldDate := BuildDate
	t.Cleanup(func() {
		Version = oldVersion
		BuildCommit = oldCommit
		BuildDate = oldDate
	})

	Version = "v-test"
	BuildCommit = "unknown"
	BuildDate = ""

	if summary := BuildSummary(); summary != "v-test" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestBuildSummaryIncludesMetadata(t *testing.T) {
	oldVersion := Version
	oldCommit := BuildCommit
	oldDate := BuildDate
	t.Cleanup(func() {
		Version = oldVersion
		BuildCommit = oldCommit
		BuildDate = oldDate
	})

	Version = "v-test"
	BuildCommit = "abc123"
	BuildDate = "2026-05-01T00:00:00Z"

	expected := "v-test commit=abc123 built=2026-05-01T00:00:00Z"
	if summary := BuildSummary(); summary != expected {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestResolveSecretsFromEnvRejectsMissingSecrets(t *testing.T) {
	_, _, err := resolveSecretsFromEnv("", "")
	if err == nil {
		t.Fatal("expected missing secrets to fail")
	}
}

func TestResolveSecretsFromEnvRejectsDefaultSessionSecret(t *testing.T) {
	_, _, err := resolveSecretsFromEnv("random_string", "")
	if err == nil {
		t.Fatal("expected default session secret to fail")
	}
}

func TestResolveSecretsFromEnvRejectsDefaultCryptoSecret(t *testing.T) {
	_, _, err := resolveSecretsFromEnv("", "random_string")
	if err == nil {
		t.Fatal("expected default crypto secret to fail")
	}
}

func TestResolveSecretsFromEnvUsesSessionSecretForCryptoFallback(t *testing.T) {
	sessionSecret, cryptoSecret, err := resolveSecretsFromEnv("session-secret", "")
	if err != nil {
		t.Fatalf("expected session secret fallback to succeed, got %v", err)
	}
	if sessionSecret != "session-secret" || cryptoSecret != "session-secret" {
		t.Fatalf("unexpected secrets: session=%q crypto=%q", sessionSecret, cryptoSecret)
	}
}

func TestResolveSecretsFromEnvUsesCryptoSecretForSessionFallback(t *testing.T) {
	sessionSecret, cryptoSecret, err := resolveSecretsFromEnv("", "crypto-secret")
	if err != nil {
		t.Fatalf("expected crypto secret fallback to succeed, got %v", err)
	}
	if sessionSecret != "crypto-secret" || cryptoSecret != "crypto-secret" {
		t.Fatalf("unexpected secrets: session=%q crypto=%q", sessionSecret, cryptoSecret)
	}
}

func TestResolveSecretsFromEnvPreservesDistinctSecrets(t *testing.T) {
	sessionSecret, cryptoSecret, err := resolveSecretsFromEnv("session-secret", "crypto-secret")
	if err != nil {
		t.Fatalf("expected explicit secrets to succeed, got %v", err)
	}
	if sessionSecret != "session-secret" || cryptoSecret != "crypto-secret" {
		t.Fatalf("unexpected secrets: session=%q crypto=%q", sessionSecret, cryptoSecret)
	}
}

// initConstantEnv is the only path that turns the container environment into the
// streaming timeout the relay uses. A compose file can forward the variable
// correctly and still have no effect if this mapping breaks, so pin it here.
func TestInitConstantEnvMapsStreamingTimeout(t *testing.T) {
	old := constant.StreamingTimeout
	t.Cleanup(func() { constant.StreamingTimeout = old })

	t.Setenv("STREAMING_TIMEOUT", "777")
	initConstantEnv()
	if constant.StreamingTimeout != 777 {
		t.Fatalf("STREAMING_TIMEOUT=777 produced %d, want 777", constant.StreamingTimeout)
	}

	// An unset variable must fall back to the code default rather than to zero:
	// a zero value is treated as "unset" downstream and replaced by
	// DefaultStreamingTimeout, which would hide a misconfigured deployment.
	t.Setenv("STREAMING_TIMEOUT", "")
	initConstantEnv()
	if constant.StreamingTimeout != 300 {
		t.Fatalf("unset STREAMING_TIMEOUT produced %d, want the 300 default", constant.StreamingTimeout)
	}

	// An unparsable value falls back to the default and logs; it must not panic.
	t.Setenv("STREAMING_TIMEOUT", "not-a-number")
	initConstantEnv()
	if constant.StreamingTimeout != 300 {
		t.Fatalf("unparsable STREAMING_TIMEOUT produced %d, want the 300 default", constant.StreamingTimeout)
	}
}
