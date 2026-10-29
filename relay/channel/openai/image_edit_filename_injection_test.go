package openai

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 客户端文件名经 RFC 2231 filename* 百分号解码后可以携带 CRLF
// （Go 的 mime/multipart 会解码 %0D%0A）。网关把该文件名原样拼进
// Content-Disposition 再 CreatePart，CRLF 会在发往上游的 multipart 里
// 注入额外的 part 头。
func TestConvertImageEditRequestSanitizesFilenameCRLF(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	fmt.Fprintf(&body, "--BOUND\r\n")
	fmt.Fprintf(&body, "Content-Disposition: form-data; name=\"image\"; filename=\"plain.png\"; filename*=UTF-8''a%%0D%%0AX-Injected%%3A%%20yes.png\r\n")
	fmt.Fprintf(&body, "Content-Type: image/png\r\n\r\n")
	body.WriteString("fake-image-bytes")
	fmt.Fprintf(&body, "\r\n--BOUND--\r\n")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=BOUND")
	require.NoError(t, c.Request.ParseMultipartForm(1<<20))

	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}
	converted, err := (&Adaptor{}).ConvertImageRequest(c, info, dto.ImageRequest{Model: "gpt-image-1", Prompt: "edit"})
	require.NoError(t, err)

	reader, ok := converted.(*bytes.Buffer)
	require.True(t, ok)

	produced := reader.String()
	require.NotContains(t, produced, "\r\nX-Injected",
		"a CRLF in the client filename must not inject a part header")
	// 文件名本身仍应保留（去掉控制字符），而不是整个丢弃
	require.Contains(t, produced, `filename="aX-Injected: yes.png"`)
}
