import { useId } from 'react'
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
  const groupId = useId()

  return (
    <LayoutGroup id={groupId}>
      <div
        className={cn(
          'flex min-h-12 max-w-full items-center gap-1.5 overflow-x-auto overscroll-x-contain rounded-[18px] border border-line/80 bg-white p-1.5 shadow-2xs',
          className,
        )}
        role="tablist"
      >
        {tabs.map((tab) => {
          const active = tab.value === value
          return (
            <button
              className={cn(
                'relative min-h-9 min-w-20 shrink-0 rounded-xl border-0 bg-transparent px-4 text-xs font-semibold text-muted outline-none transition-colors duration-150 ease-out hover:text-ink focus-visible:ring-2 focus-visible:ring-violet/30 cursor-pointer',
                active ? 'font-bold text-violet' : 'hover:text-ink',
              )}
              type="button"
              role="tab"
              aria-selected={active}
              key={tab.value}
              onClick={() => onValueChange(tab.value)}
            >
              {active && (
                <m.span
                  className="absolute inset-0 rounded-xl bg-violet-soft border border-violet/25 shadow-2xs"
                  layoutId={`${groupId}-indicator`}
                  initial={false}
                  transition={reduceMotion ? { duration: 0 } : { duration: 0.18, ease: 'easeInOut' }}
                />
              )}
              <span className="relative z-1">{tab.label}</span>
            </button>
          )
        })}
      </div>
    </LayoutGroup>
  )
}
