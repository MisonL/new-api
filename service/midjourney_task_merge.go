package service

import (
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// MidjourneyTaskMergeResult describes the observable lifecycle effect of one
// provider update. TerminalFailureTransition is true only for the first
// accepted transition into FAILURE, so quota refund logic is idempotent.
type MidjourneyTaskMergeResult struct {
	Changed                   bool
	TerminalFailureTransition bool
}

// NormalizeMidjourneyTaskStatus converts the status spellings used by common
// Midjourney-compatible providers into the canonical lifecycle values stored
// by new-api. The bool is false for empty or unknown wire values.
func NormalizeMidjourneyTaskStatus(status string) (model.TaskStatus, bool) {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case string(model.TaskStatusNotStart):
		return model.TaskStatusNotStart, true
	case string(model.TaskStatusSubmitted):
		return model.TaskStatusSubmitted, true
	case string(model.TaskStatusQueued), "PENDING":
		return model.TaskStatusQueued, true
	case string(model.TaskStatusInProgress), "PROCESSING":
		return model.TaskStatusInProgress, true
	case string(model.TaskStatusSuccess):
		return model.TaskStatusSuccess, true
	case string(model.TaskStatusFailure), "FAILED":
		return model.TaskStatusFailure, true
	default:
		return "", false
	}
}

// MergeMidjourneyTask applies a provider update without allowing sparse
// responses to erase data already persisted on the task. Empty strings, zero
// timestamps, empty arrays, and empty objects are treated as absent values.
// Status and progress remain monotonic; an empty status can update non-empty
// metadata but can never infer a terminal lifecycle state.
func MergeMidjourneyTask(task *model.Midjourney, incoming dto.MidjourneyDto) (MidjourneyTaskMergeResult, error) {
	if task == nil {
		return MidjourneyTaskMergeResult{}, errors.New("midjourney task is nil")
	}

	incomingStatus := strings.ToUpper(strings.TrimSpace(incoming.Status))
	if normalizedStatus, recognized := NormalizeMidjourneyTaskStatus(incomingStatus); recognized {
		incomingStatus = string(normalizedStatus)
	}
	if incomingStatus != "" && !ShouldApplyTaskStatus(model.TaskStatus(task.Status), model.TaskStatus(incomingStatus)) {
		return MidjourneyTaskMergeResult{}, nil
	}

	var propertiesText string
	if hasMidjourneyProperties(incoming.Properties) {
		encoded, err := common.Marshal(incoming.Properties)
		if err != nil {
			return MidjourneyTaskMergeResult{}, err
		}
		propertiesText = string(encoded)
	}
	var buttonsText string
	if hasMidjourneyValue(incoming.Buttons) {
		encoded, err := common.Marshal(incoming.Buttons)
		if err != nil {
			return MidjourneyTaskMergeResult{}, err
		}
		buttonsText = string(encoded)
	}
	var videoURLsText string
	if len(incoming.VideoUrls) > 0 {
		encoded, err := common.Marshal(incoming.VideoUrls)
		if err != nil {
			return MidjourneyTaskMergeResult{}, err
		}
		videoURLsText = string(encoded)
	}

	terminal := incomingStatus == string(model.TaskStatusSuccess) || incomingStatus == string(model.TaskStatusFailure)
	nextProgress := MergeTaskProgress(task.Progress, incoming.Progress, terminal)
	previousStatus := strings.ToUpper(strings.TrimSpace(task.Status))
	hasPayload := incomingStatus != "" ||
		strings.TrimSpace(incoming.Progress) != "" ||
		strings.TrimSpace(incoming.PromptEn) != "" ||
		strings.TrimSpace(incoming.State) != "" ||
		incoming.SubmitTime != 0 ||
		incoming.StartTime != 0 ||
		incoming.FinishTime != 0 ||
		strings.TrimSpace(incoming.ImageUrl) != "" ||
		strings.TrimSpace(incoming.FailReason) != "" ||
		strings.TrimSpace(incoming.VideoUrl) != "" ||
		propertiesText != "" ||
		buttonsText != "" ||
		videoURLsText != "" ||
		terminal
	if !hasPayload {
		return MidjourneyTaskMergeResult{}, nil
	}

	changed := task.Code != midjourneyTaskCodeSubmitted || nextProgress != task.Progress
	if incomingStatus != "" && incomingStatus != previousStatus {
		changed = true
	}
	if strings.TrimSpace(incoming.PromptEn) != "" && task.PromptEn != incoming.PromptEn {
		changed = true
	}
	if strings.TrimSpace(incoming.State) != "" && task.State != incoming.State {
		changed = true
	}
	if incoming.SubmitTime != 0 && task.SubmitTime != incoming.SubmitTime {
		changed = true
	}
	if incoming.StartTime != 0 && task.StartTime != incoming.StartTime {
		changed = true
	}
	if incoming.FinishTime != 0 && task.FinishTime != incoming.FinishTime {
		changed = true
	}
	if strings.TrimSpace(incoming.ImageUrl) != "" && task.ImageUrl != incoming.ImageUrl {
		changed = true
	}
	if strings.TrimSpace(incoming.FailReason) != "" && task.FailReason != incoming.FailReason {
		changed = true
	}
	if strings.TrimSpace(incoming.VideoUrl) != "" && task.VideoUrl != incoming.VideoUrl {
		changed = true
	}
	if propertiesText != "" && task.Properties != propertiesText {
		changed = true
	}
	if buttonsText != "" && task.Buttons != buttonsText {
		changed = true
	}
	if videoURLsText != "" && task.VideoUrls != videoURLsText {
		changed = true
	}
	if terminal && task.FinishTime == 0 {
		changed = true
	}
	if terminal && incomingStatus == string(model.TaskStatusFailure) && strings.TrimSpace(incoming.FailReason) == "" && strings.TrimSpace(task.FailReason) == "" {
		changed = true
	}
	if !changed {
		return MidjourneyTaskMergeResult{}, nil
	}

	if task.Code != midjourneyTaskCodeSubmitted {
		task.Code = midjourneyTaskCodeSubmitted
	}
	if nextProgress != task.Progress {
		task.Progress = nextProgress
	}
	if task.Progress == "" {
		task.Progress = "0%"
	}
	if strings.TrimSpace(incoming.PromptEn) != "" {
		task.PromptEn = incoming.PromptEn
	}
	if strings.TrimSpace(incoming.State) != "" {
		task.State = incoming.State
	}
	if incoming.SubmitTime != 0 {
		task.SubmitTime = incoming.SubmitTime
	}
	if incoming.StartTime != 0 {
		task.StartTime = incoming.StartTime
	}
	if incoming.FinishTime != 0 {
		task.FinishTime = incoming.FinishTime
	}
	if strings.TrimSpace(incoming.ImageUrl) != "" {
		task.ImageUrl = incoming.ImageUrl
	}
	if incomingStatus != "" {
		task.Status = incomingStatus
	}
	if strings.TrimSpace(incoming.FailReason) != "" {
		task.FailReason = incoming.FailReason
	}
	if propertiesText != "" {
		task.Properties = propertiesText
	}
	if buttonsText != "" {
		task.Buttons = buttonsText
	}
	if strings.TrimSpace(incoming.VideoUrl) != "" {
		task.VideoUrl = incoming.VideoUrl
	}
	if videoURLsText != "" {
		task.VideoUrls = videoURLsText
	}
	if terminal {
		task.Progress = "100%"
		if task.FinishTime == 0 {
			task.FinishTime = time.Now().UnixNano() / int64(time.Millisecond)
		}
		if incomingStatus == string(model.TaskStatusFailure) && strings.TrimSpace(task.FailReason) == "" {
			task.FailReason = "Midjourney upstream task failed"
		}
	}

	return MidjourneyTaskMergeResult{
		Changed:                   true,
		TerminalFailureTransition: incomingStatus == string(model.TaskStatusFailure) && previousStatus != string(model.TaskStatusFailure),
	}, nil
}

func hasMidjourneyProperties(properties *dto.Properties) bool {
	return properties != nil && (strings.TrimSpace(properties.FinalPrompt) != "" || strings.TrimSpace(properties.FinalZhPrompt) != "")
}

func hasMidjourneyValue(value any) bool {
	if value == nil {
		return false
	}
	reflected := reflect.ValueOf(value)
	for reflected.IsValid() && (reflected.Kind() == reflect.Interface || reflected.Kind() == reflect.Pointer) {
		if reflected.IsNil() {
			return false
		}
		reflected = reflected.Elem()
	}
	if !reflected.IsValid() {
		return false
	}
	switch reflected.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice:
		return reflected.Len() > 0
	case reflect.String:
		return strings.TrimSpace(reflected.String()) != ""
	default:
		return true
	}
}
