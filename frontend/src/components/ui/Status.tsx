import type { ReactNode } from 'react'
import { cn } from '../../lib/utils'

const statusTones = {
  violet: 'bg-violet-soft text-violet border-violet/20',
  accent: 'bg-accent-soft text-accent border-accent/20',
  green: 'bg-success-soft text-success border-success/20',
  amber: 'bg-warning-soft text-warning border-warning/20',
  red: 'bg-danger-soft text-danger border-danger/20',
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
        'inline-flex items-center rounded-full border px-2.5 py-0.5 text-[10px] font-bold tracking-tight select-none',
        statusTones[tone],
        className,
      )}
    >
      {children}
    </span>
  )
}
