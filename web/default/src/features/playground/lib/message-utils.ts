import { nanoid } from 'nanoid'
import { MESSAGE_ROLES, MESSAGE_STATUS, ERROR_MESSAGES } from '../constants'
import type {
  Message,
  MessageVersion,
  ChatCompletionMessage,
  ContentPart,
} from '../types'

/**
 * Create a new message version
 */
export function createMessageVersion(content: string): MessageVersion {
  return {
    id: nanoid(),
    content,
  }
}

/**
 * Get current version from message (always returns the first version)
 */
export function getCurrentVersion(message: Message): MessageVersion {
  return message.versions[0] || { id: 'default', content: '' }
}

/**
 * Update current version content in message
 */
export function updateCurrentVersionContent(
  message: Message,
  content: string
): Message {
  const currentVersion = getCurrentVersion(message)
  return {
    ...message,
    versions: [{ ...currentVersion, content }],
  }
}

/**
 * Create a user message
 */
export function createUserMessage(content: string): Message {
  return {
    key: nanoid(),
    from: MESSAGE_ROLES.USER,
    versions: [createMessageVersion(content)],
  }
}

/**
 * Create a loading assistant message
 */
export function createLoadingAssistantMessage(): Message {
  return {
    key: nanoid(),
    from: MESSAGE_ROLES.ASSISTANT,
    versions: [createMessageVersion('')],
    reasoning: undefined,
    isReasoningComplete: false,
    isContentComplete: false,
    isReasoningStreaming: false,
    status: MESSAGE_STATUS.LOADING,
  }
}

/**
 * Build message content with optional images
 */
export function buildMessageContent(
  text: string,
  imageUrls: string[] = []
): string | ContentPart[] {
  const validImages = imageUrls.filter((url) => url.trim() !== '')

  if (validImages.length === 0) {
    return text
  }

  const parts: ContentPart[] = [
    {
      type: 'text',
      text: text || '',
    },
    ...validImages.map((url) => ({
      type: 'image_url' as const,
      image_url: { url: url.trim() },
    })),
  ]

  return parts
}

/**
 * Extract text content from message content
 */
export function getTextContent(content: string | ContentPart[]): string {
  if (typeof content === 'string') {
    return content
  }

  if (Array.isArray(content)) {
    return content
      .filter((part) => part.type === 'text')
      .map((part) => part.text || '')
      .join('')
  }

  return ''
}

/**
 * Format message for API request
 */
export function formatMessageForAPI(message: Message): ChatCompletionMessage {
  const currentVersion = getCurrentVersion(message)
  return {
    role: message.from,
    content: currentVersion.content,
  }
}

/**
 * Check if message is valid for API request
 * Excludes loading/streaming assistant messages and empty content
 */
export function isValidMessage(message: Message): boolean {
  if (!message || !message.from || !message.versions.length) return false

  const content = message.versions[0]?.content
  if (content === undefined) return false

  // Exclude empty assistant messages (loading/streaming placeholders)
  if (message.from === 'assistant' && !getTextContent(content).trim()) return false

  return true
}

/**
 * Parse content to separate thinking from visible text
 * Handles both complete and incomplete <think> tags
 */
export function parseThinkTags(content: string): {
  visibleContent: string
  reasoning: string
  hasUnclosedTag: boolean
} {
  if (!content.includes('<think>')) {
    return { visibleContent: content, reasoning: '', hasUnclosedTag: false }
  }

  const visibleParts: string[] = []
  const reasoningParts: string[] = []
  let currentPos = 0
  let hasUnclosed = false

  while (true) {
    // Find next <think> tag
    const openPos = content.indexOf('<think>', currentPos)

    if (openPos === -1) {
      // No more think tags, add remaining content
      if (currentPos < content.length) {
        visibleParts.push(content.substring(currentPos))
      }
      break
    }

    // Add visible content before this tag
    if (openPos > currentPos) {
      visibleParts.push(content.substring(currentPos, openPos))
    }

    // Look for matching </think> tag
    const closePos = content.indexOf('</think>', openPos + 7)

    if (closePos === -1) {
      // Unclosed tag: rest is reasoning buffer
      reasoningParts.push(content.substring(openPos + 7))
      hasUnclosed = true
      break
    }

    // Extract reasoning content between tags
    reasoningParts.push(content.substring(openPos + 7, closePos))
    currentPos = closePos + 8
  }

  return {
    visibleContent: visibleParts.join('').trim(),
    reasoning: reasoningParts.join('\n\n').trim(),
    hasUnclosedTag: hasUnclosed,
  }
}

/**
 * Update the last assistant message with an error
 * @param messages - Current messages array
 * @param errorMessage - Error message to display
 * @returns Updated messages array
 */
export function updateAssistantMessageWithError(
  messages: Message[],
  errorMessage: string,
  errorCode?: string
): Message[] {
  return updateLastAssistantMessage(messages, (message) => {
    const updatedMessage = updateCurrentVersionContent(
      message,
      `${ERROR_MESSAGES.API_REQUEST_ERROR}: ${errorMessage}`
    )
    return {
      ...updatedMessage,
      status: MESSAGE_STATUS.ERROR,
      isReasoningStreaming: false,
      errorCode: errorCode || null,
    }
  })
}

/**
 * Helper function to update the last assistant message
 * @param messages - Current messages array
 * @param updater - Function to update the message
 * @returns Updated messages array or original if no assistant message found
 */
export function updateLastAssistantMessage(
  messages: Message[],
  updater: (message: Message) => Message
): Message[] {
  if (messages.length === 0) return messages
  const last = messages[messages.length - 1]
  if (!last || last.from !== MESSAGE_ROLES.ASSISTANT) return messages

  const updated = [...messages]
  updated[updated.length - 1] = updater(last)
  return updated
}

/**
 * Process content chunk during streaming
 * Separates <think> reasoning from visible content in real-time
 * Note: versions[0].content keeps the full raw content (with tags) during streaming
 */
export function processStreamingContent(
  message: Message,
  contentChunk?: string
): Message {
  const currentVersion = getCurrentVersion(message)
  const previousContent = getTextContent(currentVersion.content)
  const fullContent = contentChunk
    ? previousContent + contentChunk
    : previousContent

  const { reasoning, hasUnclosedTag } = parseThinkTags(fullContent)

  // Preserve existing reasoning if no think tags found (e.g., from API reasoning_content)
  const finalReasoning = reasoning
    ? { content: reasoning, duration: 0 }
    : message.reasoning

  return {
    ...updateCurrentVersionContent(message, fullContent),
    reasoning: finalReasoning,
    isReasoningStreaming: hasUnclosedTag,
  }
}

/**
 * Finalize message after streaming completes
 * Cleans content and consolidates reasoning from all sources
 */
export function finalizeMessage(
  message: Message,
  apiReasoningContent?: string
): Message {
  const currentVersion = getCurrentVersion(message)
  const { visibleContent, reasoning } = parseThinkTags(
    getTextContent(currentVersion.content)
  )

  // Priority:
  // 1. API reasoning_content passed as parameter (non-streaming response)
  // 2. Existing message.reasoning (from streaming reasoning_content)
  // 3. Extracted think tags from content
  const finalReasoning =
    apiReasoningContent || message.reasoning?.content || reasoning || ''

  return {
    ...updateCurrentVersionContent(message, visibleContent),
    reasoning: finalReasoning
      ? { content: finalReasoning, duration: message.reasoning?.duration || 0 }
      : undefined,
    isReasoningStreaming: false,
  }
}

/**
 * Sanitize messages loaded from storage
 * Converts stuck loading/streaming messages to stable state
 */
export function sanitizeMessagesOnLoad(messages: Message[]): Message[] {
  let targetIndex = -1
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (
      m?.from === MESSAGE_ROLES.ASSISTANT &&
      (m?.status === MESSAGE_STATUS.LOADING ||
        m?.status === MESSAGE_STATUS.STREAMING)
    ) {
      targetIndex = i
      break
    }
  }

  if (targetIndex === -1) return messages

  const finalized = finalizeMessage(messages[targetIndex])
  const hasContent = getTextContent(finalized.versions?.[0]?.content ?? '').trim()
  const hasReasoning = finalized.reasoning?.content?.trim()

  const sanitized: Message =
    hasContent || hasReasoning
      ? {
          ...finalized,
          status: MESSAGE_STATUS.COMPLETE,
          isReasoningStreaming: false,
        }
      : {
          ...updateCurrentVersionContent(
            finalized,
            `${ERROR_MESSAGES.API_REQUEST_ERROR}: ${ERROR_MESSAGES.INTERRUPTED}`
          ),
          status: MESSAGE_STATUS.ERROR,
          isReasoningStreaming: false,
        }

  const result = [...messages]
  result[targetIndex] = sanitized
  return result
}

/**
 * Append image URLs to message content as image parts.
 * Preserves existing text and image parts, skips URLs already present
 * (compared after trimming) and drops blank URLs.
 */
export function appendImageUrls(
  content: string | ContentPart[],
  imageUrls: string[]
): string | ContentPart[] {
  const parts: ContentPart[] =
    typeof content === 'string'
      ? [{ type: 'text', text: content }]
      : [...content]

  const existingUrls = new Set(
    parts
      .filter((part): part is ContentPart & { type: 'image_url' } =>
        part.type === 'image_url'
      )
      .map((part) => part.image_url?.url.trim())
      .filter((url): url is string => !!url)
  )

  for (const raw of imageUrls) {
    const url = raw.trim()
    if (!url || existingUrls.has(url)) continue
    existingUrls.add(url)
    parts.push({ type: 'image_url', image_url: { url } })
  }

  return parts
}

/**
 * Build the message list for a regeneration attempt:
 * history up to (excluding) the target message, then a fresh user turn
 * carrying the target's user content (if it was a user message), then a
 * new loading assistant placeholder. Returns null when the target message
 * is not found or no preceding user turn exists.
 */
export function buildRegenerationMessages(
  messages: Message[],
  targetKey: string
): Message[] | null {
  const targetIndex = messages.findIndex((message) => message.key === targetKey)
  if (targetIndex === -1) return null

  const target = messages[targetIndex]
  const history = messages.slice(0, targetIndex).filter(isValidMessage)

  if (target.from === MESSAGE_ROLES.USER) {
    return [
      ...history,
      createUserMessage(getTextContent(getCurrentVersion(target).content)),
      createLoadingAssistantMessage(),
    ]
  }

  const lastUserIndex = [...history]
    .reverse()
    .findIndex((message) => message.from === MESSAGE_ROLES.USER)
  if (lastUserIndex === -1) return null

  const cutIndex = history.length - 1 - lastUserIndex
  return [
    ...history.slice(0, cutIndex),
    history[cutIndex],
    createLoadingAssistantMessage(),
  ]
}
