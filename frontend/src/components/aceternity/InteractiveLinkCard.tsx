import type { ButtonHTMLAttributes } from 'react'
import { m, useReducedMotion } from 'motion/react'
import { Link, type LinkProps } from 'react-router'
import { cn } from '../../lib/utils'

export const baseInteractiveCardClass =
  'relative isolate h-full rounded-2xl border border-line/80 bg-white shadow-2xs transition-[border-color,box-shadow] duration-150 ease-out hover:border-violet/50 hover:shadow-surface focus-visible:ring-2 focus-visible:ring-violet/30'

type InteractiveLinkCardProps = LinkProps & {
  className?: string
  wrapperClassName?: string
}

export function InteractiveLinkCard({
  className,
  wrapperClassName,
  children,
  ...props
}: InteractiveLinkCardProps) {
  const reduceMotion = useReducedMotion()

  return (
    <m.div
      className={cn('h-full min-w-0 rounded-2xl', wrapperClassName)}
      whileHover={reduceMotion ? undefined : { y: -2 }}
      whileTap={reduceMotion ? undefined : { scale: 0.98 }}
      transition={{ duration: 0.15, ease: 'easeOut' }}
    >
      <Link className={cn(baseInteractiveCardClass, className)} {...props}>
        {children}
      </Link>
    </m.div>
  )
}

type InteractiveButtonCardProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  className?: string
  wrapperClassName?: string
}

export function InteractiveButtonCard({
  className,
  wrapperClassName,
  children,
  ...props
}: InteractiveButtonCardProps) {
  const reduceMotion = useReducedMotion()

  return (
    <m.div
      className={cn('h-full min-w-0 rounded-2xl', wrapperClassName)}
      whileHover={reduceMotion ? undefined : { y: -2 }}
      whileTap={reduceMotion ? undefined : { scale: 0.98 }}
      transition={{ duration: 0.15, ease: 'easeOut' }}
    >
      <button
        type="button"
        className={cn(baseInteractiveCardClass, 'w-full cursor-pointer', className)}
        {...props}
      >
        {children}
      </button>
    </m.div>
  )
}
