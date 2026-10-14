package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// maskAuthTokenPattern treats the word after "token" as a credential. Quota
// diagnostics read "token quota is not enough" and "token remain quota: N", so
// without these prose exemptions the reason for a rejection was masked away and
// operators saw "token *** is not enough".
func TestMaskSensitiveInfoKeepsQuotaProse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "quota diagnostic",
			in:   "token quota is not enough, token remain quota: 100",
			want: "token quota is not enough, token remain quota: 100",
		},
		{
			name: "usage diagnostic already exempt",
			in:   "token usage exceeded",
			want: "token usage exceeded",
		},
		{
			name: "invalid token diagnostic already exempt",
			in:   "this token is not valid",
			want: "this token is not valid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, MaskSensitiveInfo(tc.in))
		})
	}
}

// The same masking mistake appeared on other diagnostics that name a token
// concept rather than carrying a token value. Each string below is the verbatim
// text of an error built somewhere in this repository, and each was rewritten to
// "token *** ..." before the words were exempted.
func TestMaskSensitiveInfoKeepsTokenConceptProse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "model access configuration",
			in:   "token model access configuration is invalid",
			want: "token model access configuration is invalid",
		},
		{
			name: "scope requirement",
			in:   "synthetic compact state scope is required: token id is required",
			want: "synthetic compact state scope is required: token id is required",
		},
		{
			name: "oauth provider endpoint",
			in:   "token endpoint is required",
			want: "token endpoint is required",
		},
		{
			name: "opaque replay authentication",
			in:   "Responses opaque replay body requires token authentication",
			want: "Responses opaque replay body requires token authentication",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, MaskSensitiveInfo(tc.in))
		})
	}
}

// The exemption must not turn into a hole: a real credential after "token"
// still has to be masked.
func TestMaskSensitiveInfoStillMasksRealCredentials(t *testing.T) {
	for _, in := range []string{
		"token sk-abcdef0123456789 rejected",
		"token 9f8e7d6c5b4a39281706 rejected",
		"bearer eyJhbGciOiJIUzI1NiJ9 rejected",
		// A credential that merely begins with an exempt word is not the exempt
		// word itself, so the exact-match lookup must still mask it.
		"token model-abcdef0123456789 rejected",
		"token id-abcdef0123456789 rejected",
		"token endpoint-abcdef0123456789 rejected",
	} {
		masked := MaskSensitiveInfo(in)
		require.NotContains(t, masked, "sk-abcdef0123456789")
		require.NotContains(t, masked, "9f8e7d6c5b4a39281706")
		require.NotContains(t, masked, "eyJhbGciOiJIUzI1NiJ9")
		require.NotContains(t, masked, "model-abcdef0123456789")
		require.NotContains(t, masked, "id-abcdef0123456789")
		require.NotContains(t, masked, "endpoint-abcdef0123456789")
		require.Contains(t, masked, "***")
	}
}
