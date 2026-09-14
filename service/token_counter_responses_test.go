package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const responsesTokenCounterTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAF/gL+3nM/1QAAAABJRU5ErkJggg=="

func TestEstimateRequestTokenForAPIIncludesResponsesMediaMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	InitTokenEncoders()
	previousGetMediaToken := constant.GetMediaToken
	previousGetMediaTokenNotStream := constant.GetMediaTokenNotStream
	constant.GetMediaToken = true
	constant.GetMediaTokenNotStream = true
	t.Cleanup(func() {
		constant.GetMediaToken = previousGetMediaToken
		constant.GetMediaTokenNotStream = previousGetMediaTokenNotStream
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-5.6-sol")
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-5.6-sol",
		IsStream:        true,
	}

	baselineTokens, err := EstimateRequestTokenForAPI(ctx, &types.TokenCountMeta{
		CombineText: "same responses text",
	}, info)
	require.NoError(t, err)

	req := &dto.OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"same responses text"}]},
			{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}},
			{"type":"input_file","file_data":"cGxhY2Vob2xkZXI="},
			{"type":"input_file","file_id":"file_uploaded_input"},
			{"type":"image_generation_call","result":"` + responsesTokenCounterTestPNG + `"}
		]`),
	}
	meta := req.GetTokenCountMeta()

	require.Len(t, meta.Files, 4)
	require.Equal(t, types.FileTypeAudio, meta.Files[0].FileType)
	require.Equal(t, types.FileTypeFile, meta.Files[1].FileType)
	require.Equal(t, types.FileTypeFile, meta.Files[2].FileType)
	require.Nil(t, meta.Files[2].Source)
	require.Equal(t, types.FileTypeImage, meta.Files[3].FileType)

	mediaTokens, err := EstimateRequestTokenForAPI(ctx, meta, info)

	require.NoError(t, err)
	// Audio, inline files, uploaded-file references, and a 1x1 GPT-5 image add
	// 256, 4096, 4096, and 630 tokens without fetching the opaque file ID.
	require.Equal(t, baselineTokens+256+4096+4096+630, mediaTokens)
}
