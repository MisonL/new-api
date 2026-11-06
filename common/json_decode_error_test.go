package common

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type probeRequest struct {
	MaxTokens uint   `json:"max_tokens"`
	Model     string `json:"model"`
}

type probeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Composite destinations, so the sanitizer's array/object mapping is exercised
// against a type whose Go name would otherwise reach the client.
type probeRequestWithMessages struct {
	Messages []probeMessage `json:"messages"`
}

type probeNested struct {
	Flag bool `json:"flag"`
}

type probeRequestWithNested struct {
	StreamOptions *probeNested `json:"stream_options"`
}

func newBodyContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx
}

// A raw *json.UnmarshalTypeError names the gateway's internal struct, e.g.
// "json: cannot unmarshal string into Go struct field probeRequest.max_tokens
// of type uint". Clients must learn which field was wrong without being told
// the Go type names behind the API. The assertion goes through
// UnmarshalBodyReusable because that is the path every relay request takes.
func TestUnmarshalBodyReusableHidesGoTypeNames(t *testing.T) {
	ctx := newBodyContext(t, `{"model":"gpt-5.4","max_tokens":"abc"}`)

	var target probeRequest
	err := UnmarshalBodyReusable(ctx, &target)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "Go struct field")
	require.NotContains(t, err.Error(), "probeRequest")
	require.Contains(t, err.Error(), "max_tokens")
	require.Contains(t, err.Error(), "must be a")
}

// Non-type errors carry no Go type information and must pass through untouched
// so the existing wording other code asserts on is preserved.
func TestSanitizeJSONDecodeErrorPassesThroughOtherErrors(t *testing.T) {
	original := errors.New("EOF")
	require.Equal(t, original, SanitizeJSONDecodeError(original))

	var target probeRequest
	syntaxErr := Unmarshal([]byte(`{"model":`), &target)
	require.Error(t, syntaxErr)
	require.Equal(t, syntaxErr, SanitizeJSONDecodeError(syntaxErr))
}

func TestSanitizeJSONDecodeErrorHandlesNil(t *testing.T) {
	require.NoError(t, SanitizeJSONDecodeError(nil))
}

// The direct helper contract, independent of the gin plumbing.
func TestSanitizeJSONDecodeErrorHidesGoTypeNames(t *testing.T) {
	var target probeRequest
	err := Unmarshal([]byte(`{"model":"gpt-5.4","max_tokens":"abc"}`), &target)
	require.Error(t, err)
	// 原始错误在两个引擎里都会点名 Go 类型（v1: "Go struct field ..."，
	// v2: "Go uint"），清洗后的消息不得保留。
	require.Contains(t, err.Error(), "Go ")

	sanitized := SanitizeJSONDecodeError(err)
	require.NotContains(t, sanitized.Error(), "Go struct field")
	require.NotContains(t, sanitized.Error(), "probeRequest")
	require.Contains(t, sanitized.Error(), "max_tokens")
	require.Contains(t, sanitized.Error(), "must be a")
}

// A struct or slice field must be reported as an object/array too. Rendering
// the destination Go type leaks the internal name: "[]probeMessage" names both
// the package's type and its shape.
func TestSanitizeJSONDecodeErrorHidesCompositeGoTypeNames(t *testing.T) {
	var target probeRequestWithMessages
	err := Unmarshal([]byte(`{"messages":"nope"}`), &target)
	require.Error(t, err)

	sanitized := SanitizeJSONDecodeError(err)
	require.NotContains(t, sanitized.Error(), "probeRequest")
	require.NotContains(t, sanitized.Error(), "probeMessage")
	require.Contains(t, sanitized.Error(), "array")
	require.Contains(t, sanitized.Error(), "must be a")
}

// Every Go kind maps onto a JSON kind name, so no message can contain a raw Go
// type name regardless of which field the client got wrong.
func TestSanitizeJSONDecodeErrorNeverEmitsRawGoTypeName(t *testing.T) {
	for _, target := range []any{
		&probeRequest{},
		&probeRequestWithMessages{},
		&probeRequestWithNested{},
	} {
		err := Unmarshal([]byte(`{"model":"m","max_tokens":"x","messages":"x","stream_options":"x"}`), target)
		require.Error(t, err)
		message := SanitizeJSONDecodeError(err).Error()
		for _, leaked := range []string{"probeRequest", "probeMessage", "probeNested", "dto.", "main."} {
			require.NotContains(t, message, leaked)
		}
	}
}
