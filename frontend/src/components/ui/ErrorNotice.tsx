import { Icon } from '../Icon'
import { cn } from '../../lib/utils'

export type ErrorNoticeProps = {
  message: string
  className?: string
  onDismiss?: () => void
}

export function ErrorNotice({
  message,
  className,
  onDismiss,
}: ErrorNoticeProps) {
  return (
    <div
      className={cn(
        'my-2.5 flex items-start justify-between gap-3 rounded-xl border border-danger/20 bg-danger-soft px-3.5 py-3 text-xs font-medium text-danger-fg shadow-xs',
        className,
      )}
      role="alert"
    >
      <div className="flex items-start gap-2.5 min-w-0 flex-1">
        <span className="mt-1.5 size-2 shrink-0 rounded-full bg-danger" />
        <span className="min-w-0 break-words whitespace-pre-wrap leading-relaxed">
          {message}
        </span>
      </div>
      {onDismiss && (
        <button
          type="button"
          onClick={onDismiss}
          className="size-7 sm:size-6 shrink-0 rounded-lg text-danger-fg/80 hover:bg-danger/10 hover:text-danger-fg grid place-items-center"
          aria-label="Dismiss error"
        >
          <Icon name="close" className="size-3.5 sm:size-3" />
        </button>
      )}
    </div>
  )
}
