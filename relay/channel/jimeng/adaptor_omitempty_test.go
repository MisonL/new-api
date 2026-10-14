package jimeng

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// Every field below has an upstream default that differs from Go's zero value:
// seed defaults to -1 (random), width/height to 512, use_pre_llm and use_sr to
// true. With plain scalars plus omitempty, a client that explicitly sent
// `false` or `0` had the key dropped and got the upstream default instead.
func TestConvertImageRequestKeepsExplicitZeroValues(t *testing.T) {
	adaptor := &Adaptor{}
	request := dto.ImageRequest{
		Model:  "jimeng_high_aes_general_v21_L",
		Prompt: "a cat",
	}
	request.ExtraFields = []byte(`{
		"use_pre_llm": false,
		"use_sr": false,
		"seed": 0,
		"width": 512,
		"height": 512,
		"logo_info": {"add_logo": false, "opacity": 0}
	}`)

	payload, err := adaptor.ConvertImageRequest(nil, nil, request)
	require.NoError(t, err)

	forwarded, err := common.Marshal(payload)
	require.NoError(t, err)
	body := string(forwarded)

	require.Contains(t, body, `"use_pre_llm":false`)
	require.Contains(t, body, `"use_sr":false`)
	require.Contains(t, body, `"seed":0`)
	require.Contains(t, body, `"add_logo":false`)
	require.Contains(t, body, `"opacity":0`)
}

// A client that sends nothing must not have values invented for it: the keys
// stay absent so the upstream applies its documented defaults.
func TestConvertImageRequestOmitsAbsentFields(t *testing.T) {
	adaptor := &Adaptor{}
	request := dto.ImageRequest{
		Model:  "jimeng_high_aes_general_v21_L",
		Prompt: "a cat",
	}

	payload, err := adaptor.ConvertImageRequest(nil, nil, request)
	require.NoError(t, err)

	forwarded, err := common.Marshal(payload)
	require.NoError(t, err)
	body := string(forwarded)

	require.NotContains(t, body, `"use_pre_llm"`)
	require.NotContains(t, body, `"use_sr"`)
	require.NotContains(t, body, `"seed"`)
	require.NotContains(t, body, `"add_logo"`)
	// response_format defaults to url, so return_url is set explicitly.
	require.Contains(t, body, `"return_url":true`)
}
