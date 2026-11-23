import { AlertDialog } from '@base-ui/react/alert-dialog'
import { useState, type ReactNode } from 'react'
import { cn } from '../../lib/utils'
import { baseButtonClasses, buttonSizes, buttonVariants } from './buttonStyles'

export type ConfirmActionProps = {
  trigger: ReactNode
  title: string
  description: string
  confirmLabel: string
  onConfirm: () => void | Promise<void>
  triggerClassName?: string
  triggerAriaLabel?: string
  tone?: 'danger' | 'primary'
}

export function ConfirmAction({
  trigger,
  title,
  description,
  confirmLabel,
  onConfirm,
  triggerClassName,
  triggerAriaLabel,
  tone = 'danger',
}: ConfirmActionProps) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  async function handleConfirm() {
    try {
      setBusy(true)
      await onConfirm()
      setOpen(false)
    } finally {
      setBusy(false)
    }
  }

  return (
    <AlertDialog.Root open={open} onOpenChange={setOpen}>
      <AlertDialog.Trigger
        className={
          triggerClassName ??
          cn(baseButtonClasses, buttonVariants.default, buttonSizes.md)
        }
        aria-label={triggerAriaLabel}
      >
        {trigger}
      </AlertDialog.Trigger>

      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 z-50 bg-black/40 backdrop-blur-xs" />
        <AlertDialog.Viewport className="fixed inset-0 z-50 grid place-items-center overflow-auto p-4 sm:p-6">
          <AlertDialog.Popup className="w-[min(100%,440px)] rounded-[16px] border border-line bg-surface p-6 shadow-2xl">
            <AlertDialog.Title className="m-0 text-balance text-lg font-bold text-ink">
              {title}
            </AlertDialog.Title>
            <AlertDialog.Description className="mt-2.5 mb-0 text-pretty text-xs leading-relaxed text-muted">
              {description}
            </AlertDialog.Description>
            <div className="mt-6 flex justify-end gap-2.5 max-[600px]:flex-col-reverse max-[600px]:items-stretch">
              <AlertDialog.Close
                className={cn(baseButtonClasses, buttonVariants.default, buttonSizes.md)}
                disabled={busy}
              >
                Cancel
              </AlertDialog.Close>
              <button
                type="button"
                className={cn(
                  baseButtonClasses,
                  tone === 'danger' ? buttonVariants.danger : buttonVariants.dark,
                  buttonSizes.md,
                )}
                disabled={busy}
                onClick={handleConfirm}
              >
                {busy ? (
                  <>
                    <span className="size-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
                    <span>Processing…</span>
                  </>
                ) : (
                  confirmLabel
                )}
              </button>
            </div>
          </AlertDialog.Popup>
        </AlertDialog.Viewport>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  )
}
