package log_retention_setting

import "testing"

// NormalizeValue must return the value that actually takes effect, not merely
// report whether it is acceptable. ValidateValue alone trimmed before checking,
// so " true " passed validation while the untrimmed string reached
// updateConfigFromMap, failed strconv.ParseBool and left the field unchanged.
func TestNormalizeValueTrimsAndCanonicalizes(t *testing.T) {
	booleanCases := map[string]string{
		"true":     "true",
		" true ":   "true",
		"\ttrue\n": "true",
		"TRUE":     "true",
		"1":        "true",
		"false":    "false",
		" 0 ":      "false",
	}
	for key := range map[string]struct{}{"enabled": {}, "server_log_cleanup_enabled": {}} {
		for raw, want := range booleanCases {
			got, err := NormalizeValue(key, raw)
			if err != nil {
				t.Fatalf("NormalizeValue(%q, %q) error: %v", key, raw, err)
			}
			if got != want {
				t.Errorf("NormalizeValue(%q, %q) = %q, want %q", key, raw, got, want)
			}
		}
	}

	// Every integer key must be trimmed and canonicalized, not just the one the
	// suite happened to name. A key that skipped normalization would accept
	// " 12 " at the API, store it untrimmed, and leave the struct at its old
	// value because strconv.Atoi rejects the surrounding spaces.
	// The probe values stay inside every key's range (the tightest upper bound
	// is run_interval_hours at 720; every lower bound is 0 or 1).
	intCases := []struct{ raw, want string }{
		{"500", "500"},
		{" 500 ", "500"},
		{"\t12\n", "12"},
		{"0000500", "500"},
	}
	for _, key := range []string{
		"run_interval_hours",
		"batch_size",
		"max_batches",
		"consume_retention_days",
		"error_retention_days",
		"system_retention_days",
		"manage_retention_days",
		"topup_retention_days",
		"refund_retention_days",
		"unknown_retention_days",
		"request_payload_retention_days",
		"response_payload_retention_days",
		"server_log_keep_files",
		"server_log_keep_days",
		"server_log_max_total_size_mb",
	} {
		for _, c := range intCases {
			got, err := NormalizeValue(key, c.raw)
			if err != nil {
				t.Fatalf("NormalizeValue(%q, %q) error: %v", key, c.raw, err)
			}
			if got != c.want {
				t.Errorf("NormalizeValue(%q, %q) = %q, want %q", key, c.raw, got, c.want)
			}
		}
	}
}

func TestNormalizeValueRejectsOutOfRangeAndGarbage(t *testing.T) {
	cases := []struct{ key, raw string }{
		{"batch_size", " 999999 "},
		{"batch_size", "0"},
		{"batch_size", "not-a-number"},
		{"run_interval_hours", "0"},
		{"max_batches", "999999"},
		{"server_log_keep_files", "-1"},
		{"enabled", "yes"},
		{"enabled", ""},
	}
	for _, c := range cases {
		if _, err := NormalizeValue(c.key, c.raw); err == nil {
			t.Errorf("NormalizeValue(%q, %q) should fail, got nil error", c.key, c.raw)
		}
	}
}

// An unknown key is passed through trimmed rather than dropped, so a future
// field added to the struct is not silently discarded before it is stored.
func TestNormalizeValuePassesUnknownKeyThroughTrimmed(t *testing.T) {
	got, err := NormalizeValue("some_future_key", "  value  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "value" {
		t.Errorf("NormalizeValue returned %q, want %q", got, "value")
	}
}

// ValidateValue keeps its existing contract: an accepted value returns nil.
func TestValidateValueAcceptsNormalizedForms(t *testing.T) {
	for _, raw := range []string{"true", " true ", "1", "500", " 500 "} {
		if err := ValidateValue("enabled", raw); err != nil && raw != "500" && raw != " 500 " {
			t.Errorf("ValidateValue(enabled, %q) returned %v", raw, err)
		}
	}
	if err := ValidateValue("batch_size", " 500 "); err != nil {
		t.Errorf("ValidateValue(batch_size, %q) returned %v", " 500 ", err)
	}
}
