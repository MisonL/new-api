package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestProcessToolsParsesJSONMapTool(t *testing.T) {
	normalTools, webSearchTools := ProcessTools([]any{
		map[string]any{
			"name":        "lookup_order",
			"description": "Find an order by id",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"order_id": map[string]any{"type": "string"},
				},
			},
		},
	})

	require.Len(t, normalTools, 1)
	require.Empty(t, webSearchTools)
	require.Equal(t, "lookup_order", normalTools[0].Name)
	require.Equal(t, "Find an order by id", normalTools[0].Description)
	require.Equal(t, "object", normalTools[0].InputSchema["type"])
}

func TestProcessToolsParsesJSONMapWebSearchTool(t *testing.T) {
	normalTools, webSearchTools := ProcessTools([]any{
		map[string]any{
			"type": "web_search_20250305",
			"name": "web_search",
			"user_location": map[string]any{
				"type":    "approximate",
				"country": "US",
			},
		},
	})

	require.Empty(t, normalTools)
	require.Len(t, webSearchTools, 1)
	require.Equal(t, "web_search_20250305", webSearchTools[0].Type)
	require.Equal(t, "web_search", webSearchTools[0].Name)
	require.Equal(t, "US", webSearchTools[0].UserLocation.Country)
}

func TestProcessToolsIgnoresInvalidTools(t *testing.T) {
	normalTools, webSearchTools := ProcessTools([]any{
		nil,
		"bad tool",
		map[string]any{},
		map[string]any{"type": "unknown"},
		map[string]any{"type": "web_search_20250305", "user_location": "invalid"},
	})

	require.Empty(t, normalTools)
	require.Empty(t, webSearchTools)
}

func TestClaudeTokenCountMetaIncludesDocumentBlocks(t *testing.T) {
	req := &ClaudeRequest{
		System: []ClaudeMediaMessage{
			{
				Type: "document",
				Source: &ClaudeMessageSource{
					Type:      "base64",
					MediaType: "application/pdf",
					Data:      "system-document",
				},
			},
		},
		Messages: []ClaudeMessage{
			{
				Role: "user",
				Content: []ClaudeMediaMessage{
					{
						Type: "document",
						Source: &ClaudeMessageSource{
							Type:      "base64",
							MediaType: "application/pdf",
							Data:      "message-document",
						},
					},
				},
			},
		},
	}

	meta := req.GetTokenCountMeta()

	require.Len(t, meta.Files, 2)
	require.Equal(t, types.FileTypeFile, meta.Files[0].FileType)
	require.Equal(t, "system-document", meta.Files[0].GetRawData())
	require.Equal(t, types.FileTypeFile, meta.Files[1].FileType)
	require.Equal(t, "message-document", meta.Files[1].GetRawData())
}

func TestClaudeTokenCountMetaIncludesThinkingBlocks(t *testing.T) {
	thinking := "first hidden reasoning"
	req := &ClaudeRequest{
		Messages: []ClaudeMessage{
			{
				Role: "assistant",
				Content: []ClaudeMediaMessage{
					{
						Type:      "thinking",
						Thinking:  &thinking,
						Signature: "sig-first",
					},
					{
						Type:      "redacted_thinking",
						Data:      "redacted-payload",
						Signature: "sig-redacted",
					},
				},
			},
		},
	}

	meta := req.GetTokenCountMeta()

	require.True(t, strings.Contains(meta.CombineText, "first hidden reasoning"))
	require.True(t, strings.Contains(meta.CombineText, "sig-first"))
	require.True(t, strings.Contains(meta.CombineText, "redacted-payload"))
	require.True(t, strings.Contains(meta.CombineText, "sig-redacted"))
}
