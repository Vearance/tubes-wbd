import { Icon } from '../../../components/Icon'
import { Button } from '../../../components/ui'

export function TablePagination({
  page,
  pageSize,
  totalItems,
  onPageChange,
  onPageSizeChange,
}: {
  page: number
  pageSize: number
  totalItems: number
  onPageChange: (nextPage: number) => void
  onPageSizeChange: (nextSize: number) => void
}) {
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize))
  const safePage = Math.min(Math.max(1, page), totalPages)
  const start = totalItems === 0 ? 0 : (safePage - 1) * pageSize + 1
  const end = Math.min(totalItems, safePage * pageSize)

  return (
    <footer className="flex min-h-[52px] min-w-0 flex-wrap items-center justify-between gap-3 border-t border-line bg-surface-subtle/50 px-5 py-3 text-xs text-muted">
      <div className="flex flex-wrap items-center gap-2 sm:gap-3">
        <span className="tabular-nums">
          Showing <strong className="text-ink">{start}</strong>–
          <strong className="text-ink">{end}</strong> of{' '}
          <strong className="text-ink">{totalItems}</strong>
        </span>
        <span aria-hidden="true">·</span>
        <span className="tabular-nums">
          Page <strong className="text-ink">{safePage}</strong> / {totalPages}
        </span>
        <span className="hidden sm:inline" aria-hidden="true">
          ·
        </span>
        <label className="inline-flex items-center gap-1.5">
          <span>Rows</span>
          <select
            aria-label="Rows per page"
            value={pageSize}
            onChange={(e) => {
              onPageSizeChange(Number(e.target.value))
              onPageChange(1)
            }}
            className="h-8 rounded-lg border border-line bg-white px-2 font-mono text-xs font-semibold text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25 cursor-pointer"
          >
            {[10, 25, 50].map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div className="flex items-center gap-1.5">
        <Button
          variant="outline"
          size="sm"
          disabled={safePage <= 1}
          onClick={() => onPageChange(safePage - 1)}
        >
          <Icon name="chevron-left" className="size-3.5" />
          Previous
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={safePage >= totalPages}
          onClick={() => onPageChange(safePage + 1)}
        >
          Next
          <Icon name="chevron" className="size-3.5" />
        </Button>
      </div>
    </footer>
  )
}
