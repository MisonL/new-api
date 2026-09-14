package gemini

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVeoResolutionRatioUsesOfficialPrices(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		resolution string
		want       float64
	}{
		{name: "standard 720p", model: "veo-3.1-generate-preview", resolution: "720p", want: 1},
		{name: "standard 1080p", model: "veo-3.1-generate-preview", resolution: "1080p", want: 1},
		{name: "standard 4k", model: "veo-3.1-generate-preview", resolution: "4k", want: 1.5},
		{name: "fast 720p", model: "veo-3.1-fast-generate-preview", resolution: "720p", want: 1},
		{name: "fast 1080p", model: "veo-3.1-fast-generate-preview", resolution: "1080p", want: 1.2},
		{name: "fast 4k", model: "veo-3.1-fast-generate-preview", resolution: "4k", want: 3},
		{name: "lite 720p", model: "veo-3.1-lite-generate-preview", resolution: "720p", want: 1},
		{name: "lite 1080p", model: "veo-3.1-lite-generate-preview", resolution: "1080p", want: 1.6},
		{name: "lite 4k unsupported", model: "veo-3.1-lite-generate-preview", resolution: "4k", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.InDelta(t, tt.want, VeoResolutionRatio(tt.model, tt.resolution), 1e-9)
		})
	}
}

func TestTaskAdaptorModelListIncludesVeoLite(t *testing.T) {
	require.Contains(t, (&TaskAdaptor{}).GetModelList(), "veo-3.1-lite-generate-preview")
}
