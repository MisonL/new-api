package doubao

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHasVideoInMetadata(t *testing.T) {
	assert.True(t, hasVideoInMetadata(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type":      "video_url",
				"video_url": map[string]interface{}{"url": "https://example.com/video.mp4"},
			},
		},
	}))

	assert.False(t, hasVideoInMetadata(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": "https://example.com/image.png"},
			},
		},
	}))
}

func TestEstimateBilling_ReturnsVideoDiscountForSupportedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "video_url",
					"video_url": map[string]interface{}{
						"url": "https://example.com/video.mp4",
					},
				},
			},
		},
	})

	adaptor := &TaskAdaptor{}
	ratios := adaptor.EstimateBilling(c, &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
	})

	assert.Equal(t, map[string]float64{
		"video_input": 28.0 / 46.0,
	}, ratios)
}

func TestEstimateBilling_IgnoresUnsupportedModelAndMissingVideo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type":      "image_url",
					"image_url": map[string]interface{}{"url": "https://example.com/image.png"},
				},
			},
		},
	})

	adaptor := &TaskAdaptor{}
	assert.Nil(t, adaptor.EstimateBilling(c, &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-1-5-pro-251215",
	}))
}
