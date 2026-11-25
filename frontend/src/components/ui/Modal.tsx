import { Dialog } from '@base-ui/react/dialog'
import type { ReactNode } from 'react'
import { Icon } from '../Icon'
import { cn } from '../../lib/utils'

export type ModalProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  children: ReactNode
  size?: 'sm' | 'md' | 'lg' | 'xl' | '2xl'
  className?: string
}

const sizeClasses = {
  sm: 'w-[min(100%,420px)]',
  md: 'w-[min(100%,520px)]',
  lg: 'w-[min(100%,680px)]',
  xl: 'w-[min(100%,820px)]',
  '2xl': 'w-[min(100%,960px)]',
}

export function Modal({
  open,
  onOpenChange,
  title,
  description,
  children,
  size = 'md',
  className,
}: ModalProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/40 backdrop-blur-xs transition-opacity duration-200" />
        <Dialog.Viewport className="fixed inset-0 z-50 grid place-items-center overflow-y-auto p-4 sm:p-6">
          <Dialog.Popup
            className={cn(
              'relative rounded-[20px] border border-line/80 bg-white shadow-2xl overflow-hidden',
              sizeClasses[size],
              className,
            )}
          >
            <div className="flex items-start justify-between gap-3 border-b border-line px-6 py-4.5 bg-canvas/30">
              <div className="min-w-0 flex-1">
                <Dialog.Title className="m-0 text-base font-bold text-ink truncate">
                  {title}
                </Dialog.Title>
                {description && (
                  <Dialog.Description className="mt-0.5 text-xs text-muted">
                    {description}
                  </Dialog.Description>
                )}
              </div>
              <Dialog.Close
                className="grid size-8 shrink-0 place-items-center rounded-xl border-0 bg-transparent text-muted hover:bg-canvas hover:text-ink transition-colors cursor-pointer"
                aria-label="Close dialog"
              >
                <Icon name="close" className="size-4" />
              </Dialog.Close>
            </div>
            <div className="p-6 max-h-[calc(100dvh-140px)] overflow-y-auto [scrollbar-width:thin]">
              {children}
            </div>
          </Dialog.Popup>
        </Dialog.Viewport>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
