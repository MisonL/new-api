import { useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { sendChatCompletion } from '../api'
import { MESSAGE_STATUS, ERROR_MESSAGES } from '../constants'
import {
  buildChatCompletionPayload,
  parseCustomRequestBody,
  updateAssistantMessageWithError,
  updateLastAssistantMessage,
  processStreamingContent,
  finalizeMessage,
} from '../lib'
import type {
  ChatCompletionRequest,
  Message,
  PlaygroundConfig,
  ParameterEnabled,
} from '../types'
import { useStreamRequest } from './use-stream-request'

interface UseChatHandlerOptions {
  config: PlaygroundConfig
  parameterEnabled: ParameterEnabled
  onMessageUpdate: (updater: (prev: Message[]) => Message[]) => void
  onDebugEvent?: (event: PlaygroundDebugEvent) => void
}

/** One step of a request/response exchange, as shown by the debug panel. */
export type PlaygroundDebugEvent =
  | { kind: 'request'; payload: string }
  | { kind: 'sse'; data: string }
  | { kind: 'reply'; text: string }

/**
 * Hook for handling chat message sending and receiving
 */
export function useChatHandler({
  config,
  parameterEnabled,
  onMessageUpdate,
  onDebugEvent,
}: UseChatHandlerOptions) {
  const { t } = useTranslation()
  const { sendStreamRequest, stopStream, isStreaming } = useStreamRequest()

  // Capturing every frame is only worth its re-renders while the panel is open,
  // so the debug callbacks stay silent otherwise.
  const debugEnabled = config.showDebugPanel
  const emitDebug = useCallback(
    (event: PlaygroundDebugEvent) => {
      if (!debugEnabled) return
      onDebugEvent?.(event)
    },
    [debugEnabled, onDebugEvent]
  )

  /**
   * Build the payload for the next request. Custom request mode sends the
   * textarea verbatim, and an unusable body is refused with a visible error
   * instead of silently sending the generated payload.
   */
  const resolveRequestPayload = useCallback(
    (messages: Message[]): ChatCompletionRequest | null => {
      if (!config.customRequestMode) {
        return buildChatCompletionPayload(messages, config, parameterEnabled)
      }
      const customBody = parseCustomRequestBody(config.customRequestBody)
      if (!customBody) {
        toast.error(t(ERROR_MESSAGES.INVALID_CUSTOM_REQUEST_BODY))
        return null
      }
      return customBody as unknown as ChatCompletionRequest
    },
    [config, parameterEnabled, t]
  )

  const debugReplyRef = useRef('')

  // Handle stream update
  const handleStreamUpdate = useCallback(
    (type: 'reasoning' | 'content', chunk: string) => {
      if (debugEnabled) {
        debugReplyRef.current += chunk
        emitDebug({ kind: 'reply', text: debugReplyRef.current })
      }
      onMessageUpdate((prev) =>
        updateLastAssistantMessage(prev, (message) => {
          if (message.status === MESSAGE_STATUS.ERROR) return message

          if (type === 'reasoning') {
            // Direct API reasoning_content
            return {
              ...message,
              reasoning: {
                content: (message.reasoning?.content || '') + chunk,
                duration: 0,
              },
              isReasoningStreaming: true,
              status: MESSAGE_STATUS.STREAMING,
            }
          }

          // Content streaming: handle <think> tags
          return {
            ...processStreamingContent(message, chunk),
            status: MESSAGE_STATUS.STREAMING,
          }
        })
      )
    },
    [onMessageUpdate, debugEnabled, emitDebug]
  )

  // Handle stream complete
  const handleStreamComplete = useCallback(() => {
    onMessageUpdate((prev) =>
      updateLastAssistantMessage(prev, (message) =>
        message.status === MESSAGE_STATUS.COMPLETE ||
        message.status === MESSAGE_STATUS.ERROR
          ? message
          : { ...finalizeMessage(message), status: MESSAGE_STATUS.COMPLETE }
      )
    )
  }, [onMessageUpdate])

  // Handle stream error
  const handleStreamError = useCallback(
    (error: string, errorCode?: string) => {
      toast.error(error)
      onMessageUpdate((prev) =>
        updateAssistantMessageWithError(prev, error, errorCode)
      )
    },
    [onMessageUpdate]
  )

  // Send streaming chat request
  const sendStreamingChat = useCallback(
    (messages: Message[]) => {
      const payload = resolveRequestPayload(messages)
      if (!payload) return
      debugReplyRef.current = ''
      emitDebug({ kind: 'request', payload: JSON.stringify(payload, null, 2) })
      sendStreamRequest(
        payload,
        handleStreamUpdate,
        handleStreamComplete,
        handleStreamError,
        (data) => emitDebug({ kind: 'sse', data })
      )
    },
    [
      resolveRequestPayload,
      emitDebug,
      sendStreamRequest,
      handleStreamUpdate,
      handleStreamComplete,
      handleStreamError,
    ]
  )

  // Send non-streaming chat request
  const sendNonStreamingChat = useCallback(
    async (messages: Message[]) => {
      const payload = resolveRequestPayload(messages)
      if (!payload) return
      emitDebug({ kind: 'request', payload: JSON.stringify(payload, null, 2) })

      try {
        const response = await sendChatCompletion(payload)
        const choice = response.choices?.[0]
        if (!choice) return
        emitDebug({
          kind: 'reply',
          text: JSON.stringify(choice.message ?? {}, null, 2),
        })

        onMessageUpdate((prev) =>
          updateLastAssistantMessage(prev, (message) => ({
            ...finalizeMessage(
              {
                ...message,
                versions: [
                  {
                    ...message.versions[0],
                    content: choice.message?.content || '',
                  },
                ],
              },
              choice.message?.reasoning_content
            ),
            status: MESSAGE_STATUS.COMPLETE,
          }))
        )
      } catch (error: unknown) {
        const err = error as {
          response?: {
            data?: { message?: string; error?: { code?: string } }
          }
          message?: string
        }
        handleStreamError(
          err?.response?.data?.message ||
            err?.message ||
            ERROR_MESSAGES.API_REQUEST_ERROR,
          err?.response?.data?.error?.code || undefined
        )
      }
    },
    [resolveRequestPayload, emitDebug, onMessageUpdate, handleStreamError]
  )

  // Send chat request (stream or non-stream based on config)
  const sendChat = useCallback(
    (messages: Message[]) => {
      if (config.stream) {
        sendStreamingChat(messages)
      } else {
        sendNonStreamingChat(messages)
      }
    },
    [config.stream, sendStreamingChat, sendNonStreamingChat]
  )

  // Stop generation
  const stopGeneration = useCallback(() => {
    stopStream()
    onMessageUpdate((prev) =>
      updateLastAssistantMessage(prev, (message) =>
        message.status === MESSAGE_STATUS.LOADING ||
        message.status === MESSAGE_STATUS.STREAMING
          ? { ...finalizeMessage(message), status: MESSAGE_STATUS.COMPLETE }
          : message
      )
    )
  }, [stopStream, onMessageUpdate])

  return {
    sendChat,
    stopGeneration,
    isGenerating: isStreaming,
  }
}
