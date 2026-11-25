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
  sm: 'w-[min(100%,460px)]',
  md: 'w-[min(100%,560px)]',
  lg: 'w-[min(100%,720px)]',
  xl: 'w-[min(100%,860px)]',
  '2xl': 'w-[min(100%,1020px)]',
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
              'relative rounded-[22px] border border-line/80 bg-white shadow-2xl overflow-hidden',
              sizeClasses[size],
              className,
            )}
          >
            <div className="flex items-start justify-between gap-4 border-b border-line/80 px-7 py-6 sm:px-8 sm:py-6.5 bg-canvas/30">
              <div className="min-w-0 flex-1">
                <Dialog.Title className="m-0 text-xl font-bold tracking-tight text-ink sm:text-[23px]">
                  {title}
                </Dialog.Title>
                {description && (
                  <Dialog.Description className="mt-1.5 text-xs sm:text-sm text-muted leading-relaxed">
                    {description}
                  </Dialog.Description>
                )}
              </div>
              <Dialog.Close
                className="grid size-9 shrink-0 place-items-center rounded-xl border border-transparent bg-transparent text-muted hover:border-line hover:bg-canvas hover:text-ink transition-all cursor-pointer"
                aria-label="Close dialog"
              >
                <Icon name="close" className="size-4.5" />
              </Dialog.Close>
            </div>
            <div className="p-7 sm:p-8 max-h-[calc(100dvh-150px)] overflow-y-auto [scrollbar-width:thin]">
              {children}
            </div>
          </Dialog.Popup>
        </Dialog.Viewport>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
