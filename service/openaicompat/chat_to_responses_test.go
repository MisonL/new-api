package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChatCompletionsRequestToResponsesRequestPreservesFileContent(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-4.1",
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					dto.MediaContent{
						Type: dto.ContentTypeFile,
						File: &dto.MessageFile{
							FileName: "notes.txt",
							FileData: "YWxwaGE=",
						},
					},
				},
			},
		},
	}

	responsesReq, err := ChatCompletionsRequestToResponsesRequest(req)
	require.NoError(t, err)

	var inputItems []map[string]any
	err = common.Unmarshal(responsesReq.Input, &inputItems)
	require.NoError(t, err)
	require.Len(t, inputItems, 1)

	content, ok := inputItems[0]["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)

	part, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "input_file", part["type"])

	file, ok := part["file"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "notes.txt", file["filename"])
	require.Equal(t, "YWxwaGE=", file["file_data"])
}

