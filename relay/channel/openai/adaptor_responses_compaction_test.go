package openai

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestRemovesUnsupportedCompactionInput(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"keep prior summary"}]},
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 2)
	for _, item := range items {
		var itemType string
		require.NoError(t, common.Unmarshal(item["type"], &itemType))
		require.NotEqual(t, "compaction", itemType)
	}
}

func TestConvertOpenAIResponsesRequestRejectsCompactionOnlyInput(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[{"type":"compaction","encrypted_content":"opaque"}]`),
	}

	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compaction input")
}

func TestConvertOpenAIResponsesRequestPreservesNonObjectArrayItems(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			"plain input item",
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 2)
	var first string
	require.NoError(t, common.Unmarshal(items[0], &first))
	require.Equal(t, "plain input item", first)
}

func TestConvertOpenAIResponsesCompactRequestPreservesCompactionInput(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[{"type":"compaction","encrypted_content":"opaque"}]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
}
