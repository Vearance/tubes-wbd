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
        'my-2.5 flex items-center justify-between gap-3 rounded-[10px] border border-danger/20 bg-danger-soft px-3.5 py-3 text-xs font-medium text-[#9b2936] shadow-xs',
        className,
      )}
      role="alert"
    >
      <div className="flex items-center gap-2 min-w-0">
        <span className="size-2 shrink-0 rounded-full bg-danger" />
        <span className="truncate">{message}</span>
      </div>
      {onDismiss && (
        <button
          type="button"
          onClick={onDismiss}
          className="size-5 shrink-0 rounded text-danger/70 hover:bg-danger/10 hover:text-danger grid place-items-center"
          aria-label="Dismiss error"
        >
          <Icon name="close" className="size-3" />
        </button>
      )}
    </div>
  )
}
