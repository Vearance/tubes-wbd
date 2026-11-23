import { Icon } from '../Icon'
import { cn } from '../../lib/utils'

export type SearchProps = {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  className?: string
}

export function Search({
  value,
  onChange,
  placeholder = 'Search',
  className,
}: SearchProps) {
  return (
    <label
      className={cn(
        'group flex h-10 min-w-60 items-center gap-2 rounded-lg border border-line bg-white px-3 text-muted focus-within:border-violet focus-within:ring-2 focus-within:ring-violet/20',
        className,
      )}
    >
      <Icon
        name="search"
        className="size-4 shrink-0 text-muted group-focus-within:text-violet"
      />
      <span className="sr-only">{placeholder}</span>
      <input
        className="w-full min-w-0 border-0 bg-transparent text-xs text-ink placeholder:text-[#a59ea9] outline-none"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
      />
      {value && (
        <button
          type="button"
          onClick={() => onChange('')}
          className="size-5 rounded-md text-muted hover:bg-canvas hover:text-ink grid place-items-center"
          aria-label="Clear search"
        >
          <Icon name="close" className="size-3" />
        </button>
      )}
    </label>
  )
}
