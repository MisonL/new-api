# Upstream 429 Retry Verification

Sampling time: 2026-07-14 CST (+08:00)

## Scope

The retry controller now permits at most one additional outbound attempt after an upstream HTTP 429. The first failed channel remains excluded from the follow-up channel selection. Once the permitted follow-up reaches the real transport boundary, later internal sends are rejected before any HTTP, SDK, or WebSocket dial occurs. Local rate limits, local conversion failures, and disabled channels do not consume this upstream retry budget.

The coverage includes the shared HTTP sender, OpenAI Realtime WebSocket, AWS Bedrock SDK, Volcengine streaming TTS WebSocket, Xunfei WebSocket, task submit flows, mapped HTTP 429 responses, and Responses bootstrap recovery.

## Verification

| Command | Exit | Result |
| --- | --- | --- |
| `go test ./controller ./service ./model ./setting/operation_setting -run 'Test(ShouldRetry.*UpstreamRateLimit|ObserveUpstreamRateLimit|ShouldStopLockedTask|AppendChannelErrorAdminInfo|UpstreamRateLimitRetryState|TerminateUpstreamRateLimit|CanContinueResponsesBootstrapRecoveryStopsAfterRateLimit|GenerateTextOtherInfoIncludesUpstreamRateLimit|UpdateOptionValidatesUpstreamRateLimit|RelayStopsAfterOneOutboundUpstreamRateLimitFollowup|ResetStatusCode)' -count=1 -timeout=120s` | 0 | Budget, mapped 429, locked task, error log fields, bootstrap guard, option validation, and one follow-up channel selection passed. |
| `go test ./relay/channel -run 'Test(DoWssRequestPreservesRateLimitHandshakeAndConsumesFollowup|DoRequestWithClientConsumesRateLimitFollowupAtTransportBoundary|DoRequestWithClientRejectsRateLimitFollowupAfterBudgetExhausted)' -count=1 -timeout=90s` | 0 | Shared HTTP and Realtime WebSocket retain upstream 429 and reject any later outbound send. |
| `go test ./relay/channel/aws ./relay/channel/volcengine ./relay/channel/xunfei -run 'Test(AwsDirectInvocationsConsumeRateLimitFollowupAtSendBoundary|NewAwsClientDoesNotRetryUpstreamRateLimits|HandleTTSWebSocketResponsePreservesRateLimitAndConsumesFollowup|XunfeiWebSocketHandshakePreservesRateLimitAndConsumesFollowup)' -count=1 -timeout=120s` | 0 | AWS, Volcengine, and Xunfei direct transports preserve 429, consume one follow-up, and reject a second send. |
| `go test ./... -count=1 -timeout=180s` | 0 | Full Go test suite passed. |
| `go test -race ./controller ./service ./relay/channel ./relay/channel/aws ./relay/channel/volcengine ./relay/channel/xunfei ./relay/common -count=1 -timeout=240s` | 0 | No race reported for the changed retry state and transport paths. |
| `go vet ./...` | 0 | Passed. |
| `cd web/default && bun run lint && bun run typecheck && bun run build` | 0 | Default frontend passed. |
| `cd web/default && bun run i18n:sync` | 0 | All six locales have zero missing keys; Japanese retains one pre-existing untranslated item. |
| `cd web/classic && bun run build` | 0 | Classic frontend build passed. |
| `cd web/classic && bun run lint` | 1 | Blocked only by pre-existing formatting in `src/components/table/channels/ChannelsColumnDefs.jsx`; it is outside this change. All touched classic files pass targeted Prettier verification. |

## Runtime Gate

The remaining gate is an isolated-container rebuild followed by a real Responses request. Production deployment must verify the running build revision, the persisted `UpstreamRateLimitRetryLimit=1` option, health status, and post-release upstream 429 retry fields in error logs.
