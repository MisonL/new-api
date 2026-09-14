package taskcommon

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

type metadataIdentityTestPayload struct {
	Model     string `json:"model"`
	ModelName string `json:"model_name"`
	ReqKey    string `json:"req_key"`
	Prompt    string `json:"prompt"`
}

func TestUnmarshalMetadataDoesNotMutateCallerOrOverrideIdentity(t *testing.T) {
	metadata := map[string]any{
		"MODEL":          "attacker-model",
		"modelName":      "attacker-model-name",
		"req-key":        "attacker-req-key",
		"upstream_model": "attacker-upstream",
		"prompt":         "keep-me",
	}
	original := make(map[string]any, len(metadata))
	for key, value := range metadata {
		original[key] = value
	}

	payload := metadataIdentityTestPayload{
		Model:     "mapped-model",
		ModelName: "mapped-model-name",
		ReqKey:    "mapped-req-key",
	}
	require.NoError(t, UnmarshalMetadata(metadata, &payload))
	require.Equal(t, metadataIdentityTestPayload{
		Model:     "mapped-model",
		ModelName: "mapped-model-name",
		ReqKey:    "mapped-req-key",
		Prompt:    "keep-me",
	}, payload)
	require.Equal(t, original, metadata)
}

func TestIsProtectedMetadataIdentityKeyVariants(t *testing.T) {
	for _, key := range []string{
		"model",
		"Model",
		"model_name",
		"modelName",
		"model-name",
		"req_key",
		"ReqKey",
		"upstream_model_name",
		"origin-model",
	} {
		require.True(t, isProtectedMetadataIdentityKey(key), key)
	}
	for _, key := range []string{"prompt", "duration", "aspect_ratio", "model_type"} {
		require.False(t, isProtectedMetadataIdentityKey(key), key)
	}
}

func TestBuildProxyURLTrimsServerAddressTrailingSlash(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://gateway.example/edge/"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	require.Equal(t, "https://gateway.example/edge/v1/videos/task_1/content", BuildProxyURL("task_1"))
}

func TestCapturedTaskPollingPathKeepsExplicitRootPath(t *testing.T) {
	require.Equal(t, "/", CapturedTaskPollingPath(map[string]any{
		"polling_path": "/",
	}, "/jimeng"))
}

func TestCapturedTaskPollingPathUsesFallbackOnlyWhenMissingOrBlank(t *testing.T) {
	for _, body := range []map[string]any{
		nil,
		{},
		{"polling_path": ""},
		{"polling_path": "  "},
	} {
		require.Equal(t, "/jimeng", CapturedTaskPollingPath(body, "/jimeng"))
	}
}
