package helper

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// StreamResult is passed to each dataHandler invocation, providing methods
// to record soft errors, signal fatal stops, or mark normal completion.
// StreamScannerHandler checks IsStopped() after each callback invocation.
type StreamResult struct {
	status  *relaycommon.StreamStatus
	stopped bool
}

func newStreamResult(status *relaycommon.StreamStatus) *StreamResult {
	return &StreamResult{status: status}
}

// Error records a soft error. The stream continues processing.
// Can be called multiple times per chunk.
func (r *StreamResult) Error(err error) {
	if err == nil {
		return
	}
	r.status.RecordError(err.Error())
}

// Stop records a fatal error and marks the stream to stop after this chunk.
func (r *StreamResult) Stop(err error) {
	if err != nil {
		r.status.RecordError(err.Error())
	}
	r.status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
	r.stopped = true
}

// Done signals that the handler has finished processing normally
// (e.g., Dify "message_end"). The stream stops after this chunk.
func (r *StreamResult) Done() {
	r.status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	r.stopped = true
}

// MarkDone records a normal stream end without stopping the scanner after this
// chunk. A protocol whose terminal marker can be followed by trailing metadata
// needs this: Gemini marks the end with a candidate finishReason but may send a
// usage-only chunk afterwards, and stopping at the marker would drop that usage.
// The recorded reason is final (SetEndReason runs once), so a later transport
// error is reported as "scanner closed after normal stream end", not as an
// interruption.
func (r *StreamResult) MarkDone() {
	r.status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
}

// IsStopped returns whether Stop() or Done() was called during this chunk.
func (r *StreamResult) IsStopped() bool {
	return r.stopped
}

// reset clears the per-chunk stopped flag so the object can be reused.
func (r *StreamResult) reset() {
	r.stopped = false
}
