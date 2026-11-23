import type { ButtonHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'
import {
  baseButtonClasses,
  buttonSizes,
  buttonVariants,
  type ButtonSize,
  type ButtonVariant,
} from './buttonStyles'

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant | string
  size?: ButtonSize
  loading?: boolean
  fullWidth?: boolean
}

export function Button({
  children,
  variant = 'default',
  size = 'md',
  loading = false,
  fullWidth = false,
  className,
  type = 'button',
  disabled,
  ...props
}: ButtonProps) {
  const variantClass =
    variant in buttonVariants
      ? buttonVariants[variant as ButtonVariant]
      : buttonVariants.default

  return (
    <button
      className={cn(
        baseButtonClasses,
        variantClass,
        buttonSizes[size],
        fullWidth && 'w-full',
        className,
      )}
      type={type}
      disabled={disabled || loading}
      {...props}
    >
      {loading ? (
        <>
          <span className="size-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
          <span>{children}</span>
        </>
      ) : (
        children
      )}
    </button>
  )
}
