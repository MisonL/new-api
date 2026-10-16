import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Bug, Settings2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { getUserModels, getUserGroups } from './api'
import { PlaygroundChat } from './components/playground-chat'
import { PlaygroundDebugPanel } from './components/playground-debug-panel'
import { PlaygroundInput } from './components/playground-input'
import { PlaygroundSettingsPanel } from './components/playground-settings-panel'
import { DEFAULT_GROUP } from './constants'
import {
  usePlaygroundState,
  useChatHandler,
  type PlaygroundDebugEvent,
} from './hooks'
import {
  buildChatCompletionPayload,
  createUserMessage,
  createLoadingAssistantMessage,
  normalizeStoredMessages,
  parseCustomRequestBody,
} from './lib'
import type {
  Message as MessageType,
  PlaygroundDebugData,
} from './types'

/** Request/response capture shown by the debug panel. */
interface DebugCapture {
  request: string
  response: string
  sseMessages: string[]
  timestamp: number
}

function createDebugCapture(): DebugCapture {
  return { request: '', response: '', sseMessages: [], timestamp: 0 }
}

export function Playground() {
  const { t } = useTranslation()
  const {
    config,
    parameterEnabled,
    messages,
    models,
    groups,
    updateMessages,
    updateParameterEnabled,
    setModels,
    setGroups,
    updateConfig,
    resetConfig,
    clearMessages,
  } = usePlaygroundState()

  const [settingsOpen, setSettingsOpen] = useState(false)
  const [debugCapture, setDebugCapture] =
    useState<DebugCapture>(createDebugCapture)

  const handleDebugEvent = useCallback((event: PlaygroundDebugEvent) => {
    setDebugCapture((prev) => {
      switch (event.kind) {
        case 'request':
          return {
            request: event.payload,
            response: '',
            sseMessages: [],
            timestamp: Date.now(),
          }
        case 'sse':
          return { ...prev, sseMessages: [...prev.sseMessages, event.data] }
        case 'reply':
          return { ...prev, response: event.text }
        default:
          return prev
      }
    })
  }, [])

  const { sendChat, stopGeneration, isGenerating } = useChatHandler({
    config,
    parameterEnabled,
    onMessageUpdate: updateMessages,
    onDebugEvent: handleDebugEvent,
  })

  // Edit dialog state
  const [editingMessageKey, setEditingMessageKey] = useState<string | null>(
    null
  )

  // Load models
  const { data: modelsData, isLoading: isLoadingModels } = useQuery({
    queryKey: ['playground-models'],
    queryFn: getUserModels,
  })

  // Load groups
  const { data: groupsData } = useQuery({
    queryKey: ['playground-groups'],
    queryFn: getUserGroups,
  })

  // Update models when data changes
  useEffect(() => {
    if (!modelsData) return

    setModels(modelsData)

    // Set default model if current model is not available
    const isCurrentModelValid = modelsData.some((m) => m.value === config.model)
    if (modelsData.length > 0 && !isCurrentModelValid) {
      updateConfig('model', modelsData[0].value)
    }
  }, [modelsData, config.model, setModels, updateConfig])

  // Update groups when data changes
  useEffect(() => {
    if (!groupsData) return

    // Add auto group if not present
    const hasAutoGroup = groupsData.some((g) => g.value === DEFAULT_GROUP)
    const processedGroups = hasAutoGroup
      ? groupsData
      : [
          {
            value: DEFAULT_GROUP,
            label: 'Auto',
            ratio: 1,
            desc: 'Circuit Breaker',
          },
          ...groupsData,
        ]

    setGroups(processedGroups)
  }, [groupsData, setGroups])

  // Both side panels read the payload that would be sent next. It is only
  // computed while one of them is open: otherwise every streamed token would
  // rebuild it for a panel nobody is looking at.
  const panelsOpen = settingsOpen || config.showDebugPanel

  const previewPayload = useMemo<Record<string, unknown> | null>(() => {
    if (!panelsOpen) return null
    if (config.customRequestMode) {
      return parseCustomRequestBody(config.customRequestBody)
    }
    return buildChatCompletionPayload(
      messages,
      config,
      parameterEnabled
    ) as unknown as Record<string, unknown>
  }, [panelsOpen, config, parameterEnabled, messages])

  // The panels show when the preview last changed. The clock is external state,
  // so the stamp is written from a frame callback rather than synchronously in
  // the effect body (which would cascade a render on every keystroke).
  const [previewTimestamp, setPreviewTimestamp] = useState(0)
  useEffect(() => {
    if (!panelsOpen || !previewPayload) return
    const handle = requestAnimationFrame(() => setPreviewTimestamp(Date.now()))
    return () => cancelAnimationFrame(handle)
  }, [panelsOpen, previewPayload])

  const debugData: PlaygroundDebugData = {
    request: debugCapture.request,
    response: debugCapture.response,
    sseMessages: debugCapture.sseMessages,
    timestamp: debugCapture.timestamp,
    previewTimestamp,
    isStreaming: isGenerating,
  }

  const handleImportMessages = useCallback(
    (value: unknown) => {
      if (!Array.isArray(value)) return
      updateMessages(normalizeStoredMessages(value as MessageType[]))
    },
    [updateMessages]
  )

  const handleSendMessage = (text: string) => {
    const userMessage = createUserMessage(text)
    const assistantMessage = createLoadingAssistantMessage()

    const newMessages = [...messages, userMessage, assistantMessage]
    updateMessages(newMessages)

    // Send chat request
    sendChat(newMessages)
  }

  const handleCopyMessage = (message: MessageType) => {
    // Copy is handled in MessageActions component
    void message
  }

  const handleRegenerateMessage = (message: MessageType) => {
    // Find the message index and regenerate from there
    const messageIndex = messages.findIndex((m) => m.key === message.key)
    if (messageIndex === -1) return

    // Remove messages after this one and regenerate
    const messagesUpToHere = messages.slice(0, messageIndex)
    const loadingMessage = createLoadingAssistantMessage()
    const newMessages = [...messagesUpToHere, loadingMessage]

    updateMessages(newMessages)
    sendChat(newMessages)
  }

  const handleEditMessage = useCallback((message: MessageType) => {
    setEditingMessageKey(message.key)
  }, [])

  const handleEditOpenChange = useCallback((open: boolean) => {
    if (!open) setEditingMessageKey(null)
  }, [])

  // Apply edit and optionally re-submit from the edited user message
  const applyEdit = useCallback(
    (newContent: string, submit: boolean) => {
      if (!editingMessageKey) return
      const index = messages.findIndex((m) => m.key === editingMessageKey)
      if (index === -1) return

      const updated = messages.map((m) =>
        m.key === editingMessageKey
          ? { ...m, versions: [{ ...m.versions[0], content: newContent }] }
          : m
      )

      setEditingMessageKey(null)

      if (!submit || updated[index].from !== 'user') {
        updateMessages(updated)
        return
      }

      const toSubmit = [
        ...updated.slice(0, index + 1),
        createLoadingAssistantMessage(),
      ]
      updateMessages(toSubmit)
      sendChat(toSubmit)
    },
    [editingMessageKey, messages, updateMessages, sendChat]
  )

  const handleDeleteMessage = (message: MessageType) => {
    const newMessages = messages.filter((m) => m.key !== message.key)
    updateMessages(newMessages)
  }

  return (
    <div className='relative flex size-full flex-col overflow-hidden'>
      <div className='flex shrink-0 items-center justify-end gap-1 border-b px-2 py-1.5'>
        <Button
          type='button'
          variant={settingsOpen ? 'secondary' : 'ghost'}
          size='icon-sm'
          aria-label={t('Playground settings')}
          aria-pressed={settingsOpen}
          onClick={() => setSettingsOpen((open) => !open)}
        >
          <Settings2 className='size-4' />
        </Button>
        <Button
          type='button'
          variant={config.showDebugPanel ? 'secondary' : 'ghost'}
          size='icon-sm'
          aria-label={t('Debug')}
          aria-pressed={config.showDebugPanel}
          onClick={() => updateConfig('showDebugPanel', !config.showDebugPanel)}
        >
          <Bug className='size-4' />
        </Button>
      </div>

      <div className='flex min-h-0 flex-1 flex-col overflow-hidden lg:flex-row'>
        {/* Chat column: full width until a side panel takes its own column */}
        <div className='flex min-h-0 flex-1 flex-col overflow-hidden'>
          {/* Full-width scroll container: scrolling works even over side whitespace */}
          <div className='flex flex-1 flex-col overflow-hidden'>
            <PlaygroundChat
              messages={messages}
              onCopyMessage={handleCopyMessage}
              onRegenerateMessage={handleRegenerateMessage}
              onEditMessage={handleEditMessage}
              onDeleteMessage={handleDeleteMessage}
              isGenerating={isGenerating}
              editingKey={editingMessageKey}
              onCancelEdit={handleEditOpenChange}
              onSaveEdit={(newContent) => applyEdit(newContent, false)}
              onSaveEditAndSubmit={(newContent) => applyEdit(newContent, true)}
            />
          </div>

          {/* Input area: center content and constrain to the same container width */}
          <div className='mx-auto w-full max-w-4xl'>
            <PlaygroundInput
              disabled={isGenerating}
              groups={groups}
              groupValue={config.group}
              isGenerating={isGenerating}
              isModelLoading={isLoadingModels}
              modelValue={config.model}
              models={models}
              onGroupChange={(value) => updateConfig('group', value)}
              onModelChange={(value) => updateConfig('model', value)}
              onStop={stopGeneration}
              onSubmit={handleSendMessage}
            />
          </div>
        </div>

        {settingsOpen && (
          <section className='bg-muted/20 flex min-h-0 shrink-0 flex-col overflow-hidden border-t lg:w-80 lg:border-t-0 lg:border-l xl:w-96'>
            <PlaygroundSettingsPanel
              config={config}
              parameterEnabled={parameterEnabled}
              messages={messages}
              models={models}
              groups={groups}
              previewPayload={previewPayload}
              onConfigChange={updateConfig}
              onParameterToggle={updateParameterEnabled}
              onImportMessages={handleImportMessages}
              onReset={resetConfig}
              onClearMessages={clearMessages}
              onCustomRequestModeChange={(enabled) =>
                updateConfig('customRequestMode', enabled)
              }
            />
          </section>
        )}

        {config.showDebugPanel && (
          <div className='flex min-h-0 shrink-0 flex-col overflow-hidden lg:w-[26rem]'>
            <PlaygroundDebugPanel
              data={debugData}
              previewPayload={previewPayload}
              onClose={() => updateConfig('showDebugPanel', false)}
            />
          </div>
        )}
      </div>
    </div>
  )
}
