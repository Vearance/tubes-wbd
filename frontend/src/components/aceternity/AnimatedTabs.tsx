import { LayoutGroup, m, useReducedMotion } from 'motion/react'
import { cn } from '../../lib/utils'

type Tab = {
  label: string
  value: string
}

type AnimatedTabsProps = {
  tabs: Tab[]
  value: string
  onValueChange: (value: string) => void
  className?: string
}

export function AnimatedTabs({ tabs, value, onValueChange, className }: AnimatedTabsProps) {
  const reduceMotion = useReducedMotion()

  return <LayoutGroup id="workspace-tabs">
    <div className={cn('flex min-h-12 max-w-full gap-1 overflow-x-auto overscroll-x-contain border-b border-line p-1', className)} role="tablist">
      {tabs.map(tab => {
        const active = tab.value === value
        return <button
          className={cn(
            'relative min-h-10 min-w-24 shrink-0 rounded-lg border-0 bg-transparent px-3.5 text-xs font-medium text-muted outline-none transition-colors duration-150 ease-out hover:text-ink focus-visible:ring-2 focus-visible:ring-violet/30',
            active && 'text-violet',
          )}
          type="button"
          role="tab"
          aria-selected={active}
          key={tab.value}
          onClick={() => onValueChange(tab.value)}
        >
          {active && <m.span
            className="absolute inset-0 rounded-lg bg-violet-soft"
            layoutId="workspace-tab-indicator"
            initial={false}
            transition={reduceMotion ? { duration: 0 } : { duration: .18, ease: 'easeInOut' }}
          />}
          <span className="relative z-1">{tab.label}</span>
        </button>
      })}
    </div>
  </LayoutGroup>
}
