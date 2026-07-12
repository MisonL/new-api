package controller

import "fmt"

func formatChannelTestResponseLog(channelID int, statusCode int, responseBytes int) string {
	return fmt.Sprintf(
		"testing channel #%d completed: status_code=%d response_bytes=%d",
		channelID,
		statusCode,
		responseBytes,
	)
}
