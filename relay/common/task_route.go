package common

import (
	"strings"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

// TaskRouteIntent is the request-level operation known before a channel is
// selected. It is intentionally small and credential-free: the distributor
// can infer it from the public task payload, while the selected provider can
// apply its final wire-model rule without reparsing the request later.
type TaskRouteIntent struct {
	Action         string
	ExplicitAction bool
}

// TaskRouteIntentFromRequest derives the operation used by provider-specific
// task routing rules. An explicit metadata action keeps the adaptor's current
// precedence semantics. When it is absent, image count supplies the Vidu
// action classification; other providers simply ignore this intent.
func TaskRouteIntentFromRequest(req TaskSubmitReq) TaskRouteIntent {
	if action, explicit := taskRouteActionFromMetadata(req.Metadata); explicit {
		return TaskRouteIntent{Action: action, ExplicitAction: true}
	}
	imageCount := nonEmptyTaskRouteStringCount(req.Images)
	if imageCount == 0 {
		if strings.TrimSpace(req.Image) != "" {
			imageCount = 1
		} else if strings.TrimSpace(req.InputReference) != "" {
			imageCount = 1
		}
	}
	return taskRouteIntentForImageCount(imageCount)
}

// TaskRouteIntentFromPayload derives the same operation from a tolerant JSON
// object. The distributor runs before a provider adaptor is known, so it must
// accept provider-specific image shapes such as Agnes' object-valued image
// field instead of decoding directly into TaskSubmitReq and rejecting a valid
// request. multipartImageFileCount contains only image input files; filenames
// and file contents are deliberately not copied into the route context.
func TaskRouteIntentFromPayload(payload map[string]any, multipartImageFileCount int) TaskRouteIntent {
	if payload == nil {
		return taskRouteIntentForImageCount(maxTaskRouteImageCount(multipartImageFileCount))
	}
	if metadata, ok := payload["metadata"]; ok {
		if action, explicit := taskRouteActionFromMetadata(metadata); explicit {
			return TaskRouteIntent{Action: action, ExplicitAction: true}
		}
	}
	// Multipart form parsing stores unknown top-level fields (including
	// `action`) in TaskSubmitReq.Metadata. Treat the same field as explicit here
	// so the pre-selection route sees exactly the action the task adaptor will
	// later consume. JSON callers are normalized the same way by
	// TaskSubmitReq.UnmarshalJSON.
	if action, explicit := taskRouteActionValue(payload["action"]); explicit {
		return TaskRouteIntent{Action: action, ExplicitAction: true}
	}

	imageCount := taskRoutePayloadImageCount(payload)
	imageCount += maxTaskRouteImageCount(multipartImageFileCount)
	return taskRouteIntentForImageCount(imageCount)
}

func taskRouteIntentForImageCount(imageCount int) TaskRouteIntent {
	switch {
	case imageCount > 2:
		return TaskRouteIntent{Action: constant.TaskActionReferenceGenerate}
	case imageCount == 2:
		return TaskRouteIntent{Action: constant.TaskActionFirstTailGenerate}
	case imageCount > 0:
		return TaskRouteIntent{Action: constant.TaskActionGenerate}
	default:
		return TaskRouteIntent{Action: constant.TaskActionTextGenerate}
	}
}

func nonEmptyTaskRouteStringCount(values []string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func maxTaskRouteImageCount(count int) int {
	if count < 0 {
		return 0
	}
	return count
}

func taskRouteActionFromMetadata(value any) (string, bool) {
	switch metadata := value.(type) {
	case map[string]any:
		return taskRouteActionValue(metadata["action"])
	case string:
		if strings.TrimSpace(metadata) == "" {
			return "", false
		}
		var decoded map[string]any
		if err := appcommon.Unmarshal([]byte(metadata), &decoded); err != nil {
			return "", false
		}
		return taskRouteActionValue(decoded["action"])
	default:
		return "", false
	}
}

func taskRouteActionValue(value any) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

func taskRoutePayloadImageCount(payload map[string]any) int {
	count := 0
	for _, key := range []string{"images", "image", "input_reference", "first_frame", "last_frame"} {
		count += taskRouteImageValueCount(payload[key])
	}
	if extraBody, ok := payload["extra_body"].(map[string]any); ok {
		count += taskRoutePayloadImageCount(extraBody)
	}
	return count
}

func taskRouteImageValueCount(value any) int {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0
		}
		return 1
	case []string:
		return nonEmptyTaskRouteStringCount(typed)
	case []any:
		count := 0
		for _, item := range typed {
			count += taskRouteImageValueCount(item)
		}
		return count
	case map[string]any:
		// An object is one image input even when it contains several descriptive
		// fields. A nested images array is the one shape where the number of
		// elements carries route meaning.
		if images, ok := typed["images"]; ok {
			if count := taskRouteImageValueCount(images); count > 0 {
				return count
			}
		}
		for _, key := range []string{"image_url", "url", "data", "b64_json", "file_id", "bytes_base64_encoded"} {
			if taskRouteImageValueCount(typed[key]) > 0 {
				return 1
			}
		}
		return 0
	default:
		return 0
	}
}

// ResolveTaskWireModel applies the final provider/action model rule after the
// channel's ordinary model_mapping has been resolved. Keeping this function
// pure lets selection, health, snapshots and adaptors use exactly the same
// result, while leaving non-Vidu routes untouched.
func ResolveTaskWireModel(channelType int, action string, effectiveModel string) string {
	effectiveModel = strings.TrimSpace(effectiveModel)
	if channelType == constant.ChannelTypeVidu && strings.EqualFold(strings.TrimSpace(action), constant.TaskActionReferenceGenerate) {
		return "viduq2"
	}
	return effectiveModel
}
