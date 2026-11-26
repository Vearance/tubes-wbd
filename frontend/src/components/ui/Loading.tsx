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

export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      className={cn('animate-pulse rounded-lg bg-line/70', className)}
      aria-hidden="true"
    />
  )
}

export type PageSkeletonVariant =
  | 'cards'
  | 'table'
  | 'schema'
  | 'split'
  | 'list'
  | 'admin-split'
  | 'metrics'

export function PageSkeleton({
  variant,
  label = 'Loading workspace…',
}: {
  variant: PageSkeletonVariant
  label?: string
}) {
  return (
    <div role="status" aria-live="polite" className="space-y-4">
      <span className="sr-only">{label}</span>

      {variant === 'cards' && (
        <div className="grid min-w-0 grid-cols-1 gap-3.5 sm:grid-cols-[repeat(auto-fill,minmax(280px,1fr))]">
          {Array.from({ length: 6 }, (_, i) => (
            <div
              key={i}
              className="rounded-2xl border border-line/80 bg-white p-5 shadow-2xs space-y-4"
            >
              <div className="flex items-center justify-between gap-3">
                <Skeleton className="size-10 rounded-xl" />
                <Skeleton className="h-6 w-20 rounded-full" />
              </div>
              <div className="space-y-2">
                <Skeleton className="h-4 w-2/3" />
                <Skeleton className="h-3 w-1/2" />
              </div>
              <Skeleton className="h-8 w-full rounded-xl" />
            </div>
          ))}
        </div>
      )}

      {variant === 'table' && (
        <>
          <div className="flex items-center gap-2 rounded-2xl border border-line/80 bg-white p-1.5 shadow-2xs w-fit">
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="h-9 w-24 rounded-xl" />
            ))}
          </div>
          <div className="overflow-hidden rounded-2xl border border-line/80 bg-white shadow-2xs">
            <div className="flex items-center gap-3 border-b border-line px-4 py-3">
              <Skeleton className="h-10 w-full sm:w-64 rounded-lg" />
            </div>
            <div className="divide-y divide-line">
              <div className="grid grid-cols-[54px_repeat(4,minmax(0,1fr))] gap-4 bg-surface-subtle px-4 py-3.5">
                <Skeleton className="h-3 w-4 mx-auto" />
                <Skeleton className="h-3.5 w-24" />
                <Skeleton className="h-3.5 w-28" />
                <Skeleton className="h-3.5 w-20" />
                <Skeleton className="h-3.5 w-24" />
              </div>
              {Array.from({ length: 8 }, (_, i) => (
                <div
                  key={i}
                  className="grid grid-cols-[54px_repeat(4,minmax(0,1fr))] gap-4 px-4 py-3"
                >
                  <Skeleton className="h-3 w-5 mx-auto" />
                  <Skeleton className="h-3.5 w-3/4" />
                  <Skeleton className="h-3.5 w-5/6" />
                  <Skeleton className="h-3.5 w-1/2" />
                  <Skeleton className="h-3.5 w-2/3" />
                </div>
              ))}
            </div>
          </div>
        </>
      )}

      {variant === 'schema' && (
        <div className="relative h-[520px] overflow-hidden rounded-2xl border border-line bg-surface-subtle p-6 shadow-xs">
          <div className="grid grid-cols-1 gap-6 md:grid-cols-3">
            {Array.from({ length: 3 }, (_, i) => (
              <div
                key={i}
                className="rounded-xl border border-line bg-white shadow-xs overflow-hidden"
              >
                <div className="border-b border-line bg-surface-subtle px-4 py-3 flex items-center justify-between">
                  <Skeleton className="h-4 w-28" />
                  <Skeleton className="h-4 w-12 rounded-full" />
                </div>
                <div className="p-3 space-y-2.5">
                  {Array.from({ length: 5 }, (_, j) => (
                    <div key={j} className="flex items-center justify-between gap-2">
                      <Skeleton className="h-3 w-24" />
                      <Skeleton className="h-3 w-14" />
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {variant === 'split' && (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_360px]">
          <div className="rounded-2xl border border-line bg-white shadow-xs overflow-hidden">
            <div className="flex flex-wrap gap-2 border-b border-line bg-surface-subtle p-4">
              {Array.from({ length: 4 }, (_, i) => (
                <Skeleton key={i} className="h-9 w-28 rounded-xl" />
              ))}
            </div>
            <div className="p-6 space-y-5">
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <Skeleton className="h-14 rounded-xl" />
                <Skeleton className="h-14 rounded-xl" />
              </div>
              <Skeleton className="h-28 rounded-xl" />
              <Skeleton className="h-24 rounded-xl" />
            </div>
          </div>
          <div className="h-fit rounded-2xl border border-line bg-white shadow-xs overflow-hidden">
            <div className="border-b border-line bg-surface-subtle px-5 py-4">
              <Skeleton className="h-4 w-36" />
            </div>
            <div className="divide-y divide-line p-4 space-y-3">
              {Array.from({ length: 6 }, (_, i) => (
                <div key={i} className="flex items-center justify-between pt-2 first:pt-0">
                  <Skeleton className="h-3.5 w-28" />
                  <Skeleton className="h-5 w-16 rounded" />
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {variant === 'list' && (
        <>
          <div className="flex items-center gap-2 rounded-2xl border border-line/80 bg-white p-1.5 shadow-2xs w-fit">
            {Array.from({ length: 3 }, (_, i) => (
              <Skeleton key={i} className="h-9 w-36 rounded-xl" />
            ))}
          </div>
          <div className="flex items-center justify-between gap-3 rounded-2xl border border-line/80 bg-white p-4.5 shadow-2xs">
            <Skeleton className="h-5 w-48" />
            <Skeleton className="h-9 w-28 rounded-xl" />
          </div>
          {Array.from({ length: 3 }, (_, i) => (
            <div
              key={i}
              className="rounded-2xl border border-line bg-white p-4.5 shadow-2xs space-y-3"
            >
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="space-y-2 flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <Skeleton className="h-4 w-48" />
                    <Skeleton className="h-5 w-20 rounded-full" />
                  </div>
                  <Skeleton className="h-3 w-64 max-w-full" />
                </div>
                <Skeleton className="h-8 w-24 rounded-lg" />
              </div>
            </div>
          ))}
        </>
      )}

      {variant === 'admin-split' && (
        <div className="grid grid-cols-1 gap-5 min-[901px]:grid-cols-[minmax(300px,380px)_minmax(0,1fr)]">
          <div className="rounded-2xl border border-line/80 bg-white overflow-hidden shadow-2xs divide-y divide-line">
            <div className="p-4">
              <Skeleton className="h-10 w-full rounded-lg" />
            </div>
            {Array.from({ length: 6 }, (_, i) => (
              <div key={i} className="flex items-center gap-3 p-4">
                <Skeleton className="size-9 rounded-xl shrink-0" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-3.5 w-32" />
                  <Skeleton className="h-3 w-44" />
                </div>
              </div>
            ))}
          </div>
          <div className="rounded-2xl border border-line/80 bg-white p-6 shadow-2xs space-y-5">
            <div className="flex items-center justify-between gap-4 border-b border-line pb-5">
              <div className="space-y-2">
                <Skeleton className="h-6 w-48" />
                <Skeleton className="h-3.5 w-64" />
              </div>
              <Skeleton className="h-9 w-24 rounded-xl" />
            </div>
            {Array.from({ length: 3 }, (_, i) => (
              <Skeleton key={i} className="h-16 w-full rounded-xl" />
            ))}
          </div>
        </div>
      )}

      {variant === 'metrics' && (
        <>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {Array.from({ length: 4 }, (_, i) => (
              <div
                key={i}
                className="rounded-2xl border border-line/80 bg-white p-4 shadow-2xs space-y-3"
              >
                <Skeleton className="h-3 w-28" />
                <div className="flex items-baseline justify-between">
                  <Skeleton className="h-7 w-14" />
                  <Skeleton className="h-3 w-20" />
                </div>
              </div>
            ))}
          </div>
          <div className="rounded-2xl border border-line/80 bg-white overflow-hidden shadow-xs">
            <div className="flex items-center justify-between border-b border-line px-5 py-4">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="h-8 w-44 rounded-lg" />
            </div>
            <div className="divide-y divide-line p-4 space-y-3">
              {Array.from({ length: 4 }, (_, i) => (
                <Skeleton key={i} className="h-14 w-full rounded-xl" />
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
