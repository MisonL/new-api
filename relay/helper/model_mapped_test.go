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
	c.Set("model_mapping", `{"gpt-5.5-openai-compact":"gpt-5.5"}`)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5-openai-compact",
		},
	}
	req := &dto.OpenAIResponsesRequest{Model: "gpt-5.5-openai-compact"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}

	if !info.IsModelMapped {
		t.Fatal("expected compact mapping to be marked as mapped")
	}
	if info.UpstreamModelName != "gpt-5.5" {
		t.Fatalf("expected upstream model gpt-5.5, got %q", info.UpstreamModelName)
	}
	if req.Model != "gpt-5.5" {
		t.Fatalf("expected request model gpt-5.5, got %q", req.Model)
	}
	if info.OriginModelName != "gpt-5.5-openai-compact" {
		t.Fatalf("expected compact origin model to be preserved, got %q", info.OriginModelName)
	}
}

func TestModelMappedHelperResponsesCompactPreservesOriginWhenMappedToNamespacedUpstream(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set("model_mapping", `{"gpt-5.5-openai-compact":"openai/gpt-5.5"}`)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5-openai-compact",
		},
	}
	req := &dto.OpenAIResponsesRequest{Model: "gpt-5.5-openai-compact"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}

	if info.UpstreamModelName != "openai/gpt-5.5" {
		t.Fatalf("expected upstream model openai/gpt-5.5, got %q", info.UpstreamModelName)
	}
	if req.Model != "openai/gpt-5.5" {
		t.Fatalf("expected request model openai/gpt-5.5, got %q", req.Model)
	}
	if info.OriginModelName != "gpt-5.5-openai-compact" {
		t.Fatalf("expected compact origin model to stay routable, got %q", info.OriginModelName)
	}
}

func TestModelMappedHelperResponsesCompactDefaultsToBaseModel(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5-openai-compact",
		},
	}
	req := &dto.OpenAIResponsesRequest{Model: "gpt-5.5-openai-compact"}

	if err := ModelMappedHelper(c, info, req); err != nil {
		t.Fatalf("ModelMappedHelper returned error: %v", err)
	}

	if info.IsModelMapped {
		t.Fatal("expected default compact suffix stripping not to be marked as custom mapping")
	}
	if info.UpstreamModelName != "gpt-5.5" {
		t.Fatalf("expected upstream model gpt-5.5, got %q", info.UpstreamModelName)
	}
	if req.Model != "gpt-5.5" {
		t.Fatalf("expected request model gpt-5.5, got %q", req.Model)
	}
}
