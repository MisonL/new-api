package aws

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// A client that explicitly sends top_p: 0 is asking for greedy sampling, which
// is not the same request as omitting top_p. With plain scalar fields plus
// omitempty both keys vanished from the Bedrock body, so the upstream applied
// its own sampling default instead of the requested one.
func TestFormatRequestKeepsExplicitZeroTopPAndTopK(t *testing.T) {
	body := `{
		"messages": [{"role": "user", "content": "hi"}],
		"max_tokens": 0,
		"temperature": 0,
		"top_p": 0,
		"top_k": 0
	}`

	parsed, err := formatRequest(strings.NewReader(body), nil)
	require.NoError(t, err)

	forwarded, err := common.Marshal(parsed)
	require.NoError(t, err)

	require.Contains(t, string(forwarded), `"top_p":0`)
	require.Contains(t, string(forwarded), `"top_k":0`)
	require.Contains(t, string(forwarded), `"temperature":0`)
}

// The flip side: a client that never mentions top_p must not have one invented
// for it. The pointer types must still omit the key when it is absent.
func TestFormatRequestOmitsAbsentTopPAndTopK(t *testing.T) {
	body := `{"messages": [{"role": "user", "content": "hi"}], "max_tokens": 64}`

	parsed, err := formatRequest(strings.NewReader(body), nil)
	require.NoError(t, err)

	forwarded, err := common.Marshal(parsed)
	require.NoError(t, err)

	require.NotContains(t, string(forwarded), `"top_p"`)
	require.NotContains(t, string(forwarded), `"top_k"`)
}
