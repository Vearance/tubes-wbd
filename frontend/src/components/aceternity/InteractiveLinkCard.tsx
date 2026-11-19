import { m, useReducedMotion } from 'motion/react'
import { Link, type LinkProps } from 'react-router'
import { cn } from '../../lib/utils'

type InteractiveLinkCardProps = LinkProps & {
  className?: string
}

export function InteractiveLinkCard({ className, children, ...props }: InteractiveLinkCardProps) {
  const reduceMotion = useReducedMotion()

  return <m.div
    className="h-full min-w-0 rounded-[14px]"
    whileHover={reduceMotion ? undefined : { y: -2 }}
    whileTap={reduceMotion ? undefined : { scale: .97 }}
    transition={{ duration: .15, ease: 'easeOut' }}
  >
    <Link
      className={cn(
        'relative isolate h-full rounded-[14px] border border-line bg-white shadow-none transition-[border-color,box-shadow] duration-150 ease-out hover:border-violet hover:shadow-surface focus-visible:ring-2 focus-visible:ring-violet/30',
        className,
      )}
      {...props}
    >
      {children}
    </Link>
  </m.div>
}
