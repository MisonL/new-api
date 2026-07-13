# Responses Encrypted Context Error Match Verification

Sampling time: 2026-07-14 CST (+08:00)

## Scope

When an OpenAI-compatible upstream omits `error.param`, encrypted-context retry now accepts only the structured `ObjectParam` envelope for `input[n].encrypted_content`, `input[n].content[m].encrypted_content`, or `input[n].output[m].encrypted_content`. A precise `error.param` remains the primary path.

Plain error text that merely quotes an input path no longer authorizes removal of a Responses input item. This prevents an upstream or reflected message from causing unrelated context deletion.

## Verification

| Command | Exit | Result |
| --- | --- | --- |
| `go test ./relay/common -count=1 -timeout=90s` | 0 | Structured top-level and agent-message paths remain removable; an unstructured text match remains unchanged. |
| `go test ./relay -run 'Test(ExecuteOpenAIResponsesRequestRetriesEncryptedReasoningFailure|ExecuteOpenAIResponsesRequestRetriesWrappedMissingEncryptedContent|ExecuteOpenAIResponsesRequestDoesNotRetryMessageContentMissingEncryptedContent|ExecuteOpenAIResponsesRequestRetriesAgentMessageMissingEncryptedContent|ExecuteOpenAIResponsesRequestDoesNotDropCompactionForMissingEncryptedContent|ExecuteOpenAIResponsesRequestRetriesMissingEncryptedContentOnlyOnce|ShouldRetryResponsesWithoutEncryptedReasoningRequiresEncryptedContentPath|ExecuteOpenAIResponsesRequestRetriesEncryptedReasoningBeforeStatusMapping|ExecuteOpenAIResponsesRequestMapsStatusWhenEncryptedRetryHasNoRemovableItems|ExecuteOpenAIResponsesRequestRetryForcesConvertedBodyWhenPassThroughEnabled)' -count=1 -timeout=120s` | 0 | Retry behavior is retained for real `ObjectParam` errors while message, compaction, and no-removable-item boundaries remain intact. |
| `go test ./relay/channel/openai -count=1 -timeout=120s` | 0 | OpenAI Responses channel regression suite passed. |
