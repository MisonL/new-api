package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAgnesAdaptorExposesOfficialModelCatalog(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{ChannelType: constant.ChannelTypeAgnes}

	require.Equal(t, AgnesModelList, adaptor.GetModelList())
	require.Equal(t, AgnesChannelName, adaptor.GetChannelName())
}

func TestGetRequestURLForAgnesImageEditUsesGenerationsEndpoint(t *testing.T) {
	t.Parallel()

	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesEdits,
		RequestURLPath: "/v1/images/edits",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    "https://apihub.agnes-ai.com",
			UpstreamModelName: "agnes-image-2.1-flash",
		},
	}

	got, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://apihub.agnes-ai.com/v1/images/generations", got)
}

func TestGetRequestURLForMappedAgnesImageModelUsesGenerationsEndpoint(t *testing.T) {
	t.Parallel()

	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesEdits,
		RequestURLPath: "/v1/images/edits",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    "https://apihub.agnes-ai.com",
			UpstreamModelName: "gpt-image-1",
		},
	}
	info.UpstreamModelName = "agnes-image-2.1-flash"

	got, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://apihub.agnes-ai.com/v1/images/generations", got)
}

func TestGetRequestURLForAgnesChannelImageGenerationUsesGenerationsEndpoint(t *testing.T) {
	t.Parallel()

	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesGenerations,
		RequestURLPath: "/v1/images/generations",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeAgnes,
			ChannelBaseUrl:    "https://apihub.agnes-ai.com",
			UpstreamModelName: "agnes-image-2.1-flash",
		},
	}

	got, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://apihub.agnes-ai.com/v1/images/generations", got)
}

func TestConvertAgnesImageGenerationMovesURLResponseFormatIntoExtraBody(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	req := dto.ImageRequest{
		Model:          "agnes-image-2.1-flash",
		Prompt:         "a tiny green cube",
		Size:           "1024x768",
		ResponseFormat: "url",
	}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "agnes-image-2.1-flash",
		},
	}

	converted, err := (&Adaptor{}).ConvertImageRequest(c, info, req)
	require.NoError(t, err)

	convertedReq, ok := converted.(dto.ImageRequest)
	require.True(t, ok)

	payload, err := common.Marshal(convertedReq)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"agnes-image-2.1-flash",
		"prompt":"a tiny green cube",
		"size":"1024x768",
		"extra_body":{"response_format":"url"}
	}`, string(payload))
}

func TestConvertAgnesImageGenerationMapsB64ToReturnBase64(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	req := dto.ImageRequest{
		Model:          "agnes-image-2.0-flash",
		Prompt:         "a tiny green cube",
		Size:           "1024x768",
		ResponseFormat: "b64_json",
	}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "agnes-image-2.0-flash",
		},
	}

	converted, err := (&Adaptor{}).ConvertImageRequest(c, info, req)
	require.NoError(t, err)

	convertedReq, ok := converted.(dto.ImageRequest)
	require.True(t, ok)

	payload, err := common.Marshal(convertedReq)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"agnes-image-2.0-flash",
		"prompt":"a tiny green cube",
		"size":"1024x768",
		"return_base64":true
	}`, string(payload))
}

func TestConvertAgnesImageEditJSONMovesImagesAndResponseFormatIntoExtraBody(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	req := dto.ImageRequest{
		Model:          "agnes-image-2.1-flash",
		Prompt:         "make it orange",
		Size:           "1024x768",
		ResponseFormat: "b64_json",
		Images:         mustRawMessage(t, `["data:image/png;base64,AAA"]`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesEdits,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "agnes-image-2.1-flash",
		},
	}

	converted, err := (&Adaptor{}).ConvertImageRequest(c, info, req)
	require.NoError(t, err)

	convertedReq, ok := converted.(dto.ImageRequest)
	require.True(t, ok)

	payload, err := common.Marshal(convertedReq)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"agnes-image-2.1-flash",
		"prompt":"make it orange",
		"size":"1024x768",
		"extra_body":{
			"image":["data:image/png;base64,AAA"],
			"response_format":"b64_json"
		}
	}`, string(payload))
}

func TestConvertAgnesImageEditMultipartReturnsError(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(""))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=test-boundary")

	req := dto.ImageRequest{
		Model:  "agnes-image-2.1-flash",
		Prompt: "make it orange",
	}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesEdits,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "agnes-image-2.1-flash",
		},
	}

	_, err := (&Adaptor{}).ConvertImageRequest(c, info, req)
	require.ErrorContains(t, err, "Agnes image models require JSON image edit requests")
}
