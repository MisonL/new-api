package openai

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesCompactionHandler(c *gin.Context, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
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
