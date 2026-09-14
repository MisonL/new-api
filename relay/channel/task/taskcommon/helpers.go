package taskcommon

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// UnmarshalMetadata converts a map[string]any metadata to a typed struct via JSON round-trip.
// This replaces the repeated pattern: json.Marshal(metadata) -> json.Unmarshal(bytes, &target).
func UnmarshalMetadata(metadata map[string]any, target any) error {
	if metadata == nil {
		return nil
	}
	// Prevent metadata from overriding routing/model identity fields to avoid a
	// mapped request being billed or sent as a different upstream model. Build a
	// shallow copy instead of deleting from the caller's map: the same metadata
	// is also used by validation, billing and provider-specific action logic.
	filtered := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if isProtectedMetadataIdentityKey(key) {
			continue
		}
		filtered[key] = value
	}
	metaBytes, err := common.Marshal(filtered)
	if err != nil {
		return fmt.Errorf("marshal metadata failed: %w", err)
	}
	if err := common.Unmarshal(metaBytes, target); err != nil {
		return fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	return nil
}

func isProtectedMetadataIdentityKey(key string) bool {
	// JSON decoding matches tagged fields case-insensitively, while clients may
	// spell the same identity using camelCase, dashes or underscores. Normalize
	// separators before comparing so those variants cannot bypass the guard.
	var normalized strings.Builder
	normalized.Grow(len(key))
	for _, r := range strings.ToLower(strings.TrimSpace(key)) {
		switch r {
		case '_', '-', ' ', '\t', '\r', '\n':
			continue
		default:
			normalized.WriteRune(r)
		}
	}
	switch normalized.String() {
	case "model", "modelname", "reqkey", "upstreammodel", "upstreammodelname", "originmodel", "originmodelname":
		return true
	default:
		return false
	}
}

// DefaultString returns val if non-empty, otherwise fallback.
func DefaultString(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

// DefaultInt returns val if non-zero, otherwise fallback.
func DefaultInt(val, fallback int) int {
	if val == 0 {
		return fallback
	}
	return val
}

// EncodeLocalTaskID encodes an upstream operation name to a URL-safe base64 string.
// Used by Gemini/Vertex to store upstream names as task IDs.
func EncodeLocalTaskID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// DecodeLocalTaskID decodes a base64-encoded upstream operation name.
func DecodeLocalTaskID(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ApplyExtraHeaders replaces matching request headers with extra headers and updates Host when provided.
func ApplyExtraHeaders(request *http.Request, extraHeaders ...http.Header) {
	if request == nil {
		return
	}
	if request.Header == nil {
		request.Header = http.Header{}
	}
	for _, headers := range extraHeaders {
		for key, values := range headers {
			if len(values) == 0 {
				continue
			}
			request.Header.Del(key)
			for _, value := range values {
				request.Header.Add(key, value)
			}
			if strings.EqualFold(key, "Host") {
				request.Host = values[0]
			}
		}
	}
}

// CapturedTaskPollingPath returns the route path captured when an asynchronous
// task was submitted. The path is supplied through the internal polling body so
// an adaptor cannot accidentally reconstruct a follow-up URL from mutable
// channel settings. A missing path keeps the explicit legacy fallback used by
// rows created before route snapshots were introduced.
func CapturedTaskPollingPath(body map[string]any, fallback string) string {
	if body != nil {
		if captured, ok := body["polling_path"].(string); ok {
			// `/` is a valid captured native endpoint (Jimeng access-key
			// mode). Only a missing or blank field means that the row predates
			// route snapshots and needs the legacy fallback.
			if strings.TrimSpace(captured) != "" {
				return relaycommon.NormalizeRoutePath(captured)
			}
		}
	}
	return relaycommon.NormalizeRoutePath(fallback)
}

// BuildProxyURL constructs the video proxy URL using the public task ID.
// e.g., "https://your-server.com/v1/videos/task_xxxx/content"
func BuildProxyURL(taskID string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	return fmt.Sprintf("%s/v1/videos/%s/content", baseURL, url.PathEscape(taskID))
}

// Status-to-progress mapping constants for polling updates.
const (
	ProgressSubmitted  = "10%"
	ProgressQueued     = "20%"
	ProgressInProgress = "30%"
	ProgressComplete   = "100%"
)

// ---------------------------------------------------------------------------
// BaseBilling - embeddable no-op implementations for TaskAdaptor billing methods.
// Adaptors that do not need custom billing can embed this struct directly.
// ---------------------------------------------------------------------------

type BaseBilling struct{}

// EstimateBilling returns nil (no extra ratios; use base model price).
func (BaseBilling) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

// AdjustBillingOnSubmit returns nil (no submit-time adjustment).
func (BaseBilling) AdjustBillingOnSubmit(_ *relaycommon.RelayInfo, _ []byte) map[string]float64 {
	return nil
}

// AdjustBillingOnComplete returns 0 (keep pre-charged amount).
func (BaseBilling) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}
