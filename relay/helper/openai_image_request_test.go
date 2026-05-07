package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidOpenAIImageEditJSONPreservesReferenceFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{
		"model":"gpt-image-1",
		"prompt":"edit this",
		"images":["data:image/png;base64,AAA","data:image/png;base64,BBB"],
		"mask":"data:image/png;base64,CCC",
		"input_fidelity":"high"
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	imageReq, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)

	payload, err := common.Marshal(imageReq)
	require.NoError(t, err)

	var fields map[string]any
	require.NoError(t, common.Unmarshal(payload, &fields))
	require.Equal(t, []any{"data:image/png;base64,AAA", "data:image/png;base64,BBB"}, fields["images"])
	require.Equal(t, "data:image/png;base64,CCC", fields["mask"])
	require.Equal(t, "high", fields["input_fidelity"])
}
