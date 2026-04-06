package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestMediaContentGetFileAcceptsFilenameKey(t *testing.T) {
	file := (&MediaContent{
		File: map[string]any{
			"filename":  "notes.txt",
			"file_data": "YWxwaGE=",
		},
	}).GetFile()

	require.NotNil(t, file)
	require.Equal(t, "notes.txt", file.FileName)
	require.Equal(t, "YWxwaGE=", file.FileData)
}

func TestOpenAIResponsesRequestParseInputSupportsEmbeddedFileObject(t *testing.T) {
	inputRaw, err := common.Marshal([]map[string]any{
		{
			"role": "user",
			"content": []map[string]any{
				{
					"type": "input_file",
					"file": map[string]any{
						"filename":  "notes.txt",
						"file_data": "YWxwaGE=",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	req := &OpenAIResponsesRequest{Input: inputRaw}
	inputs := req.ParseInput()
	require.Len(t, inputs, 1)
	require.Equal(t, "input_file", inputs[0].Type)
	require.NotNil(t, inputs[0].File)
	require.Equal(t, "notes.txt", inputs[0].File.FileName)
	require.Equal(t, "YWxwaGE=", inputs[0].File.FileData)
}

