package helper

import (
	"fmt"

	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// RecordStreamEnd records the end reason for a channel whose handler reads the
// upstream stream with its own scanner instead of StreamScannerHandler. Those
// handlers never touched info.StreamStatus, so a healthy stream ended with
// EndReason still at its zero value: classifyStreamStatus then reported every
// healthy stream as "error" and the channel health probe could never be marked
// recovered (controller/relay.go:97 gates it on IsSuccessfulEnd).
//
// The classification mirrors the shared scanner's rule (stream_scanner.go:202):
// a clean end after the protocol's terminal marker is a normal EOF, and a clean
// end without that marker means the upstream stopped mid-stream. Handlers with
// an in-band terminal marker (data: [DONE], a finish event, a final chunk flag)
// must pass whether they saw it; handlers whose protocol ends at EOF cannot use
// this helper, because for them a missing marker cannot be distinguished from a
// complete stream.
func RecordStreamEnd(c *gin.Context, info *relaycommon.RelayInfo, sawTerminalMarker bool) {
	if info == nil || info.StreamStatus == nil {
		return
	}
	if info.StreamStatus.EndReason != relaycommon.StreamEndReasonNone {
		return
	}
	if sawTerminalMarker {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
		return
	}
	err := fmt.Errorf("stream disconnected before completion")
	logger.LogError(c, "upstream stream ended before completion")
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, err)
}
