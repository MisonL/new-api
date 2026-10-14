package common

import "testing"

func TestSanitizeLogLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain_unchanged", in: "model gpt-4 no available channel", want: "model gpt-4 no available channel"},
		{name: "empty", in: "", want: ""},
		{name: "lf_neutralized", in: "gpt-4\n[SYS] forged entry", want: `gpt-4\n[SYS] forged entry`},
		{name: "crlf_neutralized", in: "a\r\n[SYS] forged", want: `a\n[SYS] forged`},
		{name: "bare_cr_neutralized", in: "a\r[SYS] forged", want: `a\r[SYS] forged`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeLogLine(tc.in); got != tc.want {
				t.Fatalf("SanitizeLogLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeLogLineLeavesNoNewline(t *testing.T) {
	// The property the log writers depend on: the result cannot introduce a
	// line break that would forge an additional log entry.
	for _, in := range []string{"a\nb", "a\r\nb", "a\rb", "\n\n", "a\nb\r\nc\rd"} {
		got := SanitizeLogLine(in)
		for _, r := range got {
			if r == '\n' || r == '\r' {
				t.Fatalf("SanitizeLogLine(%q) = %q still contains a line break", in, got)
			}
		}
	}
}

func TestLogPayloadDigestHidesContent(t *testing.T) {
	secret := []byte(`{"customer_email":"a@b.com","amount":100}`)
	got := LogPayloadDigest(secret)
	if got == string(secret) {
		t.Fatalf("LogPayloadDigest returned the payload verbatim: %q", got)
	}
	if len(got) == 0 || got[:4] != "len=" {
		t.Fatalf("LogPayloadDigest(%q) = %q, want a len=/sha256= summary", secret, got)
	}
	// Same input must produce the same digest so log lines stay correlatable.
	if LogPayloadDigest(secret) != got {
		t.Fatalf("LogPayloadDigest is not deterministic")
	}
	if LogPayloadDigest([]byte("other")) == got {
		t.Fatalf("LogPayloadDigest collided for different payloads")
	}
}
