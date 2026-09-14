package common

import (
	"strings"
	"sync"
	"unicode/utf8"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	maxResponsesObservationEventType = 96
	maxResponsesObservationID        = 256
	maxResponsesObservationStatus    = 64
	maxResponsesObservationError     = 128
)

// ResponsesStreamObservation records the protocol-level evidence available
// from one Responses upstream attempt. It is diagnostic state only: callers
// must still require a valid terminal response before settling quota.
type ResponsesStreamObservation struct {
	mu sync.RWMutex

	attemptNumber int
	attempts      []ResponsesStreamObservationSnapshot

	EventCount        int
	FirstEventType    string
	LastEventType     string
	TerminalEventType string
	ResponseID        string
	ResponseStatus    string
	Completed         bool
	Incomplete        bool
	Failed            bool
	OutputSeen        bool
	TextOutputSeen    bool
	OutputEventSeen   bool
	SawDone           bool
	UsagePresent      bool
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	ErrorType         string
	ErrorCode         string
	ErrorParam        string
}

// ResponsesStreamObservationSnapshot is a race-free copy used by log
// generation and tests.
type ResponsesStreamObservationSnapshot struct {
	AttemptNumber     int
	EventCount        int
	FirstEventType    string
	LastEventType     string
	TerminalEventType string
	ResponseID        string
	ResponseStatus    string
	Completed         bool
	Incomplete        bool
	Failed            bool
	OutputSeen        bool
	TextOutputSeen    bool
	OutputEventSeen   bool
	SawDone           bool
	UsagePresent      bool
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	ErrorType         string
	ErrorCode         string
	ErrorParam        string
}

func NewResponsesStreamObservation() *ResponsesStreamObservation {
	return &ResponsesStreamObservation{attemptNumber: 1}
}

func (o *ResponsesStreamObservation) Reset() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.attemptNumber = 1
	o.attempts = nil
	o.EventCount = 0
	o.FirstEventType = ""
	o.LastEventType = ""
	o.TerminalEventType = ""
	o.ResponseID = ""
	o.ResponseStatus = ""
	o.Completed = false
	o.Incomplete = false
	o.Failed = false
	o.OutputSeen = false
	o.TextOutputSeen = false
	o.OutputEventSeen = false
	o.SawDone = false
	o.UsagePresent = false
	o.InputTokens = 0
	o.OutputTokens = 0
	o.TotalTokens = 0
	o.ErrorType = ""
	o.ErrorCode = ""
	o.ErrorParam = ""
	o.mu.Unlock()
}

// BeginAttempt preserves protocol evidence before a channel retry starts.
// RelayInfo is reused by the retry loop, so replacing the current observer
// would otherwise erase the first upstream attempt that may already have
// produced billable output.
func (o *ResponsesStreamObservation) BeginAttempt() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.EventCount > 0 || o.SawDone {
		o.attempts = appendBoundedObservationAttempt(o.attempts, o.snapshotLocked())
	}
	o.attemptNumber++
	o.resetCurrentLocked()
}

func (o *ResponsesStreamObservation) resetCurrentLocked() {
	o.EventCount = 0
	o.FirstEventType = ""
	o.LastEventType = ""
	o.TerminalEventType = ""
	o.ResponseID = ""
	o.ResponseStatus = ""
	o.Completed = false
	o.Incomplete = false
	o.Failed = false
	o.OutputSeen = false
	o.TextOutputSeen = false
	o.OutputEventSeen = false
	o.SawDone = false
	o.UsagePresent = false
	o.InputTokens = 0
	o.OutputTokens = 0
	o.TotalTokens = 0
	o.ErrorType = ""
	o.ErrorCode = ""
	o.ErrorParam = ""
}

func (o *ResponsesStreamObservation) Observe(event dto.ResponsesStreamResponse) {
	if o == nil {
		return
	}
	eventType := sanitizeResponsesObservationValue(event.Type, maxResponsesObservationEventType)
	if eventType == "" {
		return
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	o.EventCount++
	if o.FirstEventType == "" {
		o.FirstEventType = eventType
	}
	o.LastEventType = eventType
	if strings.HasPrefix(eventType, "response.output_") ||
		strings.HasPrefix(eventType, "response.reasoning_") ||
		strings.HasPrefix(eventType, "response.function_call_") ||
		strings.HasPrefix(eventType, "response.content_") {
		o.OutputSeen = true
		o.OutputEventSeen = true
	}
	if strings.HasPrefix(eventType, "response.output_text.") ||
		strings.HasPrefix(eventType, "response.content_part.") {
		o.TextOutputSeen = true
	}

	if event.Response != nil {
		if len(event.Response.Output) > 0 {
			o.OutputSeen = true
			o.OutputEventSeen = true
		}
		if id := sanitizeResponsesObservationValue(event.Response.ID, maxResponsesObservationID); id != "" {
			o.ResponseID = id
		}
		if status := sanitizeResponsesObservationValue(appcommon.JsonRawMessageToString(event.Response.Status), maxResponsesObservationStatus); status != "" {
			o.ResponseStatus = status
		}
		if event.Response.Usage != nil {
			o.UsagePresent = true
			if inputTokens := firstPositive(event.Response.Usage.InputTokens, event.Response.Usage.PromptTokens); inputTokens > 0 {
				o.InputTokens = inputTokens
			}
			if outputTokens := firstPositive(event.Response.Usage.OutputTokens, event.Response.Usage.CompletionTokens); outputTokens > 0 {
				o.OutputTokens = outputTokens
			}
			if event.Response.Usage.TotalTokens > 0 {
				o.TotalTokens = event.Response.Usage.TotalTokens
			}
			if o.TotalTokens == 0 {
				o.TotalTokens = o.InputTokens + o.OutputTokens
			}
		}
	}

	switch eventType {
	case "response.completed", "response.done":
		o.Completed = true
		if o.TerminalEventType == "" {
			o.TerminalEventType = eventType
		}
	case "response.incomplete":
		o.Incomplete = true
		if o.TerminalEventType == "" {
			o.TerminalEventType = eventType
		}
	case "response.error", "error", "response.failed":
		o.Failed = true
		if o.TerminalEventType == "" {
			o.TerminalEventType = eventType
		}
		openAIError := event.Error
		if openAIError == nil && event.Response != nil {
			openAIError = event.Response.GetOpenAIError()
		}
		if openAIError != nil {
			o.ErrorType = sanitizeResponsesObservationValue(openAIError.Type, maxResponsesObservationError)
			o.ErrorCode = sanitizeResponsesObservationValue(stringValue(openAIError.Code), maxResponsesObservationError)
			o.ErrorParam = sanitizeResponsesObservationValue(openAIError.Param, maxResponsesObservationError)
		}
	}
}

func (o *ResponsesStreamObservation) MarkDone() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.SawDone = true
	o.mu.Unlock()
}

func (o *ResponsesStreamObservation) Snapshot() ResponsesStreamObservationSnapshot {
	if o == nil {
		return ResponsesStreamObservationSnapshot{}
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return ResponsesStreamObservationSnapshot{
		AttemptNumber:     o.attemptNumber,
		EventCount:        o.EventCount,
		FirstEventType:    o.FirstEventType,
		LastEventType:     o.LastEventType,
		TerminalEventType: o.TerminalEventType,
		ResponseID:        o.ResponseID,
		ResponseStatus:    o.ResponseStatus,
		Completed:         o.Completed,
		Incomplete:        o.Incomplete,
		Failed:            o.Failed,
		OutputSeen:        o.OutputSeen,
		TextOutputSeen:    o.TextOutputSeen,
		OutputEventSeen:   o.OutputEventSeen,
		SawDone:           o.SawDone,
		UsagePresent:      o.UsagePresent,
		InputTokens:       o.InputTokens,
		OutputTokens:      o.OutputTokens,
		TotalTokens:       o.TotalTokens,
		ErrorType:         o.ErrorType,
		ErrorCode:         o.ErrorCode,
		ErrorParam:        o.ErrorParam,
	}
}

// ToLogMap returns only bounded, non-secret protocol metadata. Error messages
// are intentionally excluded; the existing masked relay error remains the
// source for human-readable failure text.
func (o *ResponsesStreamObservation) ToLogMap(receivedEvents int) map[string]interface{} {
	snapshot := o.Snapshot()
	// The observer is attached to every RelayInfo for retry safety, but only a
	// parsed Responses event proves that this was a Responses stream. This
	// prevents ordinary Chat Completions SSE logs from receiving a misleading
	// Responses completion marker merely because they emitted [DONE].
	o.mu.RLock()
	attempts := append([]ResponsesStreamObservationSnapshot(nil), o.attempts...)
	o.mu.RUnlock()
	if snapshot.EventCount == 0 && len(attempts) == 0 {
		return nil
	}
	if snapshot.EventCount == 0 && len(attempts) > 0 {
		snapshot = attempts[len(attempts)-1]
	}
	result := map[string]interface{}{
		"attempt":              snapshot.AttemptNumber,
		"event_count":          snapshot.EventCount,
		"received_events":      receivedEvents,
		"completed":            snapshot.Completed,
		"incomplete":           snapshot.Incomplete,
		"failed":               snapshot.Failed,
		"output_event_seen":    snapshot.OutputEventSeen,
		"received_done":        snapshot.SawDone,
		"usage_present":        snapshot.UsagePresent,
		"completion_confirmed": (snapshot.Completed || snapshot.Incomplete) && !snapshot.Failed,
	}
	if snapshot.OutputSeen {
		result["output_seen"] = true
	}
	if snapshot.TextOutputSeen {
		result["text_output_seen"] = true
	}
	if len(attempts) > 0 {
		allAttempts := append(attempts, snapshot)
		if len(allAttempts) > maxResponsesObservationAttempts {
			allAttempts = allAttempts[len(allAttempts)-maxResponsesObservationAttempts:]
		}
		result["attempt_count"] = len(allAttempts)
		result["attempts"] = allAttempts
	}
	if snapshot.FirstEventType != "" {
		result["first_event"] = snapshot.FirstEventType
	}
	if snapshot.LastEventType != "" {
		result["last_event"] = snapshot.LastEventType
	}
	if snapshot.TerminalEventType != "" {
		result["terminal_event"] = snapshot.TerminalEventType
	}
	if snapshot.ResponseID != "" {
		result["response_id"] = snapshot.ResponseID
	}
	if snapshot.ResponseStatus != "" {
		result["response_status"] = snapshot.ResponseStatus
	}
	if snapshot.UsagePresent {
		result["input_tokens"] = snapshot.InputTokens
		result["output_tokens"] = snapshot.OutputTokens
		result["total_tokens"] = snapshot.TotalTokens
	}
	if snapshot.ErrorType != "" {
		result["error_type"] = snapshot.ErrorType
	}
	if snapshot.ErrorCode != "" {
		result["error_code"] = snapshot.ErrorCode
	}
	if snapshot.ErrorParam != "" {
		result["error_param"] = snapshot.ErrorParam
	}
	if snapshot.OutputSeen && (!(snapshot.Completed || snapshot.Incomplete) || snapshot.Failed) {
		result["completion_unconfirmed"] = true
	}
	if snapshot.OutputSeen && snapshot.Failed {
		result["failed_after_output"] = true
	}
	return result
}

const maxResponsesObservationAttempts = 4

func appendBoundedObservationAttempt(attempts []ResponsesStreamObservationSnapshot, snapshot ResponsesStreamObservationSnapshot) []ResponsesStreamObservationSnapshot {
	attempts = append(attempts, snapshot)
	if len(attempts) > maxResponsesObservationAttempts {
		attempts = attempts[len(attempts)-maxResponsesObservationAttempts:]
	}
	return attempts
}

func (o *ResponsesStreamObservation) snapshotLocked() ResponsesStreamObservationSnapshot {
	return ResponsesStreamObservationSnapshot{
		AttemptNumber:     o.attemptNumber,
		EventCount:        o.EventCount,
		FirstEventType:    o.FirstEventType,
		LastEventType:     o.LastEventType,
		TerminalEventType: o.TerminalEventType,
		ResponseID:        o.ResponseID,
		ResponseStatus:    o.ResponseStatus,
		Completed:         o.Completed,
		Incomplete:        o.Incomplete,
		Failed:            o.Failed,
		OutputSeen:        o.OutputSeen,
		TextOutputSeen:    o.TextOutputSeen,
		OutputEventSeen:   o.OutputEventSeen,
		SawDone:           o.SawDone,
		UsagePresent:      o.UsagePresent,
		InputTokens:       o.InputTokens,
		OutputTokens:      o.OutputTokens,
		TotalTokens:       o.TotalTokens,
		ErrorType:         o.ErrorType,
		ErrorCode:         o.ErrorCode,
		ErrorParam:        o.ErrorParam,
	}
}

func sanitizeResponsesObservationValue(value string, limit int) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "?")
	value = appcommon.MaskSensitiveInfo(value)
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	limitBytes := limit
	for limitBytes > 0 && limitBytes < len(value) && !utf8.RuneStart(value[limitBytes]) {
		limitBytes--
	}
	return value[:limitBytes]
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(typesErrorCodeString(value))
}

func typesErrorCodeString(value any) string {
	return strings.TrimSpace(appcommon.Interface2String(value))
}
