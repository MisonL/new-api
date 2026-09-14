package helper

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// ErrOpaqueResponsesTieredBillingUnsupported is returned before evaluating a
// tiered billing expression for an opaque encrypted Responses replay. The
// request body is intentionally kept on disk and cannot be decoded or
// re-marshaled safely, so expressions that inspect body parameters must not
// run against an incomplete input snapshot.
var ErrOpaqueResponsesTieredBillingUnsupported = errors.New("tiered expression billing is unsupported for opaque encrypted Responses replay")

func ResolveIncomingBillingExprRequestInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	if info != nil && relaycommon.IsOpaqueResponsesReplayRequest(info.Request) {
		return billingexpr.RequestInput{}, ErrOpaqueResponsesTieredBillingUnsupported
	}
	if metadata, ok := common.GetResponsesRequestMetadata(c); ok && metadata.HasOpaqueEncryptedContent {
		return billingexpr.RequestInput{}, ErrOpaqueResponsesTieredBillingUnsupported
	}
	if info != nil && info.BillingRequestInput != nil {
		input := cloneRequestInput(*info.BillingRequestInput)
		baseHeaders, err := billingexpr.NormalizeHeaders(info.RequestHeaders)
		if err != nil {
			return billingexpr.RequestInput{}, err
		}
		overrideHeaders, err := billingexpr.NormalizeHeaders(input.Headers)
		if err != nil {
			return billingexpr.RequestInput{}, err
		}
		for k, v := range overrideHeaders {
			baseHeaders[k] = v
		}
		input.Headers = baseHeaders
		return input, nil
	}

	input := billingexpr.RequestInput{}
	if info != nil {
		var err error
		input.Headers, err = billingexpr.NormalizeHeaders(info.RequestHeaders)
		if err != nil {
			return billingexpr.RequestInput{}, err
		}
	}

	bodyBytes, err := readIncomingBillingExprBody(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	input.Body = bodyBytes
	return input, nil
}

func BuildBillingExprRequestInputFromRequest(request dto.Request, headers map[string]string) (billingexpr.RequestInput, error) {
	input := billingexpr.RequestInput{
		Headers: cloneStringMap(headers),
	}
	if request == nil {
		return input, nil
	}
	if relaycommon.IsOpaqueResponsesReplayRequest(request) {
		return billingexpr.RequestInput{}, ErrOpaqueResponsesTieredBillingUnsupported
	}

	bodyBytes, err := common.Marshal(request)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	input.Body = bodyBytes
	return input, nil
}

func readIncomingBillingExprBody(c *gin.Context) ([]byte, error) {
	if c == nil || c.Request == nil || !isJSONContentType(c.Request.Header.Get("Content-Type")) {
		return nil, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

func cloneRequestInput(src billingexpr.RequestInput) billingexpr.RequestInput {
	input := billingexpr.RequestInput{
		Headers: cloneStringMap(src.Headers),
	}
	if len(src.Body) > 0 {
		input.Body = append([]byte(nil), src.Body...)
	}
	return input
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "application/json")
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		if strings.TrimSpace(key) == "" {
			continue
		}
		dst[key] = value
	}
	return dst
}
