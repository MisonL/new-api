package controller

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatChannelTestResponseLogDoesNotIncludeBody(t *testing.T) {
	body := []byte(`{"instructions":"sensitive prompt","output":"sensitive result"}`)

	got := formatChannelTestResponseLog(205, http.StatusOK, len(body))

	require.Equal(t, "testing channel #205 completed: status_code=200 response_bytes=63", got)
	require.NotContains(t, got, "sensitive prompt")
	require.NotContains(t, got, "sensitive result")
}
