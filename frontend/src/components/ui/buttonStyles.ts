export const baseButtonClasses =
  'inline-flex min-h-11 items-center justify-center gap-2 rounded-[10px] font-bold text-[13px] transition-colors duration-150 disabled:cursor-not-allowed select-none'

export const buttonVariants = {
  default:
    'border border-line-strong bg-white text-ink hover:bg-canvas disabled:border-line disabled:bg-[#efedf1] disabled:text-[#aaa4ad]',
  dark: 'border-[#292929] bg-[#292929] text-white hover:bg-[#1f1f1f] disabled:border-line disabled:bg-[#efedf1] disabled:text-[#aaa4ad]',
  danger:
    'border-danger bg-danger text-white hover:bg-[#c93445] disabled:border-line disabled:bg-[#efedf1] disabled:text-[#aaa4ad]',
  ghost:
    'border-transparent bg-transparent text-muted hover:bg-canvas hover:text-ink disabled:text-[#aaa4ad]',
  outline:
    'border border-line bg-transparent text-ink hover:bg-white disabled:border-line disabled:text-[#aaa4ad]',
} as const

export const buttonSizes = {
  sm: 'min-h-8 px-3 text-xs rounded-lg gap-1.5',
  md: 'min-h-11 px-[18px] text-[13px] rounded-[10px] gap-2',
  lg: 'min-h-12 px-6 text-sm rounded-[12px] gap-2.5',
} as const

export type ButtonVariant = keyof typeof buttonVariants
export type ButtonSize = keyof typeof buttonSizes
