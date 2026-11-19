import { Icon } from '../Icon'
import { cn } from '../../lib/utils'

export type LoadingProps = {
  page?: boolean
  className?: string
  label?: string
}

export function Loading({
  page = false,
  className,
  label = 'Loading workspace…',
}: LoadingProps) {
  return (
    <div
      className={cn(
        'flex min-h-60 items-center justify-center gap-3 text-xs text-muted',
        page &&
          'min-h-[calc(100dvh-42px)] flex-col text-center max-[900px]:min-h-[55dvh]',
        className,
      )}
      role="status"
    >
      <span className="grid size-10 place-items-center rounded-xl bg-violet-soft text-violet shadow-xs">
        <Icon name="database" className="size-5 animate-pulse" />
      </span>
      <span className="font-medium">{label}</span>
    </div>
  )
}
