package constant

import "testing"

func TestIsValidChannelTypeRejectsReservedGapsAndBounds(t *testing.T) {
	for _, channelType := range []int{ChannelTypeOpenAI, ChannelTypeCodex, ChannelTypeAgnes} {
		if !IsValidChannelType(channelType) {
			t.Fatalf("channel type %d should be valid", channelType)
		}
	}
	for _, channelType := range []int{ChannelTypeUnknown, 28, 29, 30, 32, ChannelTypeDummy, -1, len(ChannelBaseURLs)} {
		if IsValidChannelType(channelType) {
			t.Fatalf("channel type %d should be rejected", channelType)
		}
	}
}

func TestChannelBaseURLIsSafeForInvalidTypes(t *testing.T) {
	url, ok := ChannelBaseURL(ChannelTypeOpenAI)
	if !ok || url == "" {
		t.Fatalf("expected OpenAI base URL, got %q, %v", url, ok)
	}

	for _, channelType := range []int{-1, 28, 32, ChannelTypeDummy + 1} {
		url, ok := ChannelBaseURL(channelType)
		if ok || url != "" {
			t.Fatalf("invalid channel type %d returned %q, %v", channelType, url, ok)
		}
	}

	// Custom and other channels with an intentionally empty default endpoint are
	// still valid types; the boolean reports type validity, not URL presence.
	url, ok = ChannelBaseURL(ChannelTypeCustom)
	if !ok || url != "" {
		t.Fatalf("expected valid empty custom URL, got %q, %v", url, ok)
	}
}
