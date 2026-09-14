package helper

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ModelMappedHelper(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}

	isResponsesCompact := info.RelayMode == relayconstant.RelayModeResponsesCompact
	modelMapping := common.GetContextKeyString(c, constant.ContextKeyChannelModelMapping)
	if modelMapping == "" {
		// Keep compatibility with direct helper callers that still populate the
		// legacy Gin string explicitly instead of the typed channel context key.
		modelMapping = c.GetString("model_mapping")
	}
	channelID := info.ChannelMeta.ChannelId
	finalUpstreamModelName := ""
	mapped := false
	// Selector diagnostics are the strongest request-scoped source of truth. Use
	// them before consulting the channel table so a mapping edit made while an
	// in-flight request is being retried cannot replace the route chosen at
	// dispatch time.
	if diagnostics, ok := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](c, constant.ContextKeyChannelRouteDiagnostics); ok &&
		diagnostics.SelectedChannelID == channelID &&
		strings.TrimSpace(diagnostics.RequestedModel) == strings.TrimSpace(info.OriginModelName) &&
		strings.TrimSpace(diagnostics.EffectiveRouteModel) != "" {
		finalUpstreamModelName = strings.TrimSpace(diagnostics.EffectiveRouteModel)
		mapped = finalUpstreamModelName != strings.TrimSpace(info.OriginModelName)
	}
	if finalUpstreamModelName == "" && channelID > 0 {
		// A selected channel must carry either selector diagnostics or an
		// explicitly installed mapping snapshot. Re-reading channel.model_mapping
		// here would reopen a dispatch-to-submit race in which an administrator's
		// edit changes the provider model after health and billing were bound.
		if strings.TrimSpace(modelMapping) == "" {
			if _, present := common.GetContextKey(c, constant.ContextKeyChannelModelMapping); !present {
				return fmt.Errorf("channel model mapping snapshot missing: channel_id=%d", channelID)
			}
		}
	}
	if finalUpstreamModelName == "" {
		var err error
		// Direct/specific-channel callers may not have selector diagnostics, but
		// they still pass through SetupContextForSelectedChannel. Reuse the same
		// request-scoped resolver with the installed context mapping instead of
		// reparsing a mutable string independently in the relay helper.
		channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
		if channelType == 0 && info.ChannelMeta != nil {
			channelType = info.ChannelMeta.ChannelType
		}
		mappingChannel := &model.Channel{Id: channelID, Type: channelType}
		if strings.TrimSpace(modelMapping) != "" {
			mappingChannel.ModelMapping = common.GetPointer(modelMapping)
		}
		finalUpstreamModelName, mapped, err = service.ResolveChannelModelMappingForContext(
			c,
			mappingChannel,
			info.OriginModelName,
			isResponsesCompact,
		)
		if err != nil {
			return fmt.Errorf("resolve channel model mapping: %w", err)
		}
	}
	if strings.TrimSpace(finalUpstreamModelName) == "" {
		return fmt.Errorf("resolved channel model is empty: channel_id=%d model=%q", channelID, info.OriginModelName)
	}
	info.IsModelMapped = mapped
	info.UpstreamModelName = finalUpstreamModelName
	info.ChannelMeta.UpstreamModelName = finalUpstreamModelName
	// OriginModelName is the client/routing model and is immutable for the
	// whole request, including Responses compact retries.
	service.BindChannelHealthRoute(c, info, finalUpstreamModelName)
	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}
