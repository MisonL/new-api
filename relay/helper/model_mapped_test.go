package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
)

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
