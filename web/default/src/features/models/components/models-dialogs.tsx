import { useRetainedValue } from '@/hooks/use-retained-value'
import { LazyMount } from '@/components/lazy-mount'
import { DescriptionDialog } from './dialogs/description-dialog'
import { MissingModelsDialog } from './dialogs/missing-models-dialog'
import { PrefillGroupManagement } from './dialogs/prefill-group-management'
import { SyncWizardDialog } from './dialogs/sync-wizard-dialog'
import { UpstreamConflictDialog } from './dialogs/upstream-conflict-dialog'
import { VendorMutateDialog } from './dialogs/vendor-mutate-dialog'
import { ModelMutateDrawer } from './drawers/model-mutate-drawer'
import { useModels } from './models-provider'

export function ModelsDialogs() {
  const {
    open,
    setOpen,
    currentRow,
    currentVendor,
    descriptionData,
    setDescriptionData,
  } = useModels()
  const retainedDescriptionData = useRetainedValue(
    descriptionData,
    open === 'description'
  )

  return (
    <>
      {/* Model Create/Update Drawer */}
      <LazyMount open={open === 'create-model' || open === 'update-model'}>
        <ModelMutateDrawer
          open={open === 'create-model' || open === 'update-model'}
          onOpenChange={(v) => !v && setOpen(null)}
          currentRow={currentRow}
        />
      </LazyMount>

      {/* Vendor Create/Update Dialog */}
      <LazyMount open={open === 'create-vendor' || open === 'update-vendor'}>
        <VendorMutateDialog
          open={open === 'create-vendor' || open === 'update-vendor'}
          onOpenChange={(v) => !v && setOpen(null)}
          currentVendor={open === 'update-vendor' ? currentVendor : null}
        />
      </LazyMount>

      {/* Missing Models Dialog */}
      <LazyMount open={open === 'missing-models'}>
        <MissingModelsDialog
          open={open === 'missing-models'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Sync Wizard Dialog */}
      <LazyMount open={open === 'sync-wizard'}>
        <SyncWizardDialog
          open={open === 'sync-wizard'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Upstream Conflict Dialog */}
      <LazyMount open={open === 'upstream-conflict'}>
        <UpstreamConflictDialog
          open={open === 'upstream-conflict'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Prefill Groups Management */}
      <LazyMount open={open === 'prefill-groups'}>
        <PrefillGroupManagement
          open={open === 'prefill-groups'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </LazyMount>

      {/* Description Dialog */}
      <LazyMount open={open === 'description'}>
        <DescriptionDialog
          open={open === 'description'}
          onOpenChange={(v) => {
            if (!v) {
              setOpen(null)
              setDescriptionData(null)
            }
          }}
          modelName={retainedDescriptionData?.modelName || ''}
          description={retainedDescriptionData?.description || ''}
        />
      </LazyMount>
    </>
  )
}
