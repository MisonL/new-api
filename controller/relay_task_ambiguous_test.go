package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRedactAmbiguousTaskDataOmitsMalformedProviderBody(t *testing.T) {
	body := []byte(`<html>Authorization: Bearer provider-secret</html>`)

	redacted := redactAmbiguousTaskData(body)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(redacted, &payload))
	require.Equal(t, true, payload["response_body_omitted"])
	require.Equal(t, float64(len(body)), payload["response_body_bytes"])
	require.NotContains(t, string(redacted), "provider-secret")
}

func TestRedactAmbiguousTaskDataBoundsLargeValidProviderBody(t *testing.T) {
	body, err := common.Marshal(map[string]any{
		"status":   "accepted",
		"metadata": strings.Repeat("x", maxAmbiguousTaskDataBytes),
	})
	require.NoError(t, err)

	redacted := redactAmbiguousTaskData(body)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(redacted, &payload))
	require.Equal(t, true, payload["response_body_omitted"])
	require.Equal(t, float64(len(body)), payload["response_body_bytes"])
}

func TestAmbiguousTaskFailureReasonIsBoundedAndMasked(t *testing.T) {
	reason := ambiguousTaskFailureReason(&dto.TaskError{Message: "Authorization: Bearer provider-secret\n" + strings.Repeat("x", 1024)})

	require.LessOrEqual(t, len([]rune(reason)), 512)
	require.NotContains(t, reason, "provider-secret")
	require.NotContains(t, reason, "\n")
}
