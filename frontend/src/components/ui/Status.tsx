import type { ReactNode } from 'react'
import { cn } from '../../lib/utils'

const statusTones = {
  violet: 'bg-violet-soft text-violet-fg border-violet/20',
  accent: 'bg-accent-soft text-accent-fg border-accent/20',
  green: 'bg-success-soft text-success-fg border-success/25',
  amber: 'bg-warning-soft text-warning-fg border-warning/25',
  red: 'bg-danger-soft text-danger-fg border-danger/20',
  gray: 'bg-canvas text-muted border-line',
} as const

export type StatusTone = keyof typeof statusTones

export type StatusProps = {
  children: ReactNode
  tone?: StatusTone
  className?: string
}

export function Status({
  children,
  tone = 'violet',
  className,
}: StatusProps) {
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center whitespace-nowrap rounded-full border px-2.5 py-0.5 text-[11px] font-bold tracking-tight select-none',
        statusTones[tone],
        className,
      )}
    >
      {children}
    </span>
  )
}
