package jimeng

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestHeaderSignsRuntimeHeaderOverrides(t *testing.T) {
	adaptor := &TaskAdaptor{
		accessKey: "ak",
		secretKey: "sk",
	}
	req, err := http.NewRequest(http.MethodPost, "https://origin.example.com/?Action=CVSync2AsyncSubmitTask", strings.NewReader(`{"ok":true}`))
	require.NoError(t, err)

	err = adaptor.BuildRequestHeaderWithRuntimeHeaderOverride(nil, req, &common.RelayInfo{
		ChannelMeta: &common.ChannelMeta{ApiKey: "ak|sk"},
	}, map[string]string{
		"content-type": "application/vnd.signed+json",
		"host":         "cdn.example.com",
	})
	require.NoError(t, err)

	require.Equal(t, "application/vnd.signed+json", req.Header.Get("Content-Type"))
	require.Equal(t, "cdn.example.com", req.Host)
	require.Equal(t, "cdn.example.com", req.Header.Get("Host"))
	require.Contains(t, req.Header.Get("Authorization"), "SignedHeaders=content-type;host;x-content-sha256;x-date")
}
