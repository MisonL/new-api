package openai

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesCompactionHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var compactResp dto.OpenAIResponsesCompactionResponse
	if err := common.Unmarshal(responseBody, &compactResp); err != nil {
		return nil, types.NewOpenAIError(
			fmt.Errorf("provider returned malformed compact output: invalid response body: %w", err),
			types.ErrorCodeBadResponseBody,
			http.StatusBadGateway,
		)
	}
	if oaiError := compactResp.GetOpenAIError(); responsesCompactOpenAIErrorHasContent(oaiError) {
		return nil, types.WithOpenAIError(*oaiError, responsesCompactOpenAIErrorStatus(resp.StatusCode, oaiError))
	}
	if err := validateResponsesCompactionOutput(compactResp.Output); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	opaqueContent := responsesCompactionOutputEncryptedContent(compactResp.Output)
	responseBody, err = normalizeResponsesCompactionResponseBody(&compactResp, responseBody)
	if err != nil {
		return nil, types.NewOpenAIError(
			fmt.Errorf("provider returned malformed compact output: normalize response body: %w", err),
			types.ErrorCodeBadResponseBody,
			http.StatusBadGateway,
		)
	}
	if err := recordNativeOpaqueCompactionState(c, info, compactResp, opaqueContent); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeUpdateDataError, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	usage := dto.Usage{}
	if compactResp.Usage != nil {
		usage.PromptTokens = compactResp.Usage.InputTokens
		usage.CompletionTokens = compactResp.Usage.OutputTokens
		usage.TotalTokens = compactResp.Usage.TotalTokens
		if compactResp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = compactResp.Usage.InputTokensDetails.CachedTokens
		}
	}

	return &usage, nil
}

func recordNativeOpaqueCompactionState(c *gin.Context, info *relaycommon.RelayInfo, compactResp dto.OpenAIResponsesCompactionResponse, opaqueContent string) error {
	if strings.TrimSpace(opaqueContent) == "" {
		return nil
	}
	scope := service.SyntheticCompactScopeFromSource(info)
	if scope.UserID == 0 || scope.TokenID == 0 || strings.TrimSpace(scope.Group) == "" {
		return nil
	}
	modelName := ""
	if info != nil {
		modelName = info.OriginModelName
	}
	state, err := service.StoreNativeOpaqueCompactState(
		relaycommon.GinRequestContext(c),
		scope,
		modelName,
		compactResp.ID,
		opaqueContent,
		int64(compactResp.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("store native opaque compact state: %w", err)
	}
	service.SetSyntheticCompactApplyInfo(c, service.SyntheticCompactApplyInfo{
		StateLookup:   "recorded",
		MarkerKind:    model.SyntheticCompactStateKindNativeOpaque,
		ScopeResult:   "strict",
		RouteDecision: "native_opaque_recorded",
		StateHash:     strings.TrimSpace(state.StateHash),
	})
	common.SetContextKey(c, constant.ContextKeyResponsesCompactionOutput, true)
	return nil
}

func normalizeResponsesCompactionResponseBody(compactResp *dto.OpenAIResponsesCompactionResponse, responseBody []byte) ([]byte, error) {
	var root map[string]common.RawMessage
	if err := common.Unmarshal(responseBody, &root); err != nil {
		return nil, err
	}

	changed := false
	if compactResp != nil && compactResp.Object == "response.compaction" {
		object, err := common.Marshal("response")
		if err != nil {
			return nil, err
		}
		root["object"] = object
		compactResp.Object = "response"
		changed = true
	}

	normalizedOutput, outputChanged, err := normalizeResponsesCompactionOutput(compactResp.Output)
	if err != nil {
		return nil, err
	}
	if outputChanged {
		root["output"] = normalizedOutput
		if compactResp != nil {
			compactResp.Output = normalizedOutput
		}
		changed = true
	}
	if !changed {
		return responseBody, nil
	}
	return common.Marshal(root)
}

func normalizeResponsesCompactionOutput(output common.RawMessage) (common.RawMessage, bool, error) {
	var items []common.RawMessage
	if err := common.Unmarshal(output, &items); err != nil {
		return nil, false, err
	}
	changed := false
	for i, rawItem := range items {
		var item map[string]common.RawMessage
		if err := common.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		if responsesCompactionOutputItemType(item) != "compaction_summary" {
			continue
		}
		itemType, err := common.Marshal("compaction")
		if err != nil {
			return nil, false, err
		}
		item["type"] = itemType
		normalizedItem, err := common.Marshal(item)
		if err != nil {
			return nil, false, err
		}
		items[i] = normalizedItem
		changed = true
	}
	if !changed {
		return output, false, nil
	}
	normalizedOutput, err := common.Marshal(items)
	if err != nil {
		return nil, false, err
	}
	return normalizedOutput, true, nil
}

func validateResponsesCompactionOutput(output common.RawMessage) error {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return errors.New("provider returned malformed compact output: missing output")
	}
	if common.GetJsonType(trimmed) != "array" {
		return errors.New("provider returned malformed compact output: output is not an array")
	}
	var items []common.RawMessage
	if err := common.Unmarshal(trimmed, &items); err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("provider returned malformed compact output: output is empty")
	}
	hasCompaction := false
	for _, rawItem := range items {
		var item map[string]common.RawMessage
		if err := common.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		if !relaycommon.IsResponsesCompactionItemType(responsesCompactionOutputItemType(item)) {
			continue
		}
		hasCompaction = true
		if responsesCompactionOutputHasEncryptedContent(item) {
			return nil
		}
	}
	if hasCompaction {
		return errors.New("provider returned malformed compact output: compaction output has no encrypted content")
	}
	return errors.New("provider returned malformed compact output: no compaction output")
}

func responsesCompactionOutputEncryptedContent(output common.RawMessage) string {
	var items []common.RawMessage
	if err := common.Unmarshal(bytes.TrimSpace(output), &items); err != nil {
		return ""
	}
	for _, rawItem := range items {
		var item map[string]common.RawMessage
		if err := common.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		if !relaycommon.IsResponsesCompactionItemType(responsesCompactionOutputItemType(item)) {
			continue
		}
		raw := bytes.TrimSpace(item["encrypted_content"])
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		var encryptedContent string
		if err := common.Unmarshal(raw, &encryptedContent); err == nil {
			if strings.TrimSpace(encryptedContent) != "" {
				return encryptedContent
			}
		}
	}
	return ""
}

func responsesCompactionOutputItemType(item map[string]common.RawMessage) string {
	rawType := item["type"]
	if len(rawType) == 0 {
		return ""
	}
	var itemType string
	if err := common.Unmarshal(rawType, &itemType); err != nil {
		return ""
	}
	return itemType
}

func responsesCompactionOutputHasEncryptedContent(item map[string]common.RawMessage) bool {
	raw := bytes.TrimSpace(item["encrypted_content"])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false
	}
	var encryptedContent string
	if err := common.Unmarshal(raw, &encryptedContent); err == nil {
		return strings.TrimSpace(encryptedContent) != ""
	}
	return false
}
