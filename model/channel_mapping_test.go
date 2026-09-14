package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetKeysDecodesJSONStringArrayValues(t *testing.T) {
	channel := &Channel{Key: `["key-a","key-b"]`}

	require.Equal(t, []string{"key-a", "key-b"}, channel.GetKeys())
}

func TestGetKeysPreservesJSONObjectValues(t *testing.T) {
	channel := &Channel{Key: `[{"project_id":"project-a"},{"project_id":"project-b"}]`}

	require.Equal(t, []string{`{"project_id":"project-a"}`, `{"project_id":"project-b"}`}, channel.GetKeys())
}

func TestResolveChannelModelMapping(t *testing.T) {
	tests := []struct {
		name        string
		mapping     string
		requested   string
		compact     bool
		wantModel   string
		wantMapped  bool
		wantErrText string
	}{
		{
			name:       "empty mapping keeps requested model",
			requested:  "gpt-5.6-luna",
			wantModel:  "gpt-5.6-luna",
			wantMapped: false,
		},
		{
			name:       "normal mapping follows chain",
			mapping:    `{"client-model":"provider/model-v2","provider/model-v2":"provider/model-v3"}`,
			requested:  "client-model",
			wantModel:  "provider/model-v3",
			wantMapped: true,
		},
		{
			name:       "self mapping is a valid no-op",
			mapping:    `{"model-a":"model-a"}`,
			requested:  "model-a",
			wantModel:  "model-a",
			wantMapped: false,
		},
		{
			name:       "self mapping at chain tail is terminal",
			mapping:    `{"client-model":"provider/model","provider/model":"provider/model"}`,
			requested:  "client-model",
			wantModel:  "provider/model",
			wantMapped: true,
		},
		{
			name:       "compact explicit mapping is terminal",
			mapping:    `{"gpt-5.6-luna-openai-compact":"gpt-5.6-luna","gpt-5.6-luna":"provider/gpt-5.6"}`,
			requested:  "gpt-5.6-luna-openai-compact",
			compact:    true,
			wantModel:  "gpt-5.6-luna",
			wantMapped: true,
		},
		{
			name:       "compact mapping falls through base chain when no explicit compact entry",
			mapping:    `{"gpt-5.6-luna":"provider/gpt-5.6"}`,
			requested:  "gpt-5.6-luna-openai-compact",
			compact:    true,
			wantModel:  "provider/gpt-5.6",
			wantMapped: true,
		},
		{
			name:        "mapping cycle is rejected",
			mapping:     `{"model-a":"model-b","model-b":"model-a"}`,
			requested:   "model-a",
			wantErrText: "model_mapping_contains_cycle",
		},
		{
			name:        "invalid mapping JSON is rejected",
			mapping:     `{invalid`,
			requested:   "model-a",
			wantErrText: "unmarshal_model_mapping_failed",
		},
		{
			name:        "empty source is rejected",
			mapping:     `{"":"provider/model"}`,
			requested:   "model-a",
			wantErrText: "model_mapping_empty_source",
		},
		{
			name:        "empty target is rejected",
			mapping:     `{"model-a":""}`,
			requested:   "model-a",
			wantErrText: "model_mapping_empty_target",
		},
		{
			name:        "non string target is rejected",
			mapping:     `{"model-a":123}`,
			requested:   "model-a",
			wantErrText: "model_mapping_target_must_be_string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modelName, mapped, err := ResolveChannelModelMapping(tt.mapping, tt.requested, tt.compact)
			if tt.wantErrText != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrText)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantModel, modelName)
			require.Equal(t, tt.wantMapped, mapped)
		})
	}
}

func TestValidateChannelModelMappingRejectsCyclesAndMalformedEntries(t *testing.T) {
	for _, tt := range []struct {
		name    string
		mapping string
		wantErr string
	}{
		{name: "cycle", mapping: `{"a":"b","b":"a"}`, wantErr: "model_mapping_contains_cycle"},
		{name: "empty source", mapping: `{" ":"b"}`, wantErr: "model_mapping_empty_source"},
		{name: "empty target", mapping: `{"a":" "}`, wantErr: "model_mapping_empty_target"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateChannelModelMapping(tt.mapping)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
	require.NoError(t, ValidateChannelModelMapping(`{"a":"b","b":"provider/model"}`))
}

func TestChannelModelMappingErrorsUseSentinel(t *testing.T) {
	_, _, err := ResolveChannelModelMapping(`{"a":"b","b":"a"}`, "a", false)
	require.ErrorIs(t, err, ErrChannelModelMapping)
	require.True(t, IsChannelModelMappingError(err))
	require.Contains(t, err.Error(), "model_mapping_contains_cycle")
}

func TestPrepareChannelModelMappingRejectsCyclesBeforeCompactTerminalResolution(t *testing.T) {
	snapshot, err := PrepareChannelModelMapping(`{"model-openai-compact":"model","model":"model-openai-compact"}`)
	require.Error(t, err)
	require.Nil(t, snapshot)
	require.ErrorIs(t, err, ErrChannelModelMapping)
	require.Contains(t, err.Error(), "model_mapping_contains_cycle")
}

func TestPrepareChannelModelMappingRejectsTrimmedDuplicateSources(t *testing.T) {
	_, err := PrepareChannelModelMapping(`{"model":"provider/a"," model ":"provider/a"}`)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChannelModelMapping)
	require.Contains(t, err.Error(), "model_mapping_duplicate_source")
}

func TestPrepareChannelModelMappingRejectsExactDuplicateSources(t *testing.T) {
	_, err := PrepareChannelModelMapping(`{"model":"provider/a","model":"provider/b"}`)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChannelModelMapping)
	require.Contains(t, err.Error(), "model_mapping_duplicate_source")
}

func TestResolveChannelModelMappingForRoutingRejectsExactDuplicateSources(t *testing.T) {
	_, _, err := ResolveChannelModelMappingForRouting(`{"model":"provider/a","model":"provider/b"}`, "model", false)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChannelModelMapping)
	require.Contains(t, err.Error(), "model_mapping_duplicate_source")
}

func TestPrepareChannelModelMappingRejectsTrailingJSON(t *testing.T) {
	_, err := PrepareChannelModelMapping(`{"model":"provider/a"}{}`)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChannelModelMapping)
	require.Contains(t, err.Error(), "unmarshal_model_mapping_failed")
}

func TestPreparedChannelModelMappingReusesValidatedSnapshot(t *testing.T) {
	snapshot, err := PrepareChannelModelMapping(`{"client":"provider/base","provider/base":"provider/final"}`)
	require.NoError(t, err)

	modelName, mapped, err := snapshot.Resolve("client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/final", modelName)

	// The same snapshot serves compact and ordinary candidate checks without
	// reparsing the persisted JSON or changing the resolved chain.
	compactModel, _, err := snapshot.Resolve("provider/base", true)
	require.NoError(t, err)
	require.Equal(t, "provider/final", compactModel)
}

func TestPreparedChannelModelMappingReportsSemanticEmptySnapshots(t *testing.T) {
	for _, raw := range []string{"", " ", "{}", " { } "} {
		snapshot, err := PrepareChannelModelMapping(raw)
		require.NoError(t, err, raw)
		require.True(t, snapshot.IsEmpty(), raw)
	}

	snapshot, err := PrepareChannelModelMapping(`{"client":"provider/model"}`)
	require.NoError(t, err)
	require.False(t, snapshot.IsEmpty())
}

func TestResolveChannelModelMappingForRoutingRejectsInvalidLegacyEntries(t *testing.T) {
	for _, mapping := range []string{
		`{"client":"provider/client","legacy":""}`,
		`{"client":"provider/client","":"provider/empty-source"}`,
		`{"client":"provider/client","cycle-a":"cycle-b","cycle-b":"cycle-a"}`,
	} {
		_, _, err := ResolveChannelModelMappingForRouting(mapping, "client", false)
		require.Error(t, err, mapping)
		require.ErrorIs(t, err, ErrChannelModelMapping)
	}
}

func TestResolveChannelModelMappingForRoutingRejectsCycleOnRequestedPath(t *testing.T) {
	_, _, err := ResolveChannelModelMappingForRouting(`{"client":"cycle-a","cycle-a":"cycle-b","cycle-b":"cycle-a"}`, "client", false)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChannelModelMapping)
	require.Contains(t, err.Error(), "model_mapping_contains_cycle")
}

func TestResolveChannelModelMappingForRoutingRejectsRequestedEmptyTarget(t *testing.T) {
	_, _, err := ResolveChannelModelMappingForRouting(`{"client":"","unrelated":"provider/model"}`, "client", false)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChannelModelMapping)
	require.Contains(t, err.Error(), "model_mapping_empty_target")
}
