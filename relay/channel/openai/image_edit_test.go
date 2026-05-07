package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertImageEditJSONRequestPreservesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	req := dto.ImageRequest{
		Model:         "gpt-image-1",
		Prompt:        "edit this",
		Images:        mustRawMessage(t, `["data:image/png;base64,AAA"]`),
		Mask:          mustRawMessage(t, `"data:image/png;base64,BBB"`),
		InputFidelity: mustRawMessage(t, `"high"`),
	}
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}

	converted, err := (&Adaptor{}).ConvertImageRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.ImageRequest)
	require.True(t, ok)

	payload, err := common.Marshal(convertedReq)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"gpt-image-1",
		"prompt":"edit this",
		"images":["data:image/png;base64,AAA"],
		"mask":"data:image/png;base64,BBB",
		"input_fidelity":"high"
	}`, string(payload))
}

func TestDoImageEditJSONRequestUsesJSONRequestPath(t *testing.T) {
	// service.InitHttpClient mutates process-wide HTTP client state, keep this test serial.
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	var upstreamContentType string
	var upstreamBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamContentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		upstreamBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	payload := `{"model":"gpt-image-1","prompt":"edit this","images":["data:image/png;base64,AAA"]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesEdits,
		RequestURLPath: "/v1/images/edits",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: server.URL,
			ApiKey:         "test-key",
		},
	}

	resp, err := (&Adaptor{}).DoRequest(c, info, bytes.NewBufferString(payload))
	require.NoError(t, err)
	httpResp, ok := resp.(*http.Response)
	require.True(t, ok)
	defer httpResp.Body.Close()

	require.Equal(t, "application/json", upstreamContentType)
	require.JSONEq(t, payload, upstreamBody)
}

func mustRawMessage(t *testing.T, value string) json.RawMessage {
	t.Helper()
	return json.RawMessage(value)
}
