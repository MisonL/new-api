package aws

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type awsRoundTripFunc func(*http.Request) (*http.Response, error)

func (f awsRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type awsHTTPStatusError struct {
	statusCode int
}

func (e awsHTTPStatusError) Error() string {
	return http.StatusText(e.statusCode)
}

func (e awsHTTPStatusError) HTTPStatusCode() int {
	return e.statusCode
}

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}

func TestNewAwsClientDoesNotRetryUpstreamRateLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "access-key|secret-key|us-east-1"},
	}

	client, err := newAwsClient(ctx, info)
	require.NoError(t, err)
	require.NotNil(t, client.Options().Retryer)
	require.False(t, client.Options().Retryer.IsErrorRetryable(awsHTTPStatusError{statusCode: http.StatusTooManyRequests}))
}

func TestAwsDirectInvocationsConsumeRateLimitFollowupAtSendBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLimit := operation_setting.GetUpstreamRateLimitRetryLimit()
	operation_setting.SetUpstreamRateLimitRetryLimit(1)
	t.Cleanup(func() {
		operation_setting.SetUpstreamRateLimitRetryLimit(previousLimit)
	})

	testCases := []struct {
		name    string
		request func() any
		invoke  func(*gin.Context, *relaycommon.RelayInfo, *Adaptor) (*types.NewAPIError, *dto.Usage)
	}{
		{
			name: "claude invoke",
			request: func() any {
				return &bedrockruntime.InvokeModelInput{
					ModelId:     awssdk.String("test-model"),
					Body:        []byte(`{}`),
					ContentType: awssdk.String("application/json"),
				}
			},
			invoke: awsHandler,
		},
		{
			name: "stream invoke",
			request: func() any {
				return &bedrockruntime.InvokeModelWithResponseStreamInput{
					ModelId:     awssdk.String("test-model"),
					Body:        []byte(`{}`),
					ContentType: awssdk.String("application/json"),
				}
			},
			invoke: awsStreamHandler,
		},
		{
			name: "nova invoke",
			request: func() any {
				return &bedrockruntime.InvokeModelInput{
					ModelId:     awssdk.String("test-model"),
					Body:        []byte(`{}`),
					ContentType: awssdk.String("application/json"),
				}
			},
			invoke: handleNovaRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client := bedrockruntime.New(bedrockruntime.Options{
				Region:      "us-east-1",
				Credentials: awssdk.NewCredentialsCache(credentials.NewStaticCredentialsProvider("access-key", "secret-key", "")),
				HTTPClient: &http.Client{Transport: awsRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls.Add(1)
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Status:     "429 Too Many Requests",
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"message":"rate limited"}`)),
						Request:    req,
					}, nil
				})},
				Retryer: newAwsRateLimitAwareRetryer(),
			})

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			service.ObserveUpstreamRateLimitRetry(ctx, 206)
			err, _ := tc.invoke(ctx, &relaycommon.RelayInfo{}, &Adaptor{AwsClient: client, AwsReq: tc.request()})

			require.NotNil(t, err)
			require.Equal(t, http.StatusTooManyRequests, err.StatusCode)
			require.Equal(t, int32(1), calls.Load())
			state, ok := service.GetUpstreamRateLimitRetryState(ctx)
			require.True(t, ok)
			require.Equal(t, 1, state.Used)
			require.True(t, state.Terminal)

			secondErr, _ := tc.invoke(ctx, &relaycommon.RelayInfo{}, &Adaptor{AwsClient: client, AwsReq: tc.request()})
			require.NotNil(t, secondErr)
			require.Equal(t, http.StatusTooManyRequests, secondErr.StatusCode)
			require.Equal(t, int32(1), calls.Load())
		})
	}
}
