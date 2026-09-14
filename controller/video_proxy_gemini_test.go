package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureAPIKeyPreservesQueryAndFragment(t *testing.T) {
	got := ensureAPIKey("https://storage.example/video.mp4?alt=media#download", "a+b&c=d")
	require.Equal(t, "https://storage.example/video.mp4?alt=media&key=a%2Bb%26c%3Dd#download", got)
}

func TestEnsureAPIKeyDoesNotDuplicateExistingKeyParameter(t *testing.T) {
	for _, uri := range []string{
		"https://storage.example/video.mp4?key=existing",
		"https://storage.example/video.mp4?key",
		"https://storage.example/video.mp4?alt=media&key=existing#fragment",
	} {
		t.Run(uri, func(t *testing.T) {
			require.Equal(t, uri, ensureAPIKey(uri, "new-key"))
		})
	}
}

func TestEnsureAPIKeyHandlesEmptyQueryAndNoOpInputs(t *testing.T) {
	require.Equal(t, "https://storage.example/video.mp4?key=plain-key", ensureAPIKey("https://storage.example/video.mp4", "plain-key"))
	require.Equal(t, "https://storage.example/video.mp4?key=plain-key", ensureAPIKey("https://storage.example/video.mp4?", "plain-key"))
	require.Equal(t, "", ensureAPIKey("", "plain-key"))
	require.Equal(t, "https://storage.example/video.mp4", ensureAPIKey("https://storage.example/video.mp4", ""))
	require.Equal(t, "data:video/mp4;base64,AAAA", ensureAPIKey("data:video/mp4;base64,AAAA", "plain-key"))
}

func TestEnsureAPIKeyLeavesMalformedQueryUnchanged(t *testing.T) {
	uri := "https://storage.example/video.mp4?bad=%zz#fragment"
	require.Equal(t, uri, ensureAPIKey(uri, "plain-key"))
}
