package common

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestResponsesStreamObservationCompleted(t *testing.T) {
	observation := NewResponsesStreamObservation()
	observation.Observe(dto.ResponsesStreamResponse{Type: "response.created", Response: &dto.OpenAIResponsesResponse{
		ID:     "resp_test_123",
		Status: common.RawMessage(`"in_progress"`),
	}})
	observation.Observe(dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "hello"})
	observation.Observe(dto.ResponsesStreamResponse{Type: "response.completed", Response: &dto.OpenAIResponsesResponse{
		ID:     "resp_test_123",
		Status: common.RawMessage(`"completed"`),
		Usage: &dto.Usage{
			InputTokens:  12,
			OutputTokens: 7,
			TotalTokens:  19,
		},
	}})
	observation.MarkDone()

	result := observation.ToLogMap(3)
	require.Equal(t, 3, result["event_count"])
	require.Equal(t, 3, result["received_events"])
	require.Equal(t, true, result["completed"])
	require.Equal(t, true, result["completion_confirmed"])
	require.Equal(t, true, result["received_done"])
	require.Equal(t, true, result["usage_present"])
	require.Equal(t, 12, result["input_tokens"])
	require.Equal(t, 7, result["output_tokens"])
	require.Equal(t, 19, result["total_tokens"])
	require.Equal(t, "resp_test_123", result["response_id"])
	require.NotContains(t, result, "completion_unconfirmed")
}

func TestResponsesStreamObservationMarksOutputWithoutConfirmedCompletion(t *testing.T) {
	observation := NewResponsesStreamObservation()
	observation.Observe(dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "partial"})

	result := observation.ToLogMap(1)
	require.Equal(t, true, result["output_event_seen"])
	require.Equal(t, false, result["completion_confirmed"])
	require.Equal(t, true, result["completion_unconfirmed"])
	require.NotContains(t, result, "failed_after_output")
}

func TestResponsesStreamObservationMarksFailedAfterOutputAndBoundsSensitiveFields(t *testing.T) {
	observation := NewResponsesStreamObservation()
	observation.Observe(dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "partial"})
	observation.Observe(dto.ResponsesStreamResponse{
		Type: "response.failed",
		Error: &types.OpenAIError{
			Type:  "provider_error",
			Code:  strings.Repeat("secret", 100),
			Param: "token=super-secret-value",
		},
	})

	result := observation.ToLogMap(2)
	require.Equal(t, true, result["failed"])
	require.Equal(t, true, result["failed_after_output"])
	require.LessOrEqual(t, len(result["error_code"].(string)), maxResponsesObservationError)
	require.NotContains(t, result["error_param"], "super-secret-value")
}

func TestResponsesStreamObservationDoesNotClassifyNonResponsesDone(t *testing.T) {
	observation := NewResponsesStreamObservation()
	observation.MarkDone()

	require.Nil(t, observation.ToLogMap(4))
}

func TestResponsesStreamObservationIncompleteIsConfirmed(t *testing.T) {
	observation := NewResponsesStreamObservation()
	observation.Observe(dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "partial"})
	observation.Observe(dto.ResponsesStreamResponse{Type: "response.incomplete", Response: &dto.OpenAIResponsesResponse{
		Status: common.RawMessage(`"incomplete"`),
	}})

	result := observation.ToLogMap(2)
	require.Equal(t, true, result["incomplete"])
	require.Equal(t, true, result["completion_confirmed"])
	require.NotContains(t, result, "completion_unconfirmed")
}
