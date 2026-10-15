import { nanoid } from 'nanoid'
import type {
  ChatCompletionRequest,
  ContentPart,
  Message,
  MessageRole,
  PlaygroundConfig,
  ParameterEnabled,
} from '../types'
import { MESSAGE_ROLES } from '../constants'
import { formatMessageForAPI, isValidMessage } from './message-utils'

type ContentPartInput = {
  type: string
  text?: unknown
  image_url?: { url?: unknown }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

/**
 * Normalize a raw content part. Returns null when the part is malformed,
 * which makes the whole message list invalid.
 */
function normalizeContentPart(raw: unknown): ContentPart | null {
  if (!isRecord(raw)) return null
  const part = raw as unknown as ContentPartInput

  if (part.type === 'text' && typeof part.text === 'string') {
    return { type: 'text', text: part.text }
  }
  if (
    part.type === 'image_url' &&
    isRecord(part.image_url) &&
    typeof part.image_url.url === 'string'
  ) {
    return { type: 'image_url', image_url: { url: part.image_url.url } }
  }
  return null
}

function normalizeContent(content: unknown): string | ContentPart[] | null {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return null

  const parts: ContentPart[] = []
  for (const raw of content) {
    const part = normalizeContentPart(raw)
    if (!part) return null
    parts.push(part)
  }
  return parts
}

/**
 * Convert a custom request payload's raw message list into playground
 * messages. Returns an empty array when the payload has no valid message
 * list, an unknown role, or a message with malformed content parts.
 */
export function payloadToMessages(payload: unknown): Message[] {
  if (!isRecord(payload) || !Array.isArray(payload.messages)) {
    return []
  }

  const messages: Message[] = []
  for (const raw of payload.messages) {
    if (!isRecord(raw)) return []
    const role = raw.role
    if (role !== MESSAGE_ROLES.USER && role !== MESSAGE_ROLES.ASSISTANT && role !== MESSAGE_ROLES.SYSTEM) {
      return []
    }
    const content = normalizeContent(raw.content)
    if (content === null) return []

    messages.push({
      key: nanoid(),
      from: role as MessageRole,
      versions: [{ id: nanoid(), content }],
    })
  }

  return messages
}

/**
 * Build API request payload from messages and config
 */
export function buildChatCompletionPayload(
  messages: Message[],
  config: PlaygroundConfig,
  parameterEnabled: ParameterEnabled
): ChatCompletionRequest {
  // Filter and format valid messages
  const processedMessages = messages
    .filter(isValidMessage)
    .map(formatMessageForAPI)

  const payload: ChatCompletionRequest = {
    model: config.model,
    group: config.group,
    messages: processedMessages,
    stream: config.stream,
  }

  // Add enabled parameters
  const parameterKeys: Array<keyof ParameterEnabled> = [
    'temperature',
    'top_p',
    'max_tokens',
    'frequency_penalty',
    'presence_penalty',
    'seed',
  ]

  parameterKeys.forEach((key) => {
    if (parameterEnabled[key]) {
      const value = config[key as keyof PlaygroundConfig]
      if (value !== undefined && value !== null) {
        ;(payload as unknown as Record<string, unknown>)[key] = value
      }
    }
  })

  return payload
}
