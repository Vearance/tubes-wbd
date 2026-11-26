import { cn } from '../../lib/utils'

export function NullBadge({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        'inline-block rounded border border-line bg-canvas px-1.5 py-0.5 font-mono text-[11px] font-medium text-muted italic select-none',
        className,
      )}
    >
      NULL
    </span>
  )
}
