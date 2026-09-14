package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelMappedHelperPrefersChannelModelMappingContext(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set("model_mapping", `{"model-alpha":"legacy-model"}`)
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"model-alpha":"context-model"}`)

	info := &relaycommon.RelayInfo{
		OriginModelName: "model-alpha",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "model-alpha"},
	}
	req := &dto.GeneralOpenAIRequest{Model: "model-alpha"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}
	if info.UpstreamModelName != "context-model" || req.Model != "context-model" {
		t.Fatalf("expected channel context mapping to win, info=%q request=%q", info.UpstreamModelName, req.Model)
	}
}

func TestModelMappedHelperAppliesViduReferenceWireModelForSpecificChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	mapping := `{"client-vidu":"mapped-vidu"}`
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, mapping)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeVidu)
	common.SetContextKey(c, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntent{
		Action: constant.TaskActionReferenceGenerate,
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "client-vidu",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   704,
			ChannelType: constant.ChannelTypeVidu,
		},
	}

	require.NoError(t, ModelMappedHelper(c, info, nil))
	require.Equal(t, "viduq2", info.UpstreamModelName)
	require.True(t, info.IsModelMapped)
}

func TestModelMappedHelperReusesSelectedRouteDiagnostics(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	// The persisted mapping is intentionally malformed. A selector that already
	// resolved this candidate must remain authoritative for the current request;
	// reparsing the mutable channel field here would turn a valid selection into
	// a late 504-style mapping failure.
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, "{")
	common.SetContextKey(c, constant.ContextKeyChannelRouteDiagnostics, dto.ChannelRouteSelectionDiagnostics{
		RequestedModel:      "model-alpha",
		EffectiveRouteModel: "provider/model-alpha",
		SelectedChannelID:   701,
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "model-alpha",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 701,
		},
	}
	req := &dto.GeneralOpenAIRequest{Model: "model-alpha"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}
	if info.UpstreamModelName != "provider/model-alpha" || req.Model != "provider/model-alpha" {
		t.Fatalf("expected selected route mapping to be reused, info=%q request=%q", info.UpstreamModelName, req.Model)
	}
}

func TestModelMappedHelperUsesSelectedCandidateEffectiveModelForHealthRoute(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	// The same physical channel can expose compact and base candidates. The
	// selector's chosen candidate is authoritative; helper mapping must not
	// reconstruct the first effective model from channel-wide state.
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"route-model-openai-compact":"provider/compact","route-model":"provider/base"}`)
	common.SetContextKey(c, constant.ContextKeyChannelRouteDiagnostics, dto.ChannelRouteSelectionDiagnostics{
		Group:                  "default",
		RequestedModel:         "route-model-openai-compact",
		SelectedCandidateModel: "route-model",
		EffectiveRouteModel:    "provider/base",
		SelectedChannelID:      702,
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "route-model-openai-compact",
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 702,
		},
	}
	req := &dto.OpenAIResponsesRequest{Model: "route-model-openai-compact"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}
	require.Equal(t, "provider/base", info.UpstreamModelName)
	require.Equal(t, "provider/base", req.Model)
	require.Equal(t, "provider/base", info.ChannelHealthRoute.EffectiveUpstreamModel)
}

func TestModelMappedHelperDoesNotReadLiveChannelMappingWhenContextSnapshotMissing(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-alpha",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 703},
	}

	err := ModelMappedHelper(c, info, nil)
	require.ErrorContains(t, err, "channel model mapping snapshot missing")
}

func TestModelMappedHelperResponsesCompactUsesFullCompactMappingKey(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set("model_mapping", `{"model-alpha-openai-compact":"model-alpha"}`)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "model-alpha-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "model-alpha-openai-compact",
		},
	}
	req := &dto.OpenAIResponsesRequest{Model: "model-alpha-openai-compact"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}

	if !info.IsModelMapped {
		t.Fatal("expected compact mapping to be marked as mapped")
	}
	if info.UpstreamModelName != "model-alpha" {
		t.Fatalf("expected upstream model model-alpha, got %q", info.UpstreamModelName)
	}
	if req.Model != "model-alpha" {
		t.Fatalf("expected request model model-alpha, got %q", req.Model)
	}
	if info.OriginModelName != "model-alpha-openai-compact" {
		t.Fatalf("expected compact origin model to be preserved, got %q", info.OriginModelName)
	}
}

func TestModelMappedHelperResponsesCompactPreservesOriginWhenMappedToNamespacedUpstream(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set("model_mapping", `{"model-alpha-openai-compact":"provider/model-alpha"}`)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "model-alpha-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "model-alpha-openai-compact",
		},
	}
	req := &dto.OpenAIResponsesRequest{Model: "model-alpha-openai-compact"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}

	if info.UpstreamModelName != "provider/model-alpha" {
		t.Fatalf("expected upstream model provider/model-alpha, got %q", info.UpstreamModelName)
	}
	if req.Model != "provider/model-alpha" {
		t.Fatalf("expected request model provider/model-alpha, got %q", req.Model)
	}
	if info.OriginModelName != "model-alpha-openai-compact" {
		t.Fatalf("expected compact origin model to stay routable, got %q", info.OriginModelName)
	}
}

func TestModelMappedHelperResponsesCompactFullMappingDoesNotChainThroughBaseMapping(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set("model_mapping", `{"model-alpha":"provider/model-alpha","model-alpha-openai-compact":"model-alpha"}`)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "model-alpha-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "model-alpha-openai-compact",
		},
	}
	req := &dto.OpenAIResponsesRequest{Model: "model-alpha-openai-compact"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}

	if info.UpstreamModelName != "model-alpha" {
		t.Fatalf("expected explicit compact mapping to stop at model-alpha, got %q", info.UpstreamModelName)
	}
	if req.Model != "model-alpha" {
		t.Fatalf("expected request model model-alpha, got %q", req.Model)
	}
	if info.OriginModelName != "model-alpha-openai-compact" {
		t.Fatalf("expected compact origin model to stay routable, got %q", info.OriginModelName)
	}
}

func TestModelMappedHelperResponsesCompactDefaultsToBaseModel(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "model-alpha-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "model-alpha-openai-compact",
		},
	}
	req := &dto.OpenAIResponsesRequest{Model: "model-alpha-openai-compact"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}

	if info.IsModelMapped {
		t.Fatal("expected default compact suffix stripping not to be marked as custom mapping")
	}
	if info.UpstreamModelName != "model-alpha" {
		t.Fatalf("expected upstream model model-alpha, got %q", info.UpstreamModelName)
	}
	if req.Model != "model-alpha" {
		t.Fatalf("expected request model model-alpha, got %q", req.Model)
	}
}
