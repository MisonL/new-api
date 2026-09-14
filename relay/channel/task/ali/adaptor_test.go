package ali

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertToAliRequestUsesEffectiveModelForVideoCapabilities(t *testing.T) {
	tests := []struct {
		name           string
		requested      string
		effective      string
		size           string
		wantSize       string
		wantResolution string
		wantErr        string
	}{
		{
			name:      "mapped text to video default",
			requested: "public-video",
			effective: "wan2.2-t2v-plus",
			wantSize:  "1920*1080",
		},
		{
			name:           "mapped image to video default",
			requested:      "public-video",
			effective:      "wan2.2-i2v-plus",
			wantResolution: "1080P",
		},
		{
			name:      "mapped text to video validates effective capability",
			requested: "public-video",
			effective: "wan2.2-t2v-plus",
			size:      "720P",
			wantErr:   "invalid size",
		},
		{
			name:           "mapped image to video accepts resolution",
			requested:      "public-t2v",
			effective:      "wan2.2-i2v-plus",
			size:           "720P",
			wantResolution: "720P",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					IsModelMapped:     true,
					UpstreamModelName: tt.effective,
				},
			}
			req := relaycommon.TaskSubmitReq{
				Model: tt.requested,
				Size:  tt.size,
			}

			converted, err := (&TaskAdaptor{}).convertToAliRequest(info, req)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.effective, converted.Model)
			require.Equal(t, tt.wantSize, converted.Parameters.Size)
			require.Equal(t, tt.wantResolution, converted.Parameters.Resolution)
		})
	}
}
