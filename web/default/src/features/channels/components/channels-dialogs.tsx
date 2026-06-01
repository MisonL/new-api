import { LazyMount } from '@/components/lazy-mount'
import { useChannels } from './channels-provider'
import { BalanceQueryDialog } from './dialogs/balance-query-dialog'
import { ChannelTestDialog } from './dialogs/channel-test-dialog'
import { CopyChannelDialog } from './dialogs/copy-channel-dialog'
import { EditTagDialog } from './dialogs/edit-tag-dialog'
import { FetchModelsDialog } from './dialogs/fetch-models-dialog'
import { MultiKeyManageDialog } from './dialogs/multi-key-manage-dialog'
import { OllamaModelsDialog } from './dialogs/ollama-models-dialog'
import { TagBatchEditDialog } from './dialogs/tag-batch-edit-dialog'
import { UpstreamUpdateDialog } from './dialogs/upstream-update-dialog'
import { ChannelMutateDrawer } from './drawers/channel-mutate-drawer'

export function ChannelsDialogs() {
  const { open, setOpen, currentRow, upstream } = useChannels()

  return (
    <>
      {/* Channel Create/Update Drawer */}
      <LazyMount open={open === 'create-channel' || open === 'update-channel'}>
        <ChannelMutateDrawer
          open={open === 'create-channel' || open === 'update-channel'}
          onOpenChange={(v) => !v && setOpen(null)}
          currentRow={open === 'update-channel' ? currentRow : null}
        />
      </LazyMount>

      {/* Test Channel Dialog */}
      <LazyMount open={open === 'test-channel'}>
        <ChannelTestDialog
          open={open === 'test-channel'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Balance Query Dialog */}
      <LazyMount open={open === 'balance-query'}>
        <BalanceQueryDialog
          open={open === 'balance-query'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Fetch Models Dialog */}
      <LazyMount open={open === 'fetch-models'}>
        <FetchModelsDialog
          open={open === 'fetch-models'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Ollama Models Dialog */}
      <LazyMount open={open === 'ollama-models'}>
        <OllamaModelsDialog
          open={open === 'ollama-models'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Copy Channel Dialog */}
      <LazyMount open={open === 'copy-channel'}>
        <CopyChannelDialog
          open={open === 'copy-channel'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Multi-Key Management Dialog */}
      <LazyMount open={open === 'multi-key-manage'}>
        <MultiKeyManageDialog
          open={open === 'multi-key-manage'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Tag Batch Edit Dialog */}
      <LazyMount open={open === 'tag-batch-edit'}>
        <TagBatchEditDialog
          open={open === 'tag-batch-edit'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Edit Tag Dialog */}
      <LazyMount open={open === 'edit-tag'}>
        <EditTagDialog
          open={open === 'edit-tag'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Upstream Model Update Dialog */}
      <LazyMount open={upstream.showModal}>
        <UpstreamUpdateDialog
          open={upstream.showModal}
          addModels={upstream.addModels}
          removeModels={upstream.removeModels}
          preferredTab={upstream.preferredTab}
          confirmLoading={upstream.applyLoading}
          onConfirm={upstream.applyUpdates}
          onCancel={upstream.closeModal}
        />
      </LazyMount>
    </>
  )
}
