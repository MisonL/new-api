package common

import "testing"

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
