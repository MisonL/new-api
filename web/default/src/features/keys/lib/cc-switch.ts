export interface BuildCCSwitchURLParams {
  app: string
  name: string
  models: Record<string, string>
  apiKey: string
  serverAddress: string
}

function trimTrailingSlash(value: string): string {
  return value.trim().replace(/\/+$/, '')
}

function normalizeCodexServerAddress(value: string): string {
  const trimmed = trimTrailingSlash(value)
  return trimmed.endsWith('/v1') ? trimmed.slice(0, -3) : trimmed
}

export function ensureCCSwitchApiKey(apiKey: string): string {
  const key = apiKey.trim()
  if (!key) return ''
  return key.startsWith('sk-') ? key : `sk-${key}`
}

export function buildCCSwitchURL(params: BuildCCSwitchURLParams): string {
  const serverAddress = trimTrailingSlash(params.serverAddress)
  if (!serverAddress) {
    throw new Error('Server address is required for CC Switch import')
  }
  const apiKey = ensureCCSwitchApiKey(params.apiKey)
  if (!apiKey) {
    throw new Error('API key is required for CC Switch import')
  }
  const endpoint =
    params.app === 'codex'
      ? `${normalizeCodexServerAddress(serverAddress)}/v1`
      : serverAddress
  const homepage =
    params.app === 'codex'
      ? normalizeCodexServerAddress(serverAddress)
      : serverAddress
  const searchParams = new URLSearchParams()
  searchParams.set('resource', 'provider')
  searchParams.set('app', params.app)
  searchParams.set('name', params.name)
  searchParams.set('endpoint', endpoint)
  searchParams.set('apiKey', apiKey)
  for (const [key, value] of Object.entries(params.models)) {
    if (value) searchParams.set(key, value)
  }
  searchParams.set('homepage', homepage)
  searchParams.set('enabled', 'true')
  return `ccswitch://v1/import?${searchParams.toString()}`
}
