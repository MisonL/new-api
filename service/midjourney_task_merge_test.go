package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestNormalizeMidjourneyTaskStatusAcceptsProviderAliases(t *testing.T) {
	tests := map[string]model.TaskStatus{
		"not_start":  model.TaskStatusNotStart,
		"submitted":  model.TaskStatusSubmitted,
		"pending":    model.TaskStatusQueued,
		"processing": model.TaskStatusInProgress,
		"success":    model.TaskStatusSuccess,
		"failed":     model.TaskStatusFailure,
	}
	for wire, want := range tests {
		t.Run(wire, func(t *testing.T) {
			got, recognized := NormalizeMidjourneyTaskStatus(wire)
			require.True(t, recognized)
			require.Equal(t, want, got)
		})
	}
	_, recognized := NormalizeMidjourneyTaskStatus("unknown")
	require.False(t, recognized)
}

func TestMergeMidjourneyTaskNormalizesProviderStatusAliases(t *testing.T) {
	tests := map[string]model.TaskStatus{
		"PENDING":    model.TaskStatusQueued,
		"PROCESSING": model.TaskStatusInProgress,
		"FAILED":     model.TaskStatusFailure,
	}
	for wire, want := range tests {
		t.Run(wire, func(t *testing.T) {
			task := &model.Midjourney{Status: string(model.TaskStatusNotStart), Progress: "0%"}
			result, err := MergeMidjourneyTask(task, dto.MidjourneyDto{Status: wire})
			require.NoError(t, err)
			require.True(t, result.Changed)
			require.Equal(t, want, model.TaskStatus(task.Status))
		})
	}
}

func TestMergeMidjourneyTaskPreservesDataForSparseUpdate(t *testing.T) {
	properties, err := common.Marshal(&dto.Properties{FinalPrompt: "old prompt"})
	require.NoError(t, err)
	buttons, err := common.Marshal([]dto.ActionButton{{Label: "old button"}})
	require.NoError(t, err)
	videoURLs, err := common.Marshal([]dto.ImgUrls{{Url: "https://video.example/old.mp4"}})
	require.NoError(t, err)

	task := &model.Midjourney{
		Code:       1,
		Status:     string(model.TaskStatusInProgress),
		Progress:   "70%",
		PromptEn:   "old prompt en",
		State:      "old state",
		SubmitTime: 100,
		StartTime:  200,
		ImageUrl:   "https://image.example/old.png",
		VideoUrl:   "https://video.example/old.mp4",
		VideoUrls:  string(videoURLs),
		Properties: string(properties),
		Buttons:    string(buttons),
	}

	result, err := MergeMidjourneyTask(task, dto.MidjourneyDto{
		Status:     string(model.TaskStatusInProgress),
		VideoUrls:  []dto.ImgUrls{},
		Buttons:    []dto.ActionButton{},
		Properties: &dto.Properties{},
	})
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.Equal(t, "70%", task.Progress)
	require.Equal(t, "old prompt en", task.PromptEn)
	require.Equal(t, "old state", task.State)
	require.Equal(t, int64(100), task.SubmitTime)
	require.Equal(t, "https://image.example/old.png", task.ImageUrl)
	require.Equal(t, "https://video.example/old.mp4", task.VideoUrl)
	require.Equal(t, string(videoURLs), task.VideoUrls)
	require.Equal(t, string(properties), task.Properties)
	require.Equal(t, string(buttons), task.Buttons)
}

func TestMergeMidjourneyTaskEmptyStatusDoesNotInferFailure(t *testing.T) {
	task := &model.Midjourney{
		Code:     1,
		Status:   string(model.TaskStatusInProgress),
		Progress: "40%",
	}

	result, err := MergeMidjourneyTask(task, dto.MidjourneyDto{
		Progress:   "50%",
		FailReason: "provider diagnostic",
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.False(t, result.TerminalFailureTransition)
	require.Equal(t, string(model.TaskStatusInProgress), task.Status)
	require.Equal(t, "50%", task.Progress)
	require.Equal(t, "provider diagnostic", task.FailReason)
	require.Equal(t, int64(0), task.FinishTime)
}

func TestMergeMidjourneyTaskRejectsStaleLifecycleAndProgress(t *testing.T) {
	task := &model.Midjourney{
		Code:     1,
		Status:   string(model.TaskStatusInProgress),
		Progress: "70%",
	}

	result, err := MergeMidjourneyTask(task, dto.MidjourneyDto{
		Status:   string(model.TaskStatusQueued),
		Progress: "20%",
	})
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.Equal(t, string(model.TaskStatusInProgress), task.Status)
	require.Equal(t, "70%", task.Progress)
}

func TestMergeMidjourneyTaskRefundTransitionIsIdempotent(t *testing.T) {
	task := &model.Midjourney{
		Code:     1,
		Status:   string(model.TaskStatusInProgress),
		Progress: "70%",
	}

	first, err := MergeMidjourneyTask(task, dto.MidjourneyDto{Status: string(model.TaskStatusFailure)})
	require.NoError(t, err)
	require.True(t, first.Changed)
	require.True(t, first.TerminalFailureTransition)
	require.Equal(t, "100%", task.Progress)
	require.NotZero(t, task.FinishTime)
	require.NotEmpty(t, task.FailReason)

	second, err := MergeMidjourneyTask(task, dto.MidjourneyDto{Status: string(model.TaskStatusFailure)})
	require.NoError(t, err)
	require.False(t, second.Changed)
	require.False(t, second.TerminalFailureTransition)
}

func TestIsRefundableMidjourneyTaskCode(t *testing.T) {
	for _, code := range []int{midjourneyTaskCodeSubmitted, midjourneyTaskCodeExisting, midjourneyTaskCodeQueued} {
		require.Truef(t, isRefundableMidjourneyTaskCode(code), "code %d should be refundable", code)
	}
	for _, code := range []int{0, 3, 23, 24, -1} {
		require.Falsef(t, isRefundableMidjourneyTaskCode(code), "code %d should not be refundable", code)
	}
}
