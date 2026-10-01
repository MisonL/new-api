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

	intCases := map[string]string{
		"500":     "500",
		" 500 ":   "500",
		"\t12\n":  "12",
		"0005000": "5000",
	}
	for raw, want := range intCases {
		got, err := NormalizeValue("batch_size", raw)
		if err != nil {
			t.Fatalf("NormalizeValue(batch_size, %q) error: %v", raw, err)
		}
		if got != want {
			t.Errorf("NormalizeValue(batch_size, %q) = %q, want %q", raw, got, want)
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
