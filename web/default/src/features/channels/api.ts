import type { AxiosRequestConfig } from 'axios'
import { api } from '@/lib/api'
import { getGroups as getUserGroups } from '@/features/users/api'
import type { HeaderProfile } from './lib/header-profile-utils'
import type {
  AddChannelRequest,
  BatchDeleteParams,
  BatchSetTagParams,
  Channel,
  ChannelBalanceResponse,
  ChannelTestResponse,
  ChannelRouteScope,
  ChannelRouteScopeDetailResponse,
  ChannelRouteScopeMetricsResponse,
  ChannelRouteScopeResponse,
  ChannelRoutingConfigResponse,
  CopyChannelParams,
  CopyChannelResponse,
  FetchModelsResponse,
  GetChannelResponse,
  GetChannelsParams,
  GetChannelsResponse,
  MultiKeyManageParams,
  MultiKeyStatusResponse,
  SearchChannelsParams,
  SearchChannelsResponse,
  TagOperationParams,
} from './types'

// Extended API config types
export interface ExtendedApiConfig extends AxiosRequestConfig {
  skipBusinessError?: boolean
  skipErrorHandler?: boolean
  disableDuplicate?: boolean
}

export const channelActionConfig = (
  config: ExtendedApiConfig = {}
): ExtendedApiConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

export type CodexOAuthStartResponse = {
  success: boolean
  message?: string
  data?: {
    authorize_url?: string
  }
}

export type CodexOAuthCompleteResponse = {
  success: boolean
  message?: string
  data?: {
    key?: string
    account_id?: string
    email?: string
    expires_at?: string
    last_refresh?: string
  }
}

export type CodexUsageResponse = {
  success: boolean
  message?: string
  upstream_status?: number
  data?: Record<string, unknown>
}

export type CodexCredentialRefreshResponse = {
  success: boolean
  message?: string
  data?: {
    expires_at?: string
    last_refresh?: string
    account_id?: string
    email?: string
    channel_id?: number
    channel_type?: number
    channel_name?: string
  }
}

export type UserHeaderProfilesResponse = {
  success: boolean
  message?: string
  data?: HeaderProfile[]
}

// ============================================================================
// Base Channel CRUD Operations
// ============================================================================

/**
 * Get paginated list of channels
 */
export async function getChannels(
  params: GetChannelsParams = {}
): Promise<GetChannelsResponse> {
  const res = await api.get('/api/channel', { params })
  return res.data
}

/**
 * Search channels with filters
 */
export async function searchChannels(
  params: SearchChannelsParams
): Promise<SearchChannelsResponse> {
  const res = await api.get('/api/channel/search', { params })
  return res.data
}

/**
 * Get single channel by ID
 */
export async function getChannel(id: number): Promise<GetChannelResponse> {
  const res = await api.get(`/api/channel/${id}`)
  return res.data
}

export async function getTopChannelPriorities(): Promise<{
  success: boolean
  message?: string
  data?: Array<Pick<Channel, 'id' | 'priority'>>
}> {
  const res = await api.get('/api/channel/priorities')
  return res.data
}

export async function getTopChannelPinOrders(): Promise<{
  success: boolean
  message?: string
  data?: Array<Pick<Channel, 'id' | 'pin_order'>>
}> {
  const res = await api.get('/api/channel/pins')
  return res.data
}

export async function getChannelRouteScopes(
  params: {
    group?: string
    model?: string
  } = {}
): Promise<ChannelRouteScopeResponse> {
  const res = await api.get('/api/channel-route-scopes', { params })
  return res.data
}

export async function getChannelRoutingConfig(): Promise<ChannelRoutingConfigResponse> {
  const res = await api.get('/api/channel-route-scopes/config')
  return res.data
}

export async function getChannelRouteScopeMetrics(
  scopeId: string
): Promise<ChannelRouteScopeMetricsResponse> {
  const res = await api.get(`/api/channel-route-scopes/${scopeId}/metrics`)
  return res.data
}

export async function getChannelRouteScope(
  scopeId: string
): Promise<ChannelRouteScopeDetailResponse> {
  const res = await api.get(`/api/channel-route-scopes/${scopeId}`)
  return res.data
}

export async function updateChannelRouteScopeWeights(
  scopeId: string,
  payload: {
    version: string
    mode?: string
    items: Array<{ channel_id: number; route_weight_bps: number }>
  }
): Promise<{
  success: boolean
  message?: string
  data?: { scope: ChannelRouteScope; scope_version: string }
}> {
  const res = await api.put(
    `/api/channel-route-scopes/${scopeId}/weights`,
    payload,
    channelActionConfig()
  )
  return res.data
}

export async function updateChannelRouteScopePriority(
  scopeId: string,
  payload: { version: string; route_priority: number }
): Promise<{
  success: boolean
  message?: string
  data?: { scope: ChannelRouteScope; scope_version: string }
}> {
  const res = await api.put(
    `/api/channel-route-scopes/${scopeId}/priority`,
    payload,
    channelActionConfig()
  )
  return res.data
}

export async function getUserHeaderProfiles(): Promise<UserHeaderProfilesResponse> {
  const res = await api.get('/api/user/header_profiles')
  return res.data
}

/**
 * Payload for creating or updating a user header profile.
 * Mirrors controller.userHeaderProfileUpsertRequest.
 */
export interface HeaderProfilePayload {
  name: string
  category: string
  headers: Record<string, string>
  description: string
}

export async function createUserHeaderProfile(
  payload: HeaderProfilePayload
): Promise<UserHeaderProfilesResponse> {
  const res = await api.post('/api/user/header_profiles', payload)
  return res.data
}

export async function updateUserHeaderProfile(
  id: string,
  payload: HeaderProfilePayload
): Promise<UserHeaderProfilesResponse> {
  const res = await api.put(`/api/user/header_profiles/${encodeURIComponent(id)}`, payload)
  return res.data
}

/**
 * Legacy header_override template. Mirrors controller.buildUserHeaderTemplateResponse.
 */
export interface UserHeaderTemplate {
  id: number
  name: string
  content: string
  created_at: number
  updated_at: number
}

export type UserHeaderTemplateResponse = {
  success: boolean
  message?: string
  data?: UserHeaderTemplate
}

export type UserHeaderTemplateListResponse = {
  success: boolean
  message?: string
  data?: UserHeaderTemplate[]
}

export async function getUserHeaderTemplates(): Promise<UserHeaderTemplateListResponse> {
  const res = await api.get('/api/user/header-templates')
  return res.data
}

export async function createUserHeaderTemplate(
  payload: { name: string; content: string }
): Promise<UserHeaderTemplateResponse> {
  const res = await api.post('/api/user/header-templates', payload)
  return res.data
}

export async function updateUserHeaderTemplate(
  id: number,
  payload: { name: string; content: string }
): Promise<UserHeaderTemplateResponse> {
  const res = await api.put(`/api/user/header-templates/${encodeURIComponent(String(id))}`, payload)
  return res.data
}

export async function deleteUserHeaderTemplate(
  id: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(`/api/user/header-templates/${encodeURIComponent(String(id))}`)
  return res.data
}

/**
 * Create new channel(s)
 * Supports single, batch, and multi-key modes
 */
export async function createChannel(
  data: AddChannelRequest
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post('/api/channel', data, channelActionConfig())
  return res.data
}

/**
 * Update existing channel
 */
export async function updateChannel(
  id: number,
  data: Partial<Channel>
): Promise<{ success: boolean; message?: string; data?: Channel }> {
  const res = await api.put(
    '/api/channel/',
    { id, ...data },
    channelActionConfig()
  )
  return res.data
}

export async function updateChannelPriorities(
  updates: Array<{ id: number; priority: number }>
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.put(
    '/api/channel/priorities',
    updates,
    channelActionConfig()
  )
  return res.data
}

export async function updateChannelPinOrders(
  updates: Array<{ id: number; pin_order: number }>
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.put('/api/channel/pins', updates, channelActionConfig())
  return res.data
}

/**
 * Delete single channel
 */
export async function deleteChannel(
  id: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(`/api/channel/${id}`, channelActionConfig())
  return res.data
}

/**
 * Batch delete channels
 */
export async function batchDeleteChannels(
  data: BatchDeleteParams
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post('/api/channel/batch', data, channelActionConfig())
  return res.data
}

/**
 * Batch set tag for channels
 */
export async function batchSetChannelTag(
  data: BatchSetTagParams
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post(
    '/api/channel/batch/tag',
    data,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// Channel Operations
// ============================================================================

/**
 * Test channel connectivity
 */
export async function testChannel(
  id: number,
  params?: { model?: string; endpoint_type?: string; stream?: boolean }
): Promise<ChannelTestResponse> {
  const res = await api.get(
    `/api/channel/test/${id}`,
    channelActionConfig({ params })
  )
  return res.data
}

/**
 * Update channel balance
 */
export async function updateChannelBalance(
  id: number
): Promise<ChannelBalanceResponse> {
  const res = await api.get(
    `/api/channel/update_balance/${id}`,
    channelActionConfig()
  )
  return res.data
}

/**
 * Fetch available models from upstream provider
 */
export async function fetchUpstreamModels(
  id: number
): Promise<FetchModelsResponse> {
  const res = await api.get(
    `/api/channel/fetch_models/${id}`,
    channelActionConfig()
  )
  return res.data
}

/**
 * Copy/clone a channel
 */
export async function copyChannel(
  id: number,
  params: CopyChannelParams = {}
): Promise<CopyChannelResponse> {
  const res = await api.post(
    `/api/channel/copy/${id}`,
    null,
    channelActionConfig({ params })
  )
  return res.data
}

/**
 * Fix channel abilities
 */
export async function fixChannelAbilities(): Promise<{
  success: boolean
  message?: string
  data?: { success: number; fails: number }
}> {
  const res = await api.post(
    '/api/channel/fix',
    undefined,
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete all disabled channels
 */
export async function deleteDisabledChannels(): Promise<{
  success: boolean
  message?: string
  data?: number
}> {
  const res = await api.delete('/api/channel/disabled', channelActionConfig())
  return res.data
}

/**
 * Get channel key (requires 2FA verification)
 */
export async function getChannelKey(
  id: number,
  code?: string
): Promise<{ success: boolean; message?: string; data?: { key: string } }> {
  const payload = code ? { code } : undefined
  const res = await api.post(
    `/api/channel/${id}/key`,
    payload,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// Codex Channel Operations
// ============================================================================

export async function startCodexOAuth(): Promise<CodexOAuthStartResponse> {
  const res = await api.post(
    '/api/channel/codex/oauth/start',
    {},
    channelActionConfig()
  )
  return res.data
}

export async function completeCodexOAuth(
  input: string
): Promise<CodexOAuthCompleteResponse> {
  const res = await api.post(
    '/api/channel/codex/oauth/complete',
    { input },
    channelActionConfig()
  )
  return res.data
}

export async function refreshCodexCredential(
  channelId: number
): Promise<CodexCredentialRefreshResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/codex/refresh`,
    {},
    channelActionConfig()
  )
  return res.data
}

export async function getCodexUsage(
  channelId: number
): Promise<CodexUsageResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/codex/usage`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

// ============================================================================
// Multi-Key Management
// ============================================================================

/**
 * Manage multi-key channel operations
 */
export async function manageMultiKeys(
  params: MultiKeyManageParams
): Promise<MultiKeyStatusResponse | { success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/multi_key/manage',
    params,
    channelActionConfig()
  )
  return res.data
}

/**
 * Get key status for multi-key channel
 */
export async function getMultiKeyStatus(
  channelId: number,
  page = 1,
  pageSize = 50,
  status?: number
): Promise<MultiKeyStatusResponse> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'get_key_status',
    page,
    page_size: pageSize,
    status,
  }) as Promise<MultiKeyStatusResponse>
}

/**
 * Enable a specific key in multi-key channel
 */
export async function enableMultiKey(
  channelId: number,
  keyIndex: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'enable_key',
    key_index: keyIndex,
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Disable a specific key in multi-key channel
 */
export async function disableMultiKey(
  channelId: number,
  keyIndex: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'disable_key',
    key_index: keyIndex,
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Delete a specific key in multi-key channel
 */
export async function deleteMultiKey(
  channelId: number,
  keyIndex: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'delete_key',
    key_index: keyIndex,
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Enable all keys in multi-key channel
 */
export async function enableAllMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'enable_all_keys',
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Disable all keys in multi-key channel
 */
export async function disableAllMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'disable_all_keys',
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Delete all disabled keys in multi-key channel
 */
export async function deleteDisabledMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string; data?: number }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'delete_disabled_keys',
  }) as Promise<{ success: boolean; message?: string; data?: number }>
}

// ============================================================================
// Tag Operations
// ============================================================================

/**
 * Enable all channels with a specific tag
 */
export async function enableTagChannels(
  tag: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/tag/enabled',
    { tag },
    channelActionConfig()
  )
  return res.data
}

/**
 * Disable all channels with a specific tag
 */
export async function disableTagChannels(
  tag: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/tag/disabled',
    { tag },
    channelActionConfig()
  )
  return res.data
}

/**
 * Edit all channels with a specific tag
 */
export async function editTagChannels(
  params: TagOperationParams
): Promise<{ success: boolean; message?: string }> {
  const res = await api.put('/api/channel/tag', params, channelActionConfig())
  return res.data
}

/**
 * Get models for a specific tag
 */
export async function getTagModels(
  tag: string
): Promise<{ success: boolean; message?: string; data?: string }> {
  const res = await api.get('/api/channel/tag/models', { params: { tag } })
  return res.data
}

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Fetch models from a custom endpoint (for testing before creating channel)
 */
export async function fetchModels(data: {
  base_url: string
  type: number
  key: string
}): Promise<FetchModelsResponse> {
  const res = await api.post(
    '/api/channel/fetch_models',
    data,
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete an Ollama model from a channel
 */
export async function deleteOllamaModel(params: {
  channel_id: number
  model_name: string
}): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(
    '/api/channel/ollama/delete',
    channelActionConfig({ data: params })
  )
  return res.data
}

/**
 * Test all enabled channels
 */
export async function testAllChannels(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.get('/api/channel/test', channelActionConfig())
  return res.data
}

/**
 * Update balance for all enabled channels
 */
export async function updateAllChannelsBalance(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.get(
    '/api/channel/update_balance',
    channelActionConfig()
  )
  return res.data
}

/**
 * Get all available models
 */
export async function getAllModels(): Promise<{
  success: boolean
  message?: string
  data?: Array<{ id: string; [key: string]: unknown }>
}> {
  const res = await api.get('/api/channel/models')
  return res.data
}

/**
 * Get all enabled models
 */
export async function getEnabledModels(): Promise<{
  success: boolean
  message?: string
  data?: string[]
}> {
  const res = await api.get('/api/channel/models_enabled')
  return res.data
}

// ============================================================================
// Ollama Utilities
// ============================================================================

/**
 * Check Ollama version for a given channel
 */
export async function getOllamaVersion(
  channelId: number
): Promise<{ success: boolean; message?: string; data?: { version: string } }> {
  const res = await api.get(`/api/channel/ollama/version/${channelId}`)
  return res.data
}

// ============================================================================
// Group Management
// ============================================================================

/**
 * Get all available groups (re-exported from users API for convenience)
 */
export const getGroups = getUserGroups

// ============================================================================
// Prefill Groups (Model Groups)
// ============================================================================

/**
 * Get prefill groups for quick model selection
 */
export async function getPrefillGroups(
  type: 'model' | 'group' = 'model'
): Promise<{
  success: boolean
  message?: string
  data?: Array<{ id: number; name: string; items: string | string[] }>
}> {
  const res = await api.get('/api/prefill_group', { params: { type } })
  return res.data
}
