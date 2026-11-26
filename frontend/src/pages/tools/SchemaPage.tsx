import {
  useCallback,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { AnimatePresence, m } from 'motion/react'
import { api, type TableInfo, type TableStructure } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { useWorkspaceContext } from '../../components/workspaceContext'
import { Icon } from '../../components/Icon'
import {
  Button,
  EmptyState,
  ErrorNotice,
  PageSkeleton,
  Search,
} from '../../components/ui'
import { useResource } from '../../hooks'
import { cn } from '../../lib/utils'

type Relationship = {
  id: string
  fromTable: string
  fromColumn: string
  toTable: string
  toColumn: string
}

type ParsedTableData = {
  table: TableInfo
  structure?: TableStructure
  primaryKeys: Set<string>
  foreignKeys: Array<{ column: string; targetTable: string; targetColumn: string }>
}

const fkPattern =
  /FOREIGN KEY\s*\(([^)]+)\)\s*REFERENCES\s*(?:([a-zA-Z0-9_]+)\.)?([a-zA-Z0-9_]+)\s*\(([^)]+)\)/i
const pkPattern = /PRIMARY KEY\s*\(([^)]+)\)/i

function parseTableRelationships(
  tables: TableInfo[],
  structures: TableStructure[],
): {
  tableMap: Record<string, ParsedTableData>
  relationships: Relationship[]
} {
  const tableMap: Record<string, ParsedTableData> = {}
  const relationships: Relationship[] = []

  tables.forEach((t) => {
    tableMap[t.name] = {
      table: t,
      primaryKeys: new Set<string>(),
      foreignKeys: [],
    }
  })

  structures.forEach((st) => {
    const entry = tableMap[st.table.name]
    if (!entry) return
    entry.structure = st

    st.constraints?.forEach((c) => {
      if (c.type === 'PRIMARY KEY') {
        const m = pkPattern.exec(c.definition)
        if (m) {
          m[1].split(',').forEach((col) => entry.primaryKeys.add(col.trim()))
        }
      } else if (c.type === 'FOREIGN KEY') {
        const m = fkPattern.exec(c.definition)
        if (m) {
          const fromCols = m[1].split(',').map((s) => s.trim())
          const targetTable = m[3].trim()
          const toCols = m[4].split(',').map((s) => s.trim())
          fromCols.forEach((col, idx) => {
            const targetCol = toCols[idx] || toCols[0] || 'id'
            entry.foreignKeys.push({
              column: col,
              targetTable,
              targetColumn: targetCol,
            })
            relationships.push({
              id: `${st.table.name}.${col}->${targetTable}.${targetCol}`,
              fromTable: st.table.name,
              fromColumn: col,
              toTable: targetTable,
              toColumn: targetCol,
            })
          })
        }
      }
    })

    st.indexes?.forEach((idx) => {
      if (idx.primary) {
        entry.primaryKeys.add(idx.name)
      }
    })
  })

  return { tableMap, relationships }
}

function ConnectedSchemaDiagram({
  databaseId,
  schema,
  tables,
  tableMap,
  relationships,
  loading,
}: {
  databaseId: number
  schema: string
  tables: TableInfo[]
  tableMap: Record<string, ParsedTableData>
  relationships: Relationship[]
  loading?: boolean
}) {
  const [customPositions, setCustomPositions] = useState<
    Record<string, { x: number; y: number }>
  >({})
  const [hoveredTable, setHoveredTable] = useState<string | null>(null)
  const [hoveredRel, setHoveredRel] = useState<Relationship | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  const dragRef = useRef<{
    tableName: string
    startX: number
    startY: number
    initialX: number
    initialY: number
  } | null>(null)

  const defaultPositions = useMemo(() => {
    if (tables.length === 0) return {}

    const inDegree: Record<string, number> = {}
    const outDegree: Record<string, number> = {}
    tables.forEach((t) => {
      inDegree[t.name] = 0
      outDegree[t.name] = 0
    })

    relationships.forEach((rel) => {
      if (outDegree[rel.fromTable] !== undefined) outDegree[rel.fromTable]++
      if (inDegree[rel.toTable] !== undefined) inDegree[rel.toTable]++
    })

    const layer0: string[] = []
    const layer1: string[] = []
    const layer2: string[] = []

    tables.forEach((t) => {
      if (t.kind === 'view') {
        layer2.push(t.name)
      } else if (outDegree[t.name] === 0) {
        layer0.push(t.name)
      } else if (inDegree[t.name] > 0) {
        layer1.push(t.name)
      } else {
        layer2.push(t.name)
      }
    })

    const initialPositions: Record<string, { x: number; y: number }> = {}
    const COL_WIDTH = 340
    const ROW_GAP = 280
    const OFFSET_X = 50
    const OFFSET_Y = 50

    const layers = [layer0, layer1, layer2].filter((l) => l.length > 0)
    layers.forEach((layerTables, colIdx) => {
      layerTables.forEach((tableName, rowIdx) => {
        initialPositions[tableName] = {
          x: OFFSET_X + colIdx * COL_WIDTH,
          y: OFFSET_Y + rowIdx * ROW_GAP,
        }
      })
    })

    return initialPositions
  }, [tables, relationships])

  const positions = useMemo(
    () => ({ ...defaultPositions, ...customPositions }),
    [defaultPositions, customPositions],
  )

  const handleMouseDown = (tableName: string, e: ReactMouseEvent) => {
    e.preventDefault()
    const current = positions[tableName] || { x: 50, y: 50 }
    dragRef.current = {
      tableName,
      startX: e.clientX,
      startY: e.clientY,
      initialX: current.x,
      initialY: current.y,
    }

    const handleMouseMove = (moveEvent: MouseEvent) => {
      if (!dragRef.current) return
      const deltaX = moveEvent.clientX - dragRef.current.startX
      const deltaY = moveEvent.clientY - dragRef.current.startY
      const nextX = Math.max(20, dragRef.current.initialX + deltaX)
      const nextY = Math.max(20, dragRef.current.initialY + deltaY)

      setCustomPositions((prev) => ({
        ...prev,
        [dragRef.current!.tableName]: { x: nextX, y: nextY },
      }))
    }

    const handleMouseUp = () => {
      dragRef.current = null
      window.removeEventListener('mousemove', handleMouseMove)
      window.removeEventListener('mouseup', handleMouseUp)
    }

    window.addEventListener('mousemove', handleMouseMove)
    window.addEventListener('mouseup', handleMouseUp)
  }

  const handleTouchStart = (tableName: string, e: React.TouchEvent) => {
    const touch = e.touches[0]
    if (!touch) return
    const current = positions[tableName] || { x: 50, y: 50 }
    dragRef.current = {
      tableName,
      startX: touch.clientX,
      startY: touch.clientY,
      initialX: current.x,
      initialY: current.y,
    }

    const handleTouchMove = (moveEvent: TouchEvent) => {
      if (!dragRef.current || !moveEvent.touches[0]) return
      const t = moveEvent.touches[0]
      const deltaX = t.clientX - dragRef.current.startX
      const deltaY = t.clientY - dragRef.current.startY
      const nextX = Math.max(20, dragRef.current.initialX + deltaX)
      const nextY = Math.max(20, dragRef.current.initialY + deltaY)

      setCustomPositions((prev) => ({
        ...prev,
        [dragRef.current!.tableName]: { x: nextX, y: nextY },
      }))
    }

    const handleTouchEnd = () => {
      dragRef.current = null
      window.removeEventListener('touchmove', handleTouchMove)
      window.removeEventListener('touchend', handleTouchEnd)
    }

    window.addEventListener('touchmove', handleTouchMove, { passive: true })
    window.addEventListener('touchend', handleTouchEnd)
  }

  const CARD_WIDTH = 270

  const maxX = Math.max(
    1100,
    ...Object.values(positions).map((p) => p.x + CARD_WIDTH + 100),
  )
  const maxY = Math.max(
    700,
    ...Object.values(positions).map((p) => p.y + 360),
  )

  const isRelActive = useCallback(
    (rel: Relationship) => {
      if (hoveredRel && hoveredRel.id === rel.id) return true
      if (hoveredTable && (rel.fromTable === hoveredTable || rel.toTable === hoveredTable)) {
        return true
      }
      return false
    },
    [hoveredRel, hoveredTable],
  )

  return (
    <div className="relative overflow-hidden rounded-2xl border border-line bg-surface-subtle shadow-xs">
      {Object.keys(customPositions).length > 0 && (
        <button
          type="button"
          onClick={() => setCustomPositions({})}
          className="absolute top-3 left-3 z-20 inline-flex items-center gap-1 rounded-lg border border-line/80 bg-white/90 px-2.5 py-1 text-xs font-semibold text-ink shadow-2xs hover:border-violet hover:text-violet-fg transition-colors cursor-pointer backdrop-blur-xs"
        >
          <Icon name="refresh" className="size-3" />
          Reset layout
        </button>
      )}

      {hoveredRel && (
        <div className="absolute top-3 right-3 z-20 flex items-center gap-1.5 rounded-lg border border-violet/30 bg-violet-soft px-3 py-1.5 text-xs font-semibold text-violet-fg shadow-xs">
          <Icon name="link" className="size-3.5" />
          <span>
            {hoveredRel.fromTable}.{hoveredRel.fromColumn} → {hoveredRel.toTable}.{hoveredRel.toColumn}
          </span>
        </div>
      )}

      <div
        ref={containerRef}
        className="relative h-[calc(100dvh-220px)] min-h-[460px] sm:min-h-[640px] w-full overflow-auto overscroll-contain [scrollbar-width:thin] touch-pan-x touch-pan-y cursor-grab active:cursor-grabbing"
      >
        <svg
          className="absolute inset-0 pointer-events-none"
          width={maxX}
          height={maxY}
          style={{ minWidth: '100%', minHeight: '100%' }}
        >
          <defs>
            <marker
              id="er-arrow"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 1 L 10 5 L 0 9 z" fill="var(--color-violet)" />
            </marker>
            <marker
              id="er-arrow-muted"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 1 L 10 5 L 0 9 z" fill="#b9b3c2" />
            </marker>
          </defs>

          {relationships.map((rel) => {
            const pFrom = positions[rel.fromTable]
            const pTo = positions[rel.toTable]
            if (!pFrom || !pTo) return null

            const active = isRelActive(rel)

            let x1: number, y1: number, x2: number, y2: number
            if (pTo.x >= pFrom.x + CARD_WIDTH - 20) {
              x1 = pFrom.x + CARD_WIDTH
              y1 = pFrom.y + 70
              x2 = pTo.x
              y2 = pTo.y + 70
            } else if (pTo.x + CARD_WIDTH <= pFrom.x + 20) {
              x1 = pFrom.x
              y1 = pFrom.y + 70
              x2 = pTo.x + CARD_WIDTH
              y2 = pTo.y + 70
            } else {
              x1 = pFrom.x + CARD_WIDTH / 2
              y1 = pFrom.y > pTo.y ? pFrom.y : pFrom.y + 140
              x2 = pTo.x + CARD_WIDTH / 2
              y2 = pFrom.y > pTo.y ? pTo.y + 140 : pTo.y
            }

            const dx = Math.max(40, Math.abs(x2 - x1) * 0.5)
            const d = `M ${x1} ${y1} C ${x1 < x2 ? x1 + dx : x1 - dx} ${y1}, ${
              x1 < x2 ? x2 - dx : x2 + dx
            } ${y2}, ${x2} ${y2}`

            return (
              <g key={rel.id} className="pointer-events-auto">
                <path
                  d={d}
                  fill="none"
                  stroke={active ? 'var(--color-violet)' : '#c8c2cf'}
                  strokeWidth={active ? 2.5 : 1.5}
                  strokeDasharray={rel.fromTable === rel.toTable ? '4 4' : undefined}
                  markerEnd={active ? 'url(#er-arrow)' : 'url(#er-arrow-muted)'}
                  className="transition-all duration-150"
                />
                <path
                  d={d}
                  fill="none"
                  stroke="transparent"
                  strokeWidth={14}
                  className="cursor-pointer"
                  onMouseEnter={() => setHoveredRel(rel)}
                  onMouseLeave={() => setHoveredRel(null)}
                />
              </g>
            )
          })}
        </svg>

        {tables.map((table) => {
          const pos = positions[table.name] || { x: 50, y: 50 }
          const data = tableMap[table.name]
          const isConnected =
            hoveredTable !== null &&
            (hoveredTable === table.name ||
              relationships.some(
                (r) =>
                  (r.fromTable === hoveredTable && r.toTable === table.name) ||
                  (r.toTable === hoveredTable && r.fromTable === table.name),
              ))

          return (
            <div
              key={table.name}
              style={{
                transform: `translate3d(${pos.x}px, ${pos.y}px, 0)`,
                width: `${CARD_WIDTH}px`,
              }}
              className={cn(
                'absolute rounded-xl border border-line bg-white shadow-xs transition-shadow duration-150 select-none',
                isConnected && 'border-violet ring-2 ring-violet/20 shadow-md',
                hoveredTable === table.name && 'border-violet ring-2 ring-violet/30 shadow-lg',
              )}
              onMouseEnter={() => setHoveredTable(table.name)}
              onMouseLeave={() => setHoveredTable(null)}
            >
              <div
                className="flex items-center gap-2 border-b border-line bg-canvas/60 px-3.5 py-2.5 rounded-t-xl cursor-grab active:cursor-grabbing touch-none"
                onMouseDown={(e) => handleMouseDown(table.name, e)}
                onTouchStart={(e) => handleTouchStart(table.name, e)}
              >
                <Icon name="table" className="size-4 text-violet-fg shrink-0" />
                <strong className="truncate text-xs font-bold text-ink flex-1">
                  {table.name}
                </strong>
                {table.kind === 'view' && (
                  <span className="rounded bg-accent-soft px-1.5 py-0.5 text-[11px] font-bold text-accent-fg uppercase">
                    View
                  </span>
                )}
                <Link
                  to={`/databases/${databaseId}/${schema}/${table.name}`}
                  className="rounded p-1 text-muted hover:bg-canvas hover:text-ink transition-colors"
                  title="Open table"
                  onClick={(e) => e.stopPropagation()}
                >
                  <Icon name="chevron" className="size-3.5" />
                </Link>
              </div>

              <div className="max-h-48 overflow-y-auto overscroll-contain p-2 text-xs divide-y divide-line/40 [scrollbar-width:thin]">
                {data?.structure?.columns.map((col) => {
                  const isPK = data.primaryKeys.has(col.name)
                  const isFK = data.foreignKeys.some((f) => f.column === col.name)
                  return (
                    <div
                      key={col.name}
                      className="flex items-center justify-between py-1 px-1.5 gap-2 hover:bg-canvas/50 rounded"
                    >
                      <div className="flex items-center gap-1.5 min-w-0">
                        {isPK ? (
                          <span title="Primary Key" className="text-warning-fg shrink-0">
                            <Icon name="key" className="size-3" />
                          </span>
                        ) : isFK ? (
                          <span title="Foreign Key" className="text-violet-fg shrink-0">
                            <Icon name="link" className="size-3" />
                          </span>
                        ) : (
                          <span className="size-3 shrink-0" />
                        )}
                        <span
                          className={cn(
                            'truncate font-mono text-[11px]',
                            isPK && 'font-bold text-ink',
                            isFK && 'text-violet-fg font-semibold',
                          )}
                        >
                          {col.name}
                        </span>
                      </div>
                      <span className="font-mono text-[11px] text-muted shrink-0">
                        {col.type}
                      </span>
                    </div>
                  )
                })}
                {(!data?.structure || data.structure.columns.length === 0) && (
                  <div className="py-2 text-center text-[11px] text-muted italic">
                    {loading ? 'Loading columns…' : 'No columns'}
                  </div>
                )}
              </div>

              <div className="flex items-center justify-between border-t border-line/60 bg-canvas/30 px-3 py-1.5 text-[11px] text-muted rounded-b-xl">
                <span>
                  {data?.structure?.columns.length ?? 0} columns
                </span>
                <div className="flex items-center gap-2.5">
                  <Link
                    to={`/databases/${databaseId}/${schema}/${encodeURIComponent(table.name)}/edit`}
                    className="text-muted hover:text-violet-fg font-medium no-underline"
                  >
                    DDL Blueprint
                  </Link>
                  <Link
                    to={`/databases/${databaseId}/${schema}/${encodeURIComponent(table.name)}`}
                    className="text-violet-fg hover:underline font-semibold"
                  >
                    View data
                  </Link>
                </div>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function SchemaPage() {
  const navigate = useNavigate()
  const { databaseId = '0', schema = 'public' } = useParams()
  const id = Number(databaseId)
  const { databases } = useWorkspaceContext()
  const databaseName =
    databases.find((database) => database.id === id)?.name ?? `Database #${id}`

  const schemas = useResource(() => api.schemas(id), [id])
  const tables = useResource(() => api.tables(id, schema), [id, schema])
  const [search, setSearch] = useState('')
  const [viewMode, setViewMode] = useState<'diagram' | 'cards'>('diagram')

  const structures = useResource(async () => {
    if (!tables.data || tables.data.length === 0) return []
    const results = await Promise.all(
      tables.data.map(async (t) => {
        try {
          return await api.structure(id, schema, t.name)
        } catch {
          return null
        }
      }),
    )
    return results.filter((r): r is TableStructure => r !== null)
  }, [id, schema, tables.data])

  const isLoading = (schemas.loading && !schemas.data) || (tables.loading && !tables.data)

  const filteredTables =
    tables.data?.filter((t) =>
      t.name.toLowerCase().includes(search.toLowerCase()),
    ) ?? []

  const { tableMap, relationships } = parseTableRelationships(
    tables.data ?? [],
    structures.data ?? [],
  )

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
                  navigate(`/databases/${id}/${e.target.value}/schema`)
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
        title="Schema"
        status={
          isLoading
            ? 'Loading schema…'
            : `${tables.data?.length ?? 0} tables · ${relationships.length} relations`
        }
        actions={
          <div className="flex flex-wrap items-center gap-2.5 w-full min-[901px]:w-auto">
            <div className="flex items-center rounded-lg border border-line bg-white p-0.5 shadow-2xs shrink-0">
              <button
                type="button"
                onClick={() => setViewMode('diagram')}
                className={cn(
                  'flex items-center gap-1.5 rounded-md px-2.5 sm:px-3 py-1.5 text-xs font-bold transition-colors cursor-pointer',
                  viewMode === 'diagram'
                    ? 'bg-ink text-white shadow-2xs'
                    : 'text-muted hover:text-ink',
                )}
              >
                <Icon name="schema" className="size-3.5" />
                Diagram
              </button>
              <button
                type="button"
                onClick={() => setViewMode('cards')}
                className={cn(
                  'flex items-center gap-1.5 rounded-md px-2.5 sm:px-3 py-1.5 text-xs font-bold transition-colors cursor-pointer',
                  viewMode === 'cards'
                    ? 'bg-ink text-white shadow-2xs'
                    : 'text-muted hover:text-ink',
                )}
              >
                <Icon name="grid" className="size-3.5" />
                Cards
              </button>
            </div>
            {(tables.data?.length ?? 0) > 3 && (
              <Search
                value={search}
                onChange={setSearch}
                placeholder="Find table…"
                className="min-w-0 flex-1 basis-[180px] sm:flex-initial sm:min-w-48"
              />
            )}
          </div>
        }
      />

      <AnimatePresence mode="wait">
        {isLoading ? (
          <m.div
            key="page-loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
          >
            <PageSkeleton variant="schema" label="Inspecting database schema and relations…" />
          </m.div>
        ) : (
          <m.div
            key="page-content"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.22, ease: 'easeOut' }}
            className="space-y-4"
          >
            {(schemas.error || tables.error || structures.error) && (
              <ErrorNotice
                message={schemas.error || tables.error || structures.error}
              />
            )}

            {viewMode === 'diagram' ? (
              <ConnectedSchemaDiagram
                databaseId={id}
                schema={schema}
                tables={filteredTables}
                tableMap={tableMap}
                relationships={relationships}
                loading={structures.loading}
              />
            ) : (
              <div className="grid min-h-[500px] min-w-0 max-w-full grid-cols-1 lg:grid-cols-[minmax(0,1fr)_280px] gap-5">
                <section className="min-w-0 rounded-2xl border border-line bg-surface-subtle p-3.5 sm:p-5 shadow-xs">
                  <div className="grid grid-cols-1 sm:grid-cols-[repeat(auto-fill,minmax(250px,1fr))] gap-3.5">
                    {filteredTables.map((table) => {
                      const data = tableMap[table.name]
                      const colCount = data?.structure?.columns.length ?? 0
                      return (
                        <m.article
                          key={table.name}
                          whileHover={{ y: -2 }}
                          whileTap={{ scale: 0.98 }}
                          transition={{ duration: 0.15, ease: 'easeOut' }}
                          className="group flex flex-col justify-between rounded-2xl border border-line/80 bg-white p-4 shadow-2xs transition-[border-color,box-shadow] duration-150 hover:border-violet/50 hover:shadow-surface"
                        >
                          <div>
                            <Link
                              to={`/databases/${id}/${table.schema}/${encodeURIComponent(table.name)}`}
                              className="flex items-center gap-2 pb-2.5 border-b border-line no-underline"
                            >
                              <span className="grid size-8 place-items-center rounded-lg bg-violet-soft text-violet-fg group-hover:scale-105 transition-transform">
                                <Icon name="table" className="size-4" />
                              </span>
                              <strong className="truncate text-sm font-bold text-ink flex-1 group-hover:text-violet-fg transition-colors">
                                {table.name}
                              </strong>
                              {table.kind === 'view' && (
                                <span className="rounded bg-accent-soft px-1.5 py-0.5 text-[11px] font-bold text-accent-fg uppercase">
                                  View
                                </span>
                              )}
                            </Link>
                            <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-muted">
                              <span>{colCount > 0 ? `${colCount} columns` : 'View definition'}</span>
                              <div className="flex items-center gap-2.5">
                                <Link
                                  to={`/databases/${id}/${table.schema}/${encodeURIComponent(table.name)}/edit`}
                                  className="text-xs font-medium text-muted hover:text-violet-fg no-underline"
                                >
                                  DDL Blueprint
                                </Link>
                                <Link
                                  to={`/databases/${id}/${table.schema}/${encodeURIComponent(table.name)}`}
                                  className="text-violet-fg font-medium no-underline hover:underline"
                                >
                                  View table →
                                </Link>
                              </div>
                            </div>
                          </div>
                        </m.article>
                      )
                    })}
                  </div>

                  {filteredTables.length === 0 && (
                    <EmptyState
                      icon={search ? 'search' : 'table'}
                      title={search ? 'No tables match query' : 'No tables in schema'}
                      action={
                        search ? (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => setSearch('')}
                          >
                            Clear search filter
                          </Button>
                        ) : undefined
                      }
                    >
                      {search
                        ? `No database objects in schema "${schema}" matched "${search}".`
                        : `The schema "${schema}" contains no tables or views.`}
                    </EmptyState>
                  )}
                </section>

                <aside className="h-fit rounded-2xl border border-line/80 bg-white p-6 shadow-2xs space-y-6 text-xs">
                  <div className="pb-3 border-b border-line/60">
                    <span className="text-[11px] font-extrabold uppercase tracking-wider text-violet-fg">
                      Active Schema
                    </span>
                    <h2 className="text-2xl font-black text-ink mt-1 mb-0 tracking-tight">
                      {schema}
                    </h2>
                  </div>

                  <div className="space-y-5">
                    <div>
                      <span className="text-[11px] font-bold uppercase tracking-wider text-muted block mb-1.5">
                        Database
                      </span>
                      <span className="font-semibold text-sm text-ink">{databaseName}</span>
                    </div>

                    <div>
                      <span className="text-[11px] font-bold uppercase tracking-wider text-muted block mb-2">
                        Available Schemas
                      </span>
                      <div className="flex flex-wrap gap-2">
                        {schemas.data?.map((s) => (
                          <button
                            key={s}
                            type="button"
                            onClick={() => navigate(`/databases/${id}/${s}/schema`)}
                            className={cn(
                              'rounded-full px-3 py-1 font-mono text-xs border transition-colors cursor-pointer',
                              s === schema
                                ? 'border-violet/30 bg-violet-soft text-violet-fg font-bold shadow-2xs'
                                : 'border-line bg-canvas text-muted hover:border-line-strong hover:text-ink',
                            )}
                          >
                            {s}
                          </button>
                        ))}
                      </div>
                    </div>

                    <div>
                      <span className="text-[11px] font-bold uppercase tracking-wider text-muted block mb-1.5">
                        Objects
                      </span>
                      <span className="font-semibold text-sm text-ink">
                        {tables.data?.length ?? 0} tables
                      </span>
                    </div>
                  </div>
                </aside>
              </div>
            )}
          </m.div>
        )}
      </AnimatePresence>
    </>
  )
}
