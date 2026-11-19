import type { ReactNode } from 'react'
import { Icon, type IconName } from '../Icon'
import { cn } from '../../lib/utils'

export type EmptyStateProps = {
  icon?: IconName
  title: string
  children: ReactNode
  action?: ReactNode
  className?: string
}

export function EmptyState({
  icon = 'database',
  title,
  children,
  action,
  className,
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        'flex h-full min-h-[220px] w-full flex-col items-center justify-center p-8 text-center text-muted',
        className,
      )}
    >
      <span className="mb-3.5 grid size-12 place-items-center rounded-2xl border border-line bg-surface-alt text-violet shadow-sm">
        <Icon name={icon} className="size-6" />
      </span>
      <strong className="text-sm font-bold text-ink">{title}</strong>
      <p className="mt-1.5 mb-0 max-w-[420px] text-pretty text-xs leading-relaxed text-muted">
        {children}
      </p>
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}
