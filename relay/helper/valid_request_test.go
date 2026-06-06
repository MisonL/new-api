package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidateResponsesCompactionRequestRequiresInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing input",
			body: `{"model":"gpt-5.4"}`,
			want: "input is required",
		},
		{
			name: "null input",
			body: `{"model":"gpt-5.4","input":null}`,
			want: "input is required",
		},
		{
			name: "empty array input",
			body: `{"model":"gpt-5.4","input":[]}`,
			want: "input must contain at least one item",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(tc.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			request, err := GetAndValidateResponsesCompactionRequest(ctx)
			require.Nil(t, request)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestGetAndValidateResponsesCompactionRequestAcceptsNonEmptyInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses/compact",
		strings.NewReader(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesCompactionRequest(ctx)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", request.Model)
	require.NotEmpty(t, request.Input)
}
