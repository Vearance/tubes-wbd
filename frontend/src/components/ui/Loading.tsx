import { m, useReducedMotion } from 'motion/react'
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
  const reduceMotion = useReducedMotion()

  return (
    <m.div
      initial={reduceMotion ? false : { opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={reduceMotion ? undefined : { opacity: 0 }}
      transition={{ duration: 0.22, ease: [0.16, 1, 0.3, 1] }}
      className={cn(
        'flex items-center justify-center text-xs select-none',
        page
          ? 'min-h-[calc(65dvh-42px)] flex-col gap-3.5 text-center'
          : 'min-h-36 flex-row gap-3',
        className,
      )}
      role="status"
      aria-live="polite"
    >
      <div className="relative grid size-10 place-items-center rounded-2xl border border-line/80 bg-white shadow-2xs">
        <svg
          className="size-5 animate-spin text-violet"
          xmlns="http://www.w3.org/2000/svg"
          fill="none"
          viewBox="0 0 24 24"
        >
          <circle
            className="stroke-violet/15"
            cx="12"
            cy="12"
            r="9.5"
            strokeWidth="2.5"
          />
          <path
            className="stroke-violet"
            d="M12 2.5 A 9.5 9.5 0 0 1 21.5 12"
            strokeWidth="2.5"
            strokeLinecap="round"
          />
        </svg>
      </div>
      <span className="font-medium tracking-tight text-ink/75">{label}</span>
    </m.div>
  )
}
