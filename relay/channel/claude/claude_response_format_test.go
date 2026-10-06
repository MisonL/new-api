package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// RequestOpenAI2ClaudeMessage dropped response_format on the floor, so a client
// asking a Claude channel for a JSON schema got free-form prose back with no
// error. Claude expresses the same constraint as output_config.format, whose
// schema is the raw JSON Schema -- there is no OpenAI-style name/strict
// wrapper.
//
// output_config is also where the reasoning effort lands, so the two have to
// merge rather than overwrite each other.

func claudeRequestWithResponseFormat(t *testing.T, format *dto.ResponseFormat) *dto.ClaudeRequest {
	t.Helper()
	req, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
		Model:          "claude-sonnet-5",
		MaxTokens:      common.GetPointer(uint(64)),
		Messages:       []dto.Message{{Role: "user", Content: "hi"}},
		ResponseFormat: format,
	})
	require.NoError(t, err)
	return req
}

// decodeOutputConfig reads the merged output_config object.
func decodeOutputConfig(t *testing.T, req *dto.ClaudeRequest) map[string]any {
	t.Helper()
	cfg := map[string]any{}
	if len(req.OutputConfig) == 0 {
		return cfg
	}
	require.NoError(t, common.Unmarshal(req.OutputConfig, &cfg))
	return cfg
}

func jsonSchemaFormat(schema string) *dto.ResponseFormat {
	return &dto.ResponseFormat{
		Type:       "json_schema",
		JsonSchema: []byte(`{"name":"contact","strict":true,"schema":` + schema + `}`),
	}
}

// TestRequestOpenAI2ClaudeMessageMapsJsonSchema is the regression: the schema
// must reach the upstream, unwrapped from the OpenAI json_schema envelope.
func TestRequestOpenAI2ClaudeMessageMapsJsonSchema(t *testing.T) {
	req := claudeRequestWithResponseFormat(t, jsonSchemaFormat(
		`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`))

	cfg := decodeOutputConfig(t, req)
	format, ok := cfg["format"].(map[string]any)
	require.True(t, ok, "output_config.format 必须存在，实际=%v", cfg)
	require.Equal(t, "json_schema", format["type"])

	schema, ok := format["schema"].(map[string]any)
	require.True(t, ok, "format.schema 必须存在")
	require.Equal(t, "object", schema["type"])
	_, hasName := schema["name"]
	require.False(t, hasName, "schema 内不得混入 OpenAI 的 name 包装层")
}

// TestRequestOpenAI2ClaudeMessageMapsJsonObject covers the schema-free variant.
func TestRequestOpenAI2ClaudeMessageMapsJsonObject(t *testing.T) {
	req := claudeRequestWithResponseFormat(t, &dto.ResponseFormat{Type: "json_object"})

	cfg := decodeOutputConfig(t, req)
	format, ok := cfg["format"].(map[string]any)
	require.True(t, ok, "json_object 必须映射为 json_schema 格式，实际=%v", cfg)
	require.Equal(t, "json_schema", format["type"])
	schema, ok := format["schema"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "object", schema["type"])
}

// TestRequestOpenAI2ClaudeMessageLeavesTextFormatUnset is the reverse check:
// a text response_format must not add an output_config at all.
func TestRequestOpenAI2ClaudeMessageLeavesTextFormatUnset(t *testing.T) {
	req := claudeRequestWithResponseFormat(t, &dto.ResponseFormat{Type: "text"})
	require.Empty(t, req.OutputConfig, "text 格式不应产生 output_config")
}

// TestRequestOpenAI2ClaudeMessageKeepsEffortAlongsideFormat pins the merge: an
// Opus 4.6 reasoning request carries both, and one must not clobber the other.
func TestRequestOpenAI2ClaudeMessageKeepsEffortAlongsideFormat(t *testing.T) {
	req, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
		Model:     "claude-opus-4-6-high",
		MaxTokens: common.GetPointer(uint(64)),
		Messages:  []dto.Message{{Role: "user", Content: "hi"}},
		ResponseFormat: &dto.ResponseFormat{
			Type: "json_schema",
			JsonSchema: []byte(
				`{"name":"c","schema":{"type":"object","properties":{"a":{"type":"string"}}}}`),
		},
	})
	require.NoError(t, err)

	cfg := decodeOutputConfig(t, req)
	require.Equal(t, "high", cfg["effort"], "推理档位必须保留")
	format, ok := cfg["format"].(map[string]any)
	require.True(t, ok, "与 effort 共存时 format 仍必须存在，实际=%v", cfg)
	require.Equal(t, "json_schema", format["type"])
}

// TestRequestOpenAI2ClaudeMessageRejectsBrokenJsonSchema covers a malformed
// envelope: reporting it beats silently dropping the constraint.
func TestRequestOpenAI2ClaudeMessageRejectsBrokenJsonSchema(t *testing.T) {
	_, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
		Model:     "claude-sonnet-5",
		MaxTokens: common.GetPointer(uint(64)),
		Messages:  []dto.Message{{Role: "user", Content: "hi"}},
		ResponseFormat: &dto.ResponseFormat{
			Type:       "json_schema",
			JsonSchema: []byte(`{"name":"c","schema":"not-an-object"}`),
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "response_format")
}

// TestRequestOpenAI2ClaudeMessageRejectsUnknownFormatType keeps an unrecognised
// type from being silently ignored.
func TestRequestOpenAI2ClaudeMessageRejectsUnknownFormatType(t *testing.T) {
	_, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
		Model:          "claude-sonnet-5",
		MaxTokens:      common.GetPointer(uint(64)),
		Messages:       []dto.Message{{Role: "user", Content: "hi"}},
		ResponseFormat: &dto.ResponseFormat{Type: "xml"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "response_format")
}
