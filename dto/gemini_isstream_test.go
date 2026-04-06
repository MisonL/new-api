package dto

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGeminiChatRequestIsStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name     string
		path     string
		query    string
		expected bool
	}{
		{
			name:     "stream action without alt",
			path:     "/v1beta/models/gemini-2.0-flash:streamGenerateContent",
			query:    "key=sk-xxx",
			expected: true,
		},
		{
			name:     "stream action with alt",
			path:     "/v1beta/models/gemini-2.0-flash:streamGenerateContent",
			query:    "alt=sse&key=sk-xxx",
			expected: true,
		},
		{
			name:     "generate action without alt",
			path:     "/v1beta/models/gemini-2.0-flash:generateContent",
			query:    "key=sk-xxx",
			expected: false,
		},
		{
			name:     "generate action with alt",
			path:     "/v1beta/models/gemini-2.0-flash:generateContent",
			query:    "alt=sse",
			expected: true,
		},
		{
			name:     "non stream action",
			path:     "/v1beta/models/gemini-2.0-flash:embedContent",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			url := tc.path
			if tc.query != "" {
				url += "?" + tc.query
			}
			ctx.Request, _ = http.NewRequest(http.MethodPost, url, nil)

			req := &GeminiChatRequest{}
			assert.Equal(t, tc.expected, req.IsStream(ctx))
		})
	}
}

