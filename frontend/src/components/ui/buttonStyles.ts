export const baseButtonClasses =
  'inline-flex min-h-11 max-w-full items-center justify-center gap-2 rounded-xl font-bold text-[13px] tracking-tight disabled:cursor-not-allowed select-none transition-colors'

export const buttonVariants = {
  default:
    'border border-line/80 bg-white text-ink hover:bg-canvas shadow-2xs disabled:border-line disabled:bg-disabled-bg disabled:text-disabled-fg',
  dark: 'border-ink bg-ink text-white hover:bg-ink-strong shadow-xs disabled:border-line disabled:bg-disabled-bg disabled:text-disabled-fg',
  danger:
    'border-danger bg-danger text-white hover:bg-danger-strong disabled:border-line disabled:bg-disabled-bg disabled:text-disabled-fg',
  ghost:
    'border-transparent bg-transparent text-muted hover:bg-violet-soft/40 hover:text-ink disabled:text-disabled-fg',
  outline:
    'border border-line bg-white/90 text-ink hover:bg-white hover:border-line-strong shadow-2xs disabled:border-line disabled:text-disabled-fg',
} as const

export const buttonSizes = {
  sm: 'min-h-8 px-3 text-xs rounded-lg gap-1.5',
  md: 'min-h-11 px-[18px] text-[13px] rounded-xl gap-2',
  lg: 'min-h-12 px-6 text-sm rounded-2xl gap-2.5',
} as const

export type ButtonVariant = keyof typeof buttonVariants
export type ButtonSize = keyof typeof buttonSizes
