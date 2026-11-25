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
  Loading,
  NullBadge,
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

  const cellClass =
    'h-10 overflow-hidden border-r border-b border-line px-2.5 sm:px-3 text-left whitespace-nowrap text-ellipsis max-w-[280px] sm:max-w-[340px]'

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
      <table className="w-max min-w-[max(100%,1000px)] table-fixed border-collapse text-xs">
        <thead className="sticky top-0 z-10">
          <tr>
            <th className={cn(cellClass, 'h-12 w-[54px] bg-[#fbfafd] font-mono text-[10px] text-muted text-center')}>
              #
            </th>
            {columns.map((c) => {
              const isSorted = sortCol === c
              return (
                <th
                  className={cn(
                    cellClass,
                    'h-12 bg-[#fbfafd] font-semibold text-ink transition-colors hover:bg-canvas cursor-pointer select-none',
                    isSorted && 'bg-violet-soft/40 text-violet',
                  )}
                  key={c}
                  onClick={() => onSort(c)}
                  title={`Click to sort by ${c}`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate">{c}</span>
                    <span className="shrink-0 text-muted">
                      {isSorted ? (
                        sortDir === 'asc' ? (
                          <Icon name="sort-asc" className="size-3.5 text-violet" />
                        ) : (
                          <Icon name="sort-desc" className="size-3.5 text-violet" />
                        )
                      ) : (
                        <Icon name="sort" className="size-3 text-muted/30 hover:text-muted" />
                      )}
                    </span>
                  </div>
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
                  cellClass,
                  'w-[54px] text-center font-mono text-[10px] text-muted bg-[#fbfafd]/40',
                )}
              >
                {i + 1}
              </td>
              {row.map((cell, j) => {
                const cellKey = `${i}-${j}`
                const isCopied = copiedCell === cellKey
                return (
                  <td
                    className={cn(
                      cellClass,
                      j === 0 && 'font-mono text-[#6946a1]',
                      'cursor-default',
                    )}
                    key={j}
                    onClick={() => copyValue(cell, cellKey)}
                    title={cell !== null ? `${cell} (Click to copy)` : 'NULL'}
                  >
                    {isCopied ? (
                      <span className="text-[10px] font-bold text-success flex items-center gap-1">
                        <Icon name="check" className="size-3" /> Copied
                      </span>
                    ) : cell === null ? (
                      <NullBadge />
                    ) : (
                      cell
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
  const cellClass = 'h-11 border border-line px-4 text-left text-xs'

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
      <table className="w-full min-w-[720px] border-collapse">
        <thead>
          <tr>
            {headers.map((h) => (
              <th
                className={cn(
                  cellClass,
                  'bg-[#fbfafd] text-[10px] font-bold text-muted uppercase tracking-wider',
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
                      'font-mono text-[11px] text-[#6946a1]',
                  )}
                  key={j}
                >
                  {value === 'Yes' ? (
                    <span className="inline-block rounded-full bg-success-soft text-success px-2 py-0.5 text-[10px] font-bold">
                      Yes
                    </span>
                  ) : value === 'No' ? (
                    <span className="inline-block rounded-full bg-canvas text-muted px-2 py-0.5 text-[10px] font-medium">
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
          <span className="flex items-center gap-1.5">
            <Link
              to="/databases"
              className="hover:underline hover:text-ink transition-colors"
            >
              {databaseName}
            </Link>
            <span>/</span>
            {schemas.data && schemas.data.length > 1 ? (
              <select
                value={schema}
                onChange={(e) =>
                  navigate(`/databases/${id}/${e.target.value}/tables`)
                }
                className="bg-canvas border border-line rounded px-1.5 py-0.5 text-[11px] font-mono outline-none cursor-pointer"
              >
                {schemas.data.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            ) : (
              <span>{schema}</span>
            )}
          </span>
        }
        title={selected || 'Tables'}
        status={
          isLoading
            ? 'Reading table data and schema…'
            : rows.data
              ? `${rows.data.rows.length} rows on page ${page}`
              : undefined
        }
        actions={
          selected ? (
            <div className="flex flex-wrap items-center gap-2">
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
            className="py-16"
          >
            <Loading label="Reading table data and schema…" />
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

      <section className="min-w-0 max-w-full overflow-hidden rounded-[18px] border border-line/80 bg-white shadow-2xs">
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
              <span className="text-[11px] text-muted">
                Showing {displayedRows.length} of {rows.data?.rows.length ?? 0}{' '}
                rows
              </span>
            )}
            {sortCol && (
              <div className="flex items-center gap-1 text-[11px] bg-violet-soft text-violet px-2 py-1 rounded-md">
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
          <footer className="flex min-h-[50px] min-w-0 items-center justify-between border-t border-line px-4 text-xs text-muted">
            <div className="flex items-center gap-4">
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
    </>
  )
}
