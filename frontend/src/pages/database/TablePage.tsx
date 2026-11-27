import { useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { AnimatePresence, m } from 'motion/react'
import { api } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { AnimatedTabs } from '../../components/aceternity/AnimatedTabs'
import { useWorkspaceContext } from '../../components/workspaceContext'
import { Icon } from '../../components/Icon'
import {
  baseButtonClasses,
  Button,
  buttonSizes,
  buttonVariants,
  EmptyState,
  ErrorNotice,
  Modal,
  NullBadge,
  PageSkeleton,
  Search,
} from '../../components/ui'
import { useResource } from '../../hooks'
import { cn } from '../../lib/utils'

function DataGrid({
  columns,
  rows,
  sortCol,
  sortDir,
  onSort,
  searchQuery,
  onClearSearch,
}: {
  columns: string[]
  rows: Array<Array<string | null>>
  sortCol: string | null
  sortDir: 'asc' | 'desc'
  onSort: (col: string) => void
  searchQuery?: string
  onClearSearch?: () => void
}) {
  const [copiedCell, setCopiedCell] = useState<string | null>(null)

  function copyValue(val: string | null, key: string) {
    if (val === null) return
    void navigator.clipboard?.writeText(val)
    setCopiedCell(key)
    setTimeout(() => setCopiedCell(null), 1500)
  }

  const cellBaseClass =
    'h-9 sm:h-10 overflow-hidden border-r border-b border-line text-left max-w-[200px] sm:max-w-[340px] whitespace-nowrap'

  if (rows.length === 0) {
    return (
      <div className="py-12">
        <EmptyState
          icon={searchQuery ? 'search' : 'table'}
          title={searchQuery ? 'No matching rows found' : 'Table is empty'}
          action={
            searchQuery && onClearSearch ? (
              <Button variant="outline" size="sm" onClick={onClearSearch}>
                Clear search filter
              </Button>
            ) : undefined
          }
        >
          {searchQuery
            ? `No records in this page matched "${searchQuery}".`
            : 'No data rows exist in this table yet.'}
        </EmptyState>
      </div>
    )
  }

  return (
    <div className="w-full min-w-0 max-w-full overflow-auto overscroll-x-contain [scrollbar-width:thin] touch-pan-x">
      <div className="sr-only" role="status" aria-live="polite">
        {copiedCell ? 'Cell value copied to clipboard' : ''}
      </div>
      <table className="w-full min-w-full table-auto min-[901px]:w-max min-[901px]:min-w-[max(100%,1000px)] min-[901px]:table-fixed border-collapse text-xs">
        <thead className="sticky top-0 z-10">
          <tr>
            <th
              className={cn(
                cellBaseClass,
                'h-10 sm:h-12 w-[44px] sm:w-[54px] px-2 sm:px-3 bg-surface-subtle font-mono text-[11px] text-muted text-center',
              )}
            >
              #
            </th>
            {columns.map((c) => {
              const isSorted = sortCol === c
              return (
                <th
                  className={cn(
                    cellBaseClass,
                    'h-10 sm:h-12 p-0 bg-surface-subtle font-semibold text-ink transition-colors hover:bg-canvas select-none',
                    isSorted && 'bg-violet-soft/40 text-violet-fg',
                  )}
                  key={c}
                  aria-sort={
                    isSorted
                      ? sortDir === 'asc'
                        ? 'ascending'
                        : 'descending'
                      : 'none'
                  }
                >
                  <button
                    type="button"
                    onClick={() => onSort(c)}
                    title={`Click to sort by ${c}`}
                    className="flex h-full w-full items-center justify-between gap-2 px-2.5 sm:px-3 text-left font-semibold cursor-pointer active:scale-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-violet/60"
                  >
                    <span className="truncate">{c}</span>
                    <span className="shrink-0 text-muted">
                      {isSorted ? (
                        sortDir === 'asc' ? (
                          <Icon name="sort-asc" className="size-3.5 text-violet-fg" />
                        ) : (
                          <Icon name="sort-desc" className="size-3.5 text-violet-fg" />
                        )
                      ) : (
                        <Icon name="sort" className="size-3 text-muted/40 hover:text-muted" />
                      )}
                    </span>
                  </button>
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i} className="hover:bg-canvas/60 transition-colors">
              <td
                className={cn(
                  cellBaseClass,
                  'w-[44px] sm:w-[54px] px-2 text-center font-mono text-[11px] text-muted bg-surface-subtle/40 select-none',
                )}
              >
                {i + 1}
              </td>
              {row.map((cell, j) => {
                const cellKey = `${i}-${j}`
                const isCopied = copiedCell === cellKey
                return (
                  <td
                    className={cn(cellBaseClass, 'p-0')}
                    key={j}
                  >
                    {cell === null ? (
                      <span className="flex h-full items-center px-2.5 sm:px-3">
                        <NullBadge />
                      </span>
                    ) : (
                      <button
                        type="button"
                        onClick={() => copyValue(cell, cellKey)}
                        title={`${cell} (Click to copy)`}
                        aria-label={`Copy ${columns[j]} value`}
                        className={cn(
                          'flex h-full w-full items-center truncate px-2.5 sm:px-3 text-left cursor-copy active:scale-100 hover:bg-violet-soft/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-violet/60',
                          j === 0 && 'font-mono text-violet-fg font-semibold',
                        )}
                      >
                        {isCopied ? (
                          <span className="text-[11px] font-bold text-success-fg flex items-center gap-1">
                            <Icon name="check" className="size-3" /> Copied
                          </span>
                        ) : (
                          <span className="truncate">{cell}</span>
                        )}
                      </button>
                    )}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function DefinitionTable({
  headers,
  rows,
  typeColumnIndex,
  emptyMessage,
}: {
  headers: string[]
  rows: string[][]
  typeColumnIndex?: number
  emptyMessage?: string
}) {
  const cellClass = 'h-9 sm:h-11 border border-line px-2.5 py-1.5 sm:px-4 text-left text-xs'

  if (rows.length === 0) {
    return (
      <div className="py-10">
        <EmptyState
          icon="columns"
          title="No records"
        >
          {emptyMessage ?? 'No definitions found.'}
        </EmptyState>
      </div>
    )
  }

  return (
    <div className="w-full min-w-0 max-w-full overflow-auto overscroll-x-contain [scrollbar-width:thin] touch-pan-x">
      <table className="w-full min-w-full min-[901px]:min-w-[720px] border-collapse">
        <thead>
          <tr>
            {headers.map((h) => (
              <th
                className={cn(
                  cellClass,
                  'bg-surface-subtle text-[11px] font-bold text-muted uppercase tracking-wider',
                )}
                key={h}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i} className="hover:bg-canvas/60 transition-colors">
              {row.map((value, j) => (
                <td
                  className={cn(
                    cellClass,
                    j === 0 && 'font-semibold text-ink',
                    j === typeColumnIndex &&
                      'font-mono text-xs text-violet-fg',
                  )}
                  key={j}
                >
                  {value === 'Yes' ? (
                    <span className="inline-block rounded-full bg-success-soft text-success-fg px-2.5 py-0.5 text-[11px] font-bold">
                      Yes
                    </span>
                  ) : value === 'No' ? (
                    <span className="inline-block rounded-full bg-canvas text-muted px-2.5 py-0.5 text-[11px] font-medium">
                      No
                    </span>
                  ) : (
                    value
                  )}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function TablePage() {
  const navigate = useNavigate()
  const {
    databaseId = '0',
    schema = 'public',
    table = 'tables',
  } = useParams()
  const id = Number(databaseId)
  const [params, setParams] = useSearchParams()
  const page = Number(params.get('page') || 1)
  const tab = params.get('tab') || 'data'
  const [search, setSearch] = useState('')
  const [sortCol, setSortCol] = useState<string | null>(null)
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')
  const [tablePickerOpen, setTablePickerOpen] = useState(false)
  const [tablePickerSearch, setTablePickerSearch] = useState('')

  const { databases } = useWorkspaceContext()
  const databaseName =
    databases.find((database) => database.id === id)?.name ??
    `Database #${id}`

  const schemas = useResource(() => api.schemas(id), [id])
  const tables = useResource(() => api.tables(id, schema), [id, schema])
  const selected = table === 'tables' ? tables.data?.[0]?.name : table

  const structure = useResource(
    () =>
      selected
        ? api.structure(id, schema, selected)
        : Promise.resolve(null),
    [id, schema, selected],
  )
  const rows = useResource(
    () =>
      selected
        ? api.rows(id, schema, selected, page, 50)
        : Promise.resolve(null),
    [id, schema, selected, page],
  )

  const isLoading =
    (tables.loading && !tables.data) ||
    (structure.loading && !structure.data) ||
    (rows.loading && !rows.data)

  const error = tables.error || structure.error || rows.error

  function handleSort(columnName: string) {
    if (sortCol === columnName) {
      if (sortDir === 'asc') setSortDir('desc')
      else {
        setSortCol(null)
        setSortDir('asc')
      }
    } else {
      setSortCol(columnName)
      setSortDir('asc')
    }
  }

  let displayedRows =
    rows.data?.rows.filter((row) =>
      row.some((cell) =>
        String(cell ?? '')
          .toLowerCase()
          .includes(search.toLowerCase()),
      ),
    ) ?? []

  if (sortCol && rows.data) {
    const colIndex = rows.data.columns.indexOf(sortCol)
    if (colIndex !== -1) {
      displayedRows = [...displayedRows].sort((a, b) => {
        const valA = a[colIndex]
        const valB = b[colIndex]
        if (valA === null && valB === null) return 0
        if (valA === null) return 1
        if (valB === null) return -1
        const comp = String(valA).localeCompare(String(valB), undefined, {
          numeric: true,
        })
        return sortDir === 'asc' ? comp : -comp
      })
    }
  }

  return (
    <>
      <UtilityHeader
        backTo="/databases"
        path={
          <span className="flex min-w-0 items-center gap-1.5">
            <Link
              to="/databases"
              className="truncate hover:underline hover:text-ink transition-colors"
            >
              {databaseName}
            </Link>
            <span className="shrink-0">/</span>
            {schemas.data && schemas.data.length > 1 ? (
              <select
                aria-label="Select schema"
                value={schema}
                onChange={(e) =>
                  navigate(`/databases/${id}/${e.target.value}/tables`)
                }
                className="max-w-[120px] shrink-0 rounded-lg border border-line bg-canvas px-2 py-0.5 font-mono text-base sm:text-xs text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25 cursor-pointer"
              >
                {schemas.data.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            ) : (
              <span className="shrink-0">{schema}</span>
            )}
          </span>
        }
        title={
          <>
            <span className="hidden min-[901px]:inline">
              {selected || 'Tables'}
            </span>
            {(tables.data?.length ?? 0) > 0 ? (
              <button
                type="button"
                onClick={() => {
                  setTablePickerSearch('')
                  setTablePickerOpen(true)
                }}
                aria-label="Switch table"
                aria-expanded={tablePickerOpen}
                className="inline-flex min-w-0 max-w-[240px] items-center gap-1.5 rounded-xl border border-line-strong bg-white px-2.5 py-1 font-mono text-sm font-bold text-ink shadow-2xs transition-colors hover:border-violet hover:bg-violet-soft/20 cursor-pointer min-[901px]:hidden"
              >
                <Icon name="table" className="size-4 text-violet-fg shrink-0" />
                <span className="truncate">{selected || 'Tables'}</span>
                <Icon
                  name="chevrons-up-down"
                  className="size-3.5 text-muted shrink-0"
                />
              </button>
            ) : (
              <span className="min-[901px]:hidden">Tables</span>
            )}
          </>
        }
        status={
          isLoading
            ? 'Reading table data and schema…'
            : rows.data
              ? `${rows.data.rows.length} rows on page ${page}`
              : undefined
        }
        actions={
          selected ? (
            <div className="flex flex-wrap items-center gap-1.5 sm:gap-2.5 min-[901px]:w-auto">
              <Link
                to={`/databases/${id}/${schema}/query?table=${encodeURIComponent(selected)}`}
                className={cn(
                  baseButtonClasses,
                  buttonVariants.outline,
                  buttonSizes.sm,
                  'no-underline',
                )}
              >
                <Icon name="code" className="size-3.5" />
                Query Table
              </Link>
              <Link
                to={`/databases/${id}/${schema}/${encodeURIComponent(selected)}/edit`}
                className={cn(
                  baseButtonClasses,
                  buttonVariants.dark,
                  buttonSizes.sm,
                  'no-underline',
                )}
              >
                <Icon name="edit" className="size-3.5" />
                DDL Blueprint
              </Link>
            </div>
          ) : undefined
        }
      />

      <AnimatePresence mode="wait">
        {isLoading ? (
          <m.div
            key="table-loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
          >
            <PageSkeleton variant="table" label="Reading table data and schema…" />
          </m.div>
        ) : (
          <m.div
            key={`table-content-${selected ?? 'none'}`}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.22, ease: 'easeOut' }}
            className="space-y-4"
          >
            {error && <ErrorNotice message={error} />}

      <AnimatedTabs
        tabs={['data', 'columns', 'constraints', 'indexes'].map((name) => ({
          value: name,
          label: name[0].toUpperCase() + name.slice(1),
        }))}
        value={tab}
        onValueChange={(value) =>
          setParams({ tab: value, page: String(page) })
        }
        className="mb-4"
      />

      <section className="min-w-0 max-w-full overflow-hidden rounded-2xl border border-line/80 bg-white shadow-2xs">
        {tab === 'data' && (
          <div className="flex min-h-[54px] max-w-full flex-wrap items-center gap-2.5 border-b border-line px-3 py-2">
            <div className="w-full sm:w-auto shrink-0">
              <Search
                value={search}
                onChange={setSearch}
                placeholder="Search rows in table…"
                className="w-full sm:min-w-64"
              />
            </div>
            {search && (
              <span className="text-xs text-muted">
                Showing {displayedRows.length} of {rows.data?.rows.length ?? 0}{' '}
                rows
              </span>
            )}
            {sortCol && (
              <div className="flex items-center gap-1 text-xs bg-violet-soft text-violet-fg px-2.5 py-1 rounded-lg">
                <span>
                  Sorted by <b>{sortCol}</b> ({sortDir})
                </span>
                <button
                  type="button"
                  onClick={() => setSortCol(null)}
                  className="hover:text-ink ml-1"
                  aria-label="Clear sort"
                >
                  <Icon name="close" className="size-3" />
                </button>
              </div>
            )}
          </div>
        )}

        {tab === 'data' && (
          <DataGrid
            columns={rows.data?.columns ?? []}
            rows={displayedRows}
            sortCol={sortCol}
            sortDir={sortDir}
            onSort={handleSort}
            searchQuery={search}
            onClearSearch={() => setSearch('')}
          />
        )}

        {tab === 'columns' && (
          <DefinitionTable
            headers={['Name', 'Data type', 'Nullable', 'Default']}
            rows={(structure.data?.columns ?? []).map((c) => [
              c.name,
              c.type,
              c.nullable ? 'Yes' : 'No',
              c.default ?? '—',
            ])}
            typeColumnIndex={1}
          />
        )}

        {tab === 'constraints' && (
          <DefinitionTable
            headers={['Name', 'Type', 'Definition']}
            rows={(structure.data?.constraints ?? []).map((c) => [
              c.name,
              c.type,
              c.definition,
            ])}
            emptyMessage="No constraints defined on this table."
          />
        )}

        {tab === 'indexes' && (
          <DefinitionTable
            headers={['Name', 'Unique', 'Primary', 'Definition']}
            rows={(structure.data?.indexes ?? []).map((i) => [
              i.name,
              i.unique ? 'Yes' : 'No',
              i.primary ? 'Yes' : 'No',
              i.definition,
            ])}
            emptyMessage="No indexes defined on this table."
          />
        )}

        {tab === 'data' && (
          <footer className="flex min-h-[50px] min-w-0 flex-wrap items-center justify-between gap-2 border-t border-line px-3.5 py-2.5 sm:px-4 text-xs text-muted">
            <div className="flex items-center gap-2 sm:gap-4">
              <span>
                Page <strong className="text-ink">{page}</strong>
              </span>
              <span>·</span>
              <span>
                Max <strong>{rows.data?.page_size ?? 50}</strong> rows/page
              </span>
            </div>
            <div className="flex items-center gap-1.5">
              <Button
                variant="outline"
                size="sm"
                disabled={page <= 1}
                onClick={() =>
                  setParams({ tab, page: String(page - 1) })
                }
              >
                <Icon name="chevron-left" className="size-3.5" />
                Previous
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={!rows.data?.has_next}
                onClick={() =>
                  setParams({ tab, page: String(page + 1) })
                }
              >
                Next
                <Icon name="chevron" className="size-3.5" />
              </Button>
            </div>
          </footer>
        )}
      </section>
          </m.div>
        )}
      </AnimatePresence>

      <Modal
        open={tablePickerOpen}
        onOpenChange={setTablePickerOpen}
        title="Switch Table"
        description={`Tables in ${schema} schema`}
        size="sm"
      >
        <div className="space-y-3">
          {(tables.data?.length ?? 0) > 4 && (
            <Search
              value={tablePickerSearch}
              onChange={setTablePickerSearch}
              placeholder="Filter tables…"
              className="w-full"
            />
          )}
          <div className="max-h-[50dvh] space-y-1 overflow-y-auto pr-1 [scrollbar-width:thin]">
            {(tables.data ?? [])
              .filter((t) =>
                t.name.toLowerCase().includes(tablePickerSearch.toLowerCase()),
              )
              .map((entry) => {
                const isCurrent = entry.name === selected
                return (
                  <button
                    key={`${entry.schema}.${entry.name}`}
                    type="button"
                    onClick={() => {
                      setTablePickerOpen(false)
                      navigate(
                        `/databases/${id}/${entry.schema}/${encodeURIComponent(entry.name)}`,
                      )
                    }}
                    className={cn(
                      'flex min-h-10 w-full items-center gap-2.5 rounded-xl border px-3 py-2 text-left font-mono text-xs transition-colors cursor-pointer',
                      isCurrent
                        ? 'border-violet bg-violet-soft/60 font-bold text-violet-fg shadow-2xs'
                        : 'border-line/70 bg-white text-ink hover:bg-canvas',
                    )}
                  >
                    <Icon
                      name="table"
                      className={cn(
                        'size-3.5 shrink-0',
                        isCurrent ? 'text-violet-fg' : 'text-muted',
                      )}
                    />
                    <span className="flex-1 truncate">{entry.name}</span>
                    {entry.kind === 'view' && (
                      <span className="rounded bg-accent-soft px-1.5 py-0.5 font-sans text-[11px] font-bold uppercase text-accent-fg">
                        View
                      </span>
                    )}
                    {isCurrent && (
                      <Icon
                        name="check"
                        className="size-3.5 shrink-0 text-violet-fg"
                      />
                    )}
                  </button>
                )
              })}
            {(tables.data ?? []).filter((t) =>
              t.name.toLowerCase().includes(tablePickerSearch.toLowerCase()),
            ).length === 0 && (
              <p className="m-0 py-4 text-center text-xs text-muted italic">
                No tables match &ldquo;{tablePickerSearch}&rdquo;
              </p>
            )}
          </div>
        </div>
      </Modal>
    </>
  )
}
