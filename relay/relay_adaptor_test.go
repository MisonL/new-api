package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	"github.com/stretchr/testify/require"
)

func TestGetAdaptorAgnesDefaultsToAgnesCatalog(t *testing.T) {
	t.Parallel()

	adaptor := GetAdaptor(constant.APITypeAgnes)

	require.NotNil(t, adaptor)
	require.Equal(t, openai.AgnesModelList, adaptor.GetModelList())
	require.Equal(t, openai.AgnesChannelName, adaptor.GetChannelName())
}
