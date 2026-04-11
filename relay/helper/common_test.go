package helper

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldTreatAsEventStreamForMislabelledJSON(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(`{"object":"chat.completion"}`)),
	}

	isStream := ShouldTreatAsEventStream(resp, false)
	require.False(t, isStream)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, `{"object":"chat.completion"}`, string(body))
}

func TestShouldTreatAsEventStreamForActualSSE(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader("data: {\"ok\":true}\n\n")),
	}

	isStream := ShouldTreatAsEventStream(resp, false)
	require.True(t, isStream)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "data: {\"ok\":true}\n\n", string(body))
}

func TestShouldTreatAsEventStreamKeepsRequestedStream(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}

	require.True(t, ShouldTreatAsEventStream(resp, true))
}
