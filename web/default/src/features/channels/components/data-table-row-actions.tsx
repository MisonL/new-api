import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { type Row } from '@tanstack/react-table'
import {
  ArrowDown,
  ArrowUp,
  MoreHorizontal,
  Boxes,
  Pencil,
  TestTube,
  Gauge,
  DollarSign,
  Download,
  Copy,
  Power,
  PowerOff,
  Key,
  Trash2,
  RefreshCw,
  Loader2,
  Pin,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LazyMount } from '@/components/lazy-mount'
import { MODEL_FETCHABLE_TYPES } from '../constants'
import {
  channelsQueryKeys,
  handleDeleteChannel,
  handleTestChannel,
  handleToggleChannelStatus,
  handleUpdateChannelField,
  handleUpdateChannelPriorityMovePlan,
  getNextTopChannelPriority,
  getTopChannelPriorityMovePlan,
  isTopChannel,
  isChannelEnabled,
  isMultiKeyChannel,
} from '../lib'
import { parseUpstreamUpdateMeta } from '../lib/upstream-update-utils'
import type { Channel } from '../types'
import { useChannels } from './channels-provider'

interface DataTableRowActionsProps {
  row: Row<Channel>
  getTopPriority: () => Promise<number>
  getTopChannels: () => Promise<Pick<Channel, 'id' | 'priority'>[]>
  topChannels?: Pick<Channel, 'id' | 'priority'>[]
}

export function DataTableRowActions({
  row,
  getTopPriority,
  getTopChannels,
  topChannels,
}: DataTableRowActionsProps) {
  const { t } = useTranslation()
  const channel = row.original
  const { setOpen, setCurrentRow, upstream } = useChannels()
  const queryClient = useQueryClient()
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [isTesting, setIsTesting] = useState(false)
  const [isTogglingStatus, setIsTogglingStatus] = useState(false)
  const [isPinning, setIsPinning] = useState(false)
  const [movingPinnedDirection, setMovingPinnedDirection] = useState<
    'up' | 'down' | null
  >(null)

  const isEnabled = isChannelEnabled(channel)
  const isMultiKey = isMultiKeyChannel(channel)
  const isPinned = isTopChannel(channel)
  const pinnedOrder = topChannels ?? []
  const hasPinnedOrder = pinnedOrder.length > 0
  const pinnedIndex = pinnedOrder.findIndex((item) => item.id === channel.id)
  const isPinnedFirst = isPinned && hasPinnedOrder && pinnedIndex === 0
  const isPinnedLast =
    isPinned &&
    hasPinnedOrder &&
    pinnedIndex >= 0 &&
    pinnedIndex === pinnedOrder.length - 1
  const isPriorityUpdating = isPinning || movingPinnedDirection !== null
  const getPinnedBoundaryMessage = (direction: 'up' | 'down') =>
    direction === 'up'
      ? t('This pinned channel is already at the top')
      : t('This pinned channel is already at the bottom')

  const handleEdit = () => {
    setCurrentRow(channel)
    setOpen('update-channel')
  }

  const handleTest = () => {
    setCurrentRow(channel)
    setOpen('test-channel')
  }

  const handleDirectTest = async (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation()
    setIsTesting(true)
    try {
      await handleTestChannel(channel.id, undefined, () => {
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
      })
    } finally {
      setIsTesting(false)
    }
  }

  const handleQueryBalance = () => {
    setCurrentRow(channel)
    setOpen('balance-query')
  }

  const handleFetchModels = () => {
    setCurrentRow(channel)
    setOpen('fetch-models')
  }

  const handleManageOllamaModels = () => {
    setCurrentRow(channel)
    setOpen('ollama-models')
  }

  const handleCopy = () => {
    setCurrentRow(channel)
    setOpen('copy-channel')
  }

  const handlePinToTop = async () => {
    if (isPriorityUpdating) return
    if (isPinnedFirst) {
      toast.info(getPinnedBoundaryMessage('up'))
      return
    }
    setIsPinning(true)
    try {
      const topPriority = await getTopPriority()
      const nextPriority = getNextTopChannelPriority(channel, [
        { priority: topPriority },
      ])
      await handleUpdateChannelField(
        channel.id,
        'priority',
        nextPriority,
        queryClient
      )
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to pin channel')
      )
    } finally {
      setIsPinning(false)
    }
  }

  const handleMovePinnedChannel = async (direction: 'up' | 'down') => {
    if (isPriorityUpdating) return
    if (!isPinned) {
      toast.info(t('Only pinned channels can be moved'))
      return
    }
    if (!hasPinnedOrder) {
      toast.error(t('Failed to load pinned channels'))
      return
    }
    if (
      (direction === 'up' && isPinnedFirst) ||
      (direction === 'down' && isPinnedLast)
    ) {
      toast.info(getPinnedBoundaryMessage(direction))
      return
    }

    setMovingPinnedDirection(direction)
    try {
      const latestTopChannels = await getTopChannels()
      const latestIndex = latestTopChannels.findIndex(
        (item) => item.id === channel.id
      )
      if (latestIndex < 0) {
        toast.error(t('Failed to load pinned channels'))
        return
      }
      const latestIsFirst = latestIndex === 0
      const latestIsLast = latestIndex === latestTopChannels.length - 1
      if (
        (direction === 'up' && latestIsFirst) ||
        (direction === 'down' && latestIsLast)
      ) {
        toast.info(getPinnedBoundaryMessage(direction))
        return
      }

      const plan = getTopChannelPriorityMovePlan(
        channel,
        latestTopChannels,
        direction
      )
      await handleUpdateChannelPriorityMovePlan(plan, queryClient)
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to update pinned channel order')
      )
    } finally {
      setMovingPinnedDirection(null)
    }
  }

  const handleManageKeys = () => {
    setCurrentRow(channel)
    setOpen('multi-key-manage')
  }

  const handleToggleStatus = async (
    e?: React.MouseEvent<HTMLButtonElement>
  ) => {
    e?.stopPropagation()
    setIsTogglingStatus(true)
    try {
      await handleToggleChannelStatus(channel.id, channel.status, queryClient)
    } finally {
      setIsTogglingStatus(false)
    }
  }

  return (
    <div className='flex items-center justify-end gap-0.5'>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant='ghost'
            size='icon-lg'
            onClick={handlePinToTop}
            disabled={isPriorityUpdating || isPinnedFirst}
            aria-label={t('Pin to Top')}
            className={
              isPinned
                ? 'text-primary hover:text-primary'
                : 'text-muted-foreground hover:text-foreground'
            }
          >
            {isPinning ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Pin className='size-4' />
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          {isPinning ? t('Pinning...') : t('Pin to Top')}
        </TooltipContent>
      </Tooltip>

      {isPinned && (
        <>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant='ghost'
                size='icon-lg'
                onClick={() => handleMovePinnedChannel('up')}
                disabled={
                  isPriorityUpdating || !hasPinnedOrder || isPinnedFirst
                }
                aria-label={t('Move pinned channel up')}
              >
                {movingPinnedDirection === 'up' ? (
                  <Loader2 className='size-4 animate-spin' />
                ) : (
                  <ArrowUp className='size-4' />
                )}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t('Move pinned channel up')}</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant='ghost'
                size='icon-lg'
                onClick={() => handleMovePinnedChannel('down')}
                disabled={isPriorityUpdating || !hasPinnedOrder || isPinnedLast}
                aria-label={t('Move pinned channel down')}
              >
                {movingPinnedDirection === 'down' ? (
                  <Loader2 className='size-4 animate-spin' />
                ) : (
                  <ArrowDown className='size-4' />
                )}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t('Move pinned channel down')}</TooltipContent>
          </Tooltip>
        </>
      )}

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant='ghost'
            size='icon-lg'
            onClick={handleDirectTest}
            disabled={isTesting}
            aria-label={t('Test Connection')}
          >
            {isTesting ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Gauge className='size-4' />
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t('Test Connection')}</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant='ghost'
            size='icon-lg'
            onClick={handleToggleStatus}
            disabled={isTogglingStatus}
            aria-label={isEnabled ? t('Disable') : t('Enable')}
            className={
              isEnabled
                ? 'text-destructive hover:text-destructive'
                : 'text-emerald-600 hover:text-emerald-600 dark:text-emerald-400 dark:hover:text-emerald-400'
            }
          >
            {isTogglingStatus ? (
              <Loader2 className='size-4 animate-spin' />
            ) : isEnabled ? (
              <PowerOff className='size-4' />
            ) : (
              <Power className='size-4' />
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          {isEnabled ? t('Disable') : t('Enable')}
        </TooltipContent>
      </Tooltip>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant='ghost'
            size='icon-lg'
            className='data-[state=open]:bg-muted'
          >
            <MoreHorizontal className='h-4 w-4' />
            <span className='sr-only'>{t('Open menu')}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' className='w-48'>
          {/* Pin to top */}
          <DropdownMenuItem
            onClick={handlePinToTop}
            disabled={isPriorityUpdating || isPinnedFirst}
          >
            {isPinning ? t('Pinning...') : t('Pin to Top')}
            <DropdownMenuShortcut>
              {isPinning ? (
                <Loader2 size={16} className='animate-spin' />
              ) : (
                <Pin size={16} />
              )}
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {isPinned && (
            <>
              <DropdownMenuItem
                onClick={() => handleMovePinnedChannel('up')}
                disabled={
                  isPriorityUpdating || !hasPinnedOrder || isPinnedFirst
                }
              >
                {t('Move Up')}
                <DropdownMenuShortcut>
                  {movingPinnedDirection === 'up' ? (
                    <Loader2 size={16} className='animate-spin' />
                  ) : (
                    <ArrowUp size={16} />
                  )}
                </DropdownMenuShortcut>
              </DropdownMenuItem>

              <DropdownMenuItem
                onClick={() => handleMovePinnedChannel('down')}
                disabled={isPriorityUpdating || !hasPinnedOrder || isPinnedLast}
              >
                {t('Move Down')}
                <DropdownMenuShortcut>
                  {movingPinnedDirection === 'down' ? (
                    <Loader2 size={16} className='animate-spin' />
                  ) : (
                    <ArrowDown size={16} />
                  )}
                </DropdownMenuShortcut>
              </DropdownMenuItem>
            </>
          )}

          <DropdownMenuSeparator />

          {/* Edit */}
          <DropdownMenuItem onClick={handleEdit}>
            {t('Edit')}
            <DropdownMenuShortcut>
              <Pencil size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {/* Test Connection */}
          <DropdownMenuItem onClick={handleTest}>
            {t('Test Connection')}
            <DropdownMenuShortcut>
              <TestTube size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {/* Query Balance */}
          <DropdownMenuItem onClick={handleQueryBalance}>
            {t('Query Balance')}
            <DropdownMenuShortcut>
              <DollarSign size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {/* Fetch Models */}
          <DropdownMenuItem onClick={handleFetchModels}>
            {t('Fetch Models')}
            <DropdownMenuShortcut>
              <Download size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {/* Detect Upstream Updates (only for fetchable channel types) */}
          {MODEL_FETCHABLE_TYPES.has(channel.type) && (
            <DropdownMenuItem
              onClick={() => {
                const meta = parseUpstreamUpdateMeta(channel.settings)
                if (
                  meta.pendingAddModels.length > 0 ||
                  meta.pendingRemoveModels.length > 0
                ) {
                  upstream.openModal(
                    channel,
                    meta.pendingAddModels,
                    meta.pendingRemoveModels,
                    meta.pendingAddModels.length > 0 ? 'add' : 'remove'
                  )
                } else {
                  upstream.detectChannelUpdates(channel)
                }
              }}
            >
              {t('Upstream Updates')}
              <DropdownMenuShortcut>
                <RefreshCw size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          {/* Ollama Models (only for Ollama channels) */}
          {channel.type === 4 && (
            <DropdownMenuItem onClick={handleManageOllamaModels}>
              {t('Manage Ollama Models')}
              <DropdownMenuShortcut>
                <Boxes size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          <DropdownMenuSeparator />

          {/* Copy Channel */}
          <DropdownMenuItem onClick={handleCopy}>
            {t('Copy Channel')}
            <DropdownMenuShortcut>
              <Copy size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {/* Manage Keys (only for multi-key channels) */}
          {isMultiKey && (
            <DropdownMenuItem onClick={handleManageKeys}>
              {t('Manage Keys')}
              <DropdownMenuShortcut>
                <Key size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          <DropdownMenuSeparator />

          {/* Delete */}
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault()
              setDeleteConfirmOpen(true)
            }}
            className='text-destructive focus:text-destructive'
          >
            {t('Delete')}
            <DropdownMenuShortcut>
              <Trash2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <LazyMount open={deleteConfirmOpen}>
        <ConfirmDialog
          open={deleteConfirmOpen}
          onOpenChange={setDeleteConfirmOpen}
          title={t('Delete Channel')}
          desc={`Are you sure you want to delete "${channel.name}"? This action cannot be undone.`}
          confirmText='Delete'
          destructive
          handleConfirm={() => {
            handleDeleteChannel(channel.id, queryClient)
            setDeleteConfirmOpen(false)
          }}
        />
      </LazyMount>
    </div>
  )
}
