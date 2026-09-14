package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestTaskRouteIntentFromRequestUsesMetadataActionFirst(t *testing.T) {
	intent := TaskRouteIntentFromRequest(TaskSubmitReq{
		Images: []string{"one", "two", "three"},
		Metadata: map[string]interface{}{
			"action": constant.TaskActionGenerate,
		},
	})

	require.Equal(t, constant.TaskActionGenerate, intent.Action)
	require.True(t, intent.ExplicitAction)
}

func TestTaskSubmitReqUnmarshalNormalizesTopLevelAction(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, req.UnmarshalJSON([]byte(`{"action":"generate","images":["one","two","three"]}`)))
	intent := TaskRouteIntentFromRequest(req)
	require.Equal(t, TaskRouteIntent{Action: constant.TaskActionGenerate, ExplicitAction: true}, intent)

	var nested TaskSubmitReq
	require.NoError(t, nested.UnmarshalJSON([]byte(`{"action":"generate","metadata":{"action":"referenceGenerate"}}`)))
	require.Equal(t, TaskRouteIntent{Action: constant.TaskActionReferenceGenerate, ExplicitAction: true}, TaskRouteIntentFromRequest(nested))
}

func TestTaskRouteIntentFromRequestDerivesImageActions(t *testing.T) {
	tests := []struct {
		name   string
		req    TaskSubmitReq
		action string
	}{
		{name: "reference", req: TaskSubmitReq{Images: []string{"one", "two", "three"}}, action: constant.TaskActionReferenceGenerate},
		{name: "first tail", req: TaskSubmitReq{Images: []string{"one", "two"}}, action: constant.TaskActionFirstTailGenerate},
		{name: "single image", req: TaskSubmitReq{InputReference: "one"}, action: constant.TaskActionGenerate},
		{name: "text", req: TaskSubmitReq{}, action: constant.TaskActionTextGenerate},
		{name: "empty images are ignored", req: TaskSubmitReq{Images: []string{"", "  "}}, action: constant.TaskActionTextGenerate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := TaskRouteIntentFromRequest(tt.req)
			require.Equal(t, tt.action, intent.Action)
			require.False(t, intent.ExplicitAction)
		})
	}
}

func TestTaskRouteIntentFromPayloadAcceptsProviderSpecificImageShapes(t *testing.T) {
	tests := []struct {
		name         string
		payload      map[string]any
		fileCount    int
		wantAction   string
		wantExplicit bool
	}{
		{
			name:       "object image",
			payload:    map[string]any{"image": map[string]any{"url": "https://image.example/input.png"}},
			wantAction: constant.TaskActionGenerate,
		},
		{
			name:       "three multipart files",
			payload:    map[string]any{"input_reference": ""},
			fileCount:  3,
			wantAction: constant.TaskActionReferenceGenerate,
		},
		{
			name:       "nested extra body images",
			payload:    map[string]any{"extra_body": map[string]any{"images": []any{"one", "two"}}},
			wantAction: constant.TaskActionFirstTailGenerate,
		},
		{
			name: "explicit action wins",
			payload: map[string]any{
				"images":   []any{"one", "two", "three"},
				"metadata": map[string]any{"action": constant.TaskActionGenerate},
			},
			wantAction:   constant.TaskActionGenerate,
			wantExplicit: true,
		},
		{
			name: "multipart top-level action wins",
			payload: map[string]any{
				"action": constant.TaskActionGenerate,
				"images": []any{"one", "two", "three"},
			},
			wantAction:   constant.TaskActionGenerate,
			wantExplicit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := TaskRouteIntentFromPayload(tt.payload, tt.fileCount)
			require.Equal(t, tt.wantAction, intent.Action)
			require.Equal(t, tt.wantExplicit, intent.ExplicitAction)
		})
	}
}

func TestResolveTaskWireModelOnlyOverridesViduReferenceRoute(t *testing.T) {
	require.Equal(t, "viduq2", ResolveTaskWireModel(constant.ChannelTypeVidu, constant.TaskActionReferenceGenerate, "mapped-vidu"))
	require.Equal(t, "mapped-vidu", ResolveTaskWireModel(constant.ChannelTypeVidu, constant.TaskActionGenerate, "mapped-vidu"))
	require.Equal(t, "mapped-vidu", ResolveTaskWireModel(constant.ChannelTypeOpenAI, constant.TaskActionReferenceGenerate, "mapped-vidu"))
	require.Equal(t, "viduq2", ResolveTaskWireModel(constant.ChannelTypeVidu, constant.TaskActionReferenceGenerate, ""))
}
