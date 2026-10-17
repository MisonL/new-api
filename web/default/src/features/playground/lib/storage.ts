import { DEFAULT_CONFIG, STORAGE_KEYS } from '../constants'
import type {
  PlaygroundConfig,
  ParameterEnabled,
  Message,
  MessageVersion,
} from '../types'
import { sanitizeMessagesOnLoad } from './message-utils'

/**
 * Normalize an imported or stored config object into a complete
 * PlaygroundConfig, filling missing fields with defaults.
 */
export function normalizePlaygroundConfig(
  value: unknown
): PlaygroundConfig | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const source = value as Partial<PlaygroundConfig> & Record<string, unknown>
  const normalized: PlaygroundConfig = {
    ...DEFAULT_CONFIG,
    model:
      typeof source.model === 'string' && source.model
        ? source.model
        : DEFAULT_CONFIG.model,
    group:
      typeof source.group === 'string' && source.group
        ? source.group
        : DEFAULT_CONFIG.group,
    temperature:
      typeof source.temperature === 'number'
        ? source.temperature
        : DEFAULT_CONFIG.temperature,
    top_p:
      typeof source.top_p === 'number' ? source.top_p : DEFAULT_CONFIG.top_p,
    max_tokens:
      typeof source.max_tokens === 'number'
        ? source.max_tokens
        : DEFAULT_CONFIG.max_tokens,
    frequency_penalty:
      typeof source.frequency_penalty === 'number'
        ? source.frequency_penalty
        : DEFAULT_CONFIG.frequency_penalty,
    presence_penalty:
      typeof source.presence_penalty === 'number'
        ? source.presence_penalty
        : DEFAULT_CONFIG.presence_penalty,
    seed: typeof source.seed === 'number' ? source.seed : DEFAULT_CONFIG.seed,
    stream:
      typeof source.stream === 'boolean'
        ? source.stream
        : DEFAULT_CONFIG.stream,
    imageEnabled:
      typeof source.imageEnabled === 'boolean'
        ? source.imageEnabled
        : DEFAULT_CONFIG.imageEnabled,
    imageUrls: Array.isArray(source.imageUrls)
      ? source.imageUrls.filter((url): url is string => typeof url === 'string')
      : DEFAULT_CONFIG.imageUrls,
    customRequestMode:
      typeof source.customRequestMode === 'boolean'
        ? source.customRequestMode
        : DEFAULT_CONFIG.customRequestMode,
    customRequestBody:
      typeof source.customRequestBody === 'string'
        ? source.customRequestBody
        : DEFAULT_CONFIG.customRequestBody,
    showDebugPanel:
      typeof source.showDebugPanel === 'boolean'
        ? source.showDebugPanel
        : DEFAULT_CONFIG.showDebugPanel,
  }
  return normalized
}

/**
 * Load playground config from localStorage
 */
export function loadConfig(): Partial<PlaygroundConfig> {
  try {
    const saved = localStorage.getItem(STORAGE_KEYS.CONFIG)
    if (saved) {
      return JSON.parse(saved)
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load config:', error)
  }
  return {}
}

/**
 * Save playground config to localStorage
 */
export function saveConfig(config: Partial<PlaygroundConfig>): void {
  try {
    localStorage.setItem(STORAGE_KEYS.CONFIG, JSON.stringify(config))
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save config:', error)
  }
}

/**
 * Load parameter enabled state from localStorage
 */
export function loadParameterEnabled(): Partial<ParameterEnabled> {
  try {
    const saved = localStorage.getItem(STORAGE_KEYS.PARAMETER_ENABLED)
    if (saved) {
      return JSON.parse(saved)
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load parameter enabled:', error)
  }
  return {}
}

/**
 * Save parameter enabled state to localStorage
 */
export function saveParameterEnabled(
  parameterEnabled: Partial<ParameterEnabled>
): void {
  try {
    localStorage.setItem(
      STORAGE_KEYS.PARAMETER_ENABLED,
      JSON.stringify(parameterEnabled)
    )
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save parameter enabled:', error)
  }
}

/**
 * Load messages from localStorage
 */
export function loadMessages(): Message[] | null {
  try {
    const saved = localStorage.getItem(STORAGE_KEYS.MESSAGES)
    if (saved) {
      const parsed: unknown = JSON.parse(saved)
      if (!Array.isArray(parsed)) {
        localStorage.removeItem(STORAGE_KEYS.MESSAGES)
        return null
      }
      const sanitized = sanitizeMessagesOnLoad(parsed as Message[])
      // Persist sanitized result to avoid re-sanitizing on subsequent loads
      if (sanitized !== parsed) {
        saveMessages(sanitized)
      }
      return sanitized
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load messages:', error)
  }
  return null
}

/**
 * Save messages to localStorage
 */
export function saveMessages(messages: Message[]): void {
  try {
    localStorage.setItem(STORAGE_KEYS.MESSAGES, JSON.stringify(messages))
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save messages:', error)
  }
}

/**
 * Clear all playground data
 */
export function clearPlaygroundData(): void {
  try {
    localStorage.removeItem(STORAGE_KEYS.CONFIG)
    localStorage.removeItem(STORAGE_KEYS.PARAMETER_ENABLED)
    localStorage.removeItem(STORAGE_KEYS.MESSAGES)
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to clear playground data:', error)
  }
}

const SAFE_SOURCE_PROTOCOLS = new Set(['http:', 'https:'])

/**
 * Normalize messages restored from storage or import: drop malformed
 * content parts, filter non-http(s) source hrefs, and finalize assistant
 * messages stuck in loading/streaming state.
 */
export function normalizeStoredMessages(messages: Message[]): Message[] {
  const sanitized = sanitizeMessagesOnLoad(messages)

  return sanitized.map((message) => {
    const versions: MessageVersion[] = message.versions.map((version) => {
      const content = version.content
      if (!Array.isArray(content)) return version
      const parts = content.filter(
        (part) =>
          !!part &&
          (part.type === 'text' || part.type === 'image_url') &&
          (part.type !== 'image_url' || !!part.image_url?.url)
      )
      return { ...version, content: parts }
    })

    let sources = message.sources
    if (Array.isArray(sources)) {
      sources = sources.filter((source) => {
        try {
          return SAFE_SOURCE_PROTOCOLS.has(new URL(source.href).protocol)
        } catch {
          return false
        }
      })
    }

    return { ...message, versions, sources }
  })
}
