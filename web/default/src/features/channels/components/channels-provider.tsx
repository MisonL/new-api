/* eslint-disable react-refresh/only-export-components */
import React, { createContext, useContext, useState, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { useChannelHealthRuntime } from '@/features/channel-health/use-channel-health-runtime'
import { getChannelRoutingConfig } from '../api'
import { useChannelUpstreamUpdates } from '../hooks/use-channel-upstream-updates'
import { channelsQueryKeys } from '../lib'
import type { Channel } from '../types'

// ============================================================================
// Types
// ============================================================================

type DialogType =
  | 'create-channel'
  | 'update-channel'
  | 'test-channel'
  | 'balance-query'
  | 'fetch-models'
  | 'ollama-models'
  | 'multi-key-manage'
  | 'tag-batch-edit'
  | 'edit-tag'
  | 'copy-channel'
  | null

type UpstreamUpdateState = ReturnType<typeof useChannelUpstreamUpdates>

const channelsRoute = getRouteApi('/_authenticated/channels/')

type ChannelsContextType = {
  open: DialogType
  setOpen: (open: DialogType) => void
  currentRow: Channel | null
  setCurrentRow: (row: Channel | null) => void
  currentTag: string | null
  setCurrentTag: (tag: string | null) => void
  enableTagMode: boolean
  setEnableTagMode: (enabled: boolean) => void
  idSort: boolean
  setIdSort: (enabled: boolean) => void
  upstream: UpstreamUpdateState
  canViewChannelHealthRuntime: boolean
  canConfigureChannelHealthRuntime: boolean
  channelHealthRuntime: ReturnType<typeof useChannelHealthRuntime>
  runtimeChannelFocus: number | undefined
  channelRoutingConfig: Awaited<
    ReturnType<typeof getChannelRoutingConfig>
  >['data']
}

// ============================================================================
// Context
// ============================================================================

const ChannelsContext = createContext<ChannelsContextType | undefined>(
  undefined
)

// ============================================================================
// Provider
// ============================================================================

export function ChannelsProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState<DialogType>(null)
  const [currentRow, setCurrentRow] = useState<Channel | null>(null)
  const [currentTag, setCurrentTag] = useState<string | null>(null)
  const [enableTagModePreference, setEnableTagModePreference] = useState(() => {
    return localStorage.getItem('enable-tag-mode') === 'true'
  })
  const [idSort, setIdSort] = useState(() => {
    return localStorage.getItem('channels-id-sort') === 'true'
  })
  const channelSearch = channelsRoute.useSearch()
  const runtimeChannelFocus = channelSearch.runtimeChannelFocus
  const enableTagMode =
    runtimeChannelFocus === undefined ? enableTagModePreference : false
  const userRole = useAuthStore((state) => state.auth.user?.role ?? ROLE.GUEST)
  const canViewChannelHealthRuntime = userRole >= ROLE.ADMIN
  const canConfigureChannelHealthRuntime = userRole >= ROLE.SUPER_ADMIN
  const channelHealthRuntime = useChannelHealthRuntime({
    enabled: canViewChannelHealthRuntime,
  })
  const { data: channelRoutingConfigResponse } = useQuery({
    queryKey: ['channel-routing-config'],
    queryFn: getChannelRoutingConfig,
    staleTime: 30_000,
  })

  const queryClient = useQueryClient()
  const refreshChannels = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: channelsQueryKeys.all })
  }, [queryClient])
  const upstream = useChannelUpstreamUpdates(refreshChannels)

  return (
    <ChannelsContext.Provider
      value={{
        open,
        setOpen,
        currentRow,
        setCurrentRow,
        currentTag,
        setCurrentTag,
        enableTagMode,
        setEnableTagMode: setEnableTagModePreference,
        idSort,
        setIdSort,
        upstream,
        canViewChannelHealthRuntime,
        canConfigureChannelHealthRuntime,
        channelHealthRuntime,
        runtimeChannelFocus,
        channelRoutingConfig: channelRoutingConfigResponse?.data,
      }}
    >
      {children}
    </ChannelsContext.Provider>
  )
}

// ============================================================================
// Hook
// ============================================================================

export function useChannels() {
  const context = useContext(ChannelsContext)
  if (!context) {
    throw new Error('useChannels must be used within ChannelsProvider')
  }
  return context
}
