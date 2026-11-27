import {
  useRef,
  useState,
  type KeyboardEvent,
} from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { AnimatePresence, m } from 'motion/react'
import { api, type QueryResult, type TableStructure } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { useWorkspaceContext } from '../../components/workspaceContext'
import { Icon } from '../../components/Icon'
import {
  Button,
  EmptyState,
  ErrorNotice,
  Modal,
  NullBadge,
  Status,
} from '../../components/ui'
import { useResource } from '../../hooks'
import { cn } from '../../lib/utils'

function formatSQL(sql: string): string {
  if (!sql.trim()) return ''

  // Preserve string literals and comments
  const placeholders: string[] = []
  let safeSql = sql.replace(/'(?:''|[^'])*'/g, (match) => {
    placeholders.push(match)
    return `__STR_${placeholders.length - 1}__`
  })

  // Normalize all clause keywords to uppercase
  const majorClauses = [
    'SELECT', 'FROM', 'WHERE', 'GROUP BY', 'HAVING', 'ORDER BY',
    'LIMIT', 'OFFSET', 'LEFT JOIN', 'RIGHT JOIN', 'INNER JOIN',
    'FULL OUTER JOIN', 'FULL JOIN', 'CROSS JOIN', 'JOIN',
    'INSERT INTO', 'VALUES', 'UPDATE', 'SET', 'DELETE FROM',
    'CREATE TABLE', 'ALTER TABLE', 'DROP TABLE', 'UNION ALL', 'UNION',
  ]
  const subClauses = ['AND', 'OR', 'ON']

  majorClauses.forEach((kw) => {
    safeSql = safeSql.replace(new RegExp(`\\b${kw}\\b`, 'gi'), kw)
  })
  subClauses.forEach((kw) => {
    safeSql = safeSql.replace(new RegExp(`\\b${kw}\\b`, 'gi'), kw)
  })

  // Add newline before major clauses
  majorClauses.forEach((kw) => {
    safeSql = safeSql.replace(new RegExp(`\\s*\\b(${kw})\\b\\s*`, 'g'), '\n$1 ')
  })
  // Add newline + indent before sub clauses
  subClauses.forEach((kw) => {
    safeSql = safeSql.replace(new RegExp(`\\s*\\b(${kw})\\b\\s*`, 'g'), '\n  $1 ')
  })

  // Format comma separated select items onto clean lines
  const lines = safeSql.split('\n').map((l) => l.trim()).filter(Boolean)
  const formattedLines: string[] = []

  for (const line of lines) {
    if (line.startsWith('SELECT ')) {
      const selectContent = line.slice(7).trim()
      if (selectContent.includes(',')) {
        formattedLines.push('SELECT')
        const items = selectContent.split(',').map((it) => it.trim()).filter(Boolean)
        items.forEach((item, idx) => {
          formattedLines.push(`  ${item}${idx < items.length - 1 ? ',' : ''}`)
        })
        continue
      }
    }
    formattedLines.push(line)
  }

  let result = formattedLines.join('\n').trim()

  // Restore string literals
  placeholders.forEach((val, idx) => {
    result = result.replace(`__STR_${idx}__`, val)
  })

  if (result && !result.endsWith(';')) {
    result += ';'
  }

  return result
}

type ExplainPlanNode = {
  'Node Type'?: string
  'Relation Name'?: string
  'Schema'?: string
  'Alias'?: string
  'Startup Cost'?: number
  'Total Cost'?: number
  'Plan Rows'?: number
  'Plan Width'?: number
  'Filter'?: string
  'Index Name'?: string
  'Index Cond'?: string
  Plans?: ExplainPlanNode[]
}

function PlanCard({ node, index }: { node: ExplainPlanNode; index: number }) {
  if (!node || typeof node !== 'object') return null

  return (
    <div className="rounded-xl border border-line bg-surface-subtle p-4 shadow-2xs">
      <div className="flex items-center gap-2 mb-3">
        <Status tone="violet">Step #{index}</Status>
        <span className="font-bold text-sm text-ink">{node['Node Type'] ?? 'Scan Node'}</span>
        {node['Relation Name'] && (
          <span className="font-mono text-xs text-muted">
            on <b>{node['Relation Name']}</b>
          </span>
        )}
      </div>

      <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs">
        <div className="rounded-lg border border-line bg-white p-2.5">
          <span className="text-[11px] uppercase font-bold text-muted block mb-0.5">
            Total Cost
          </span>
          <span className="font-mono font-semibold text-ink">
            {node['Total Cost'] ?? '—'}
          </span>
        </div>
        <div className="rounded-lg border border-line bg-white p-2.5">
          <span className="text-[11px] uppercase font-bold text-muted block mb-0.5">
            Startup Cost
          </span>
          <span className="font-mono font-semibold text-ink">
            {node['Startup Cost'] ?? '—'}
          </span>
        </div>
        <div className="rounded-lg border border-line bg-white p-2.5">
          <span className="text-[11px] uppercase font-bold text-muted block mb-0.5">
            Est. Rows
          </span>
          <span className="font-mono font-semibold text-ink">
            {node['Plan Rows'] ?? '—'}
          </span>
        </div>
        <div className="rounded-lg border border-line bg-white p-2.5">
          <span className="text-[11px] uppercase font-bold text-muted block mb-0.5">
            Plan Width
          </span>
          <span className="font-mono font-semibold text-ink">
            {node['Plan Width'] ?? '—'}
          </span>
        </div>
      </div>

      {node['Filter'] && (
        <div className="mt-3 rounded-lg bg-warning-soft/60 border border-warning/25 p-2.5 text-xs">
          <span className="text-[11px] font-bold text-warning-fg uppercase block mb-0.5">
            Filter Condition
          </span>
          <code className="font-mono text-xs text-ink">{node['Filter']}</code>
        </div>
      )}

      {node['Index Cond'] && (
        <div className="mt-3 rounded-lg bg-success-soft/60 border border-success/25 p-2.5 text-xs">
          <span className="text-[11px] font-bold text-success-fg uppercase block mb-0.5">
            Index Condition ({node['Index Name']})
          </span>
          <code className="font-mono text-xs text-ink">{node['Index Cond']}</code>
        </div>
      )}
    </div>
  )
}

export function QueryPage() {
  const navigate = useNavigate()
  const { databaseId = '0', schema = 'public' } = useParams()
  const [searchParams] = useSearchParams()
  const id = Number(databaseId)
  const { databases } = useWorkspaceContext()

  const activeDb = databases.find((database) => database.id === id)
  const databaseName = activeDb?.name ?? `Database #${id}`
  const canEdit = activeDb?.permission === 'view_edit'

  const schemas = useResource(() => api.schemas(id), [id])
  const tables = useResource(() => api.tables(id, schema), [id, schema])

  const initialSqlParam = searchParams.get('sql')
  const initialTableParam = searchParams.get('table')
  const [editedSql, setEditedSql] = useState<string | null>(
    initialSqlParam ??
      (initialTableParam
        ? `SELECT * FROM ${schema}.${initialTableParam};`
        : null),
  )

  const defaultTableName = tables.data?.[0]?.name
  const sql =
    editedSql ??
    (defaultTableName
      ? `SELECT * FROM ${schema}.${defaultTableName};`
      : `-- Select a table from the schema reference on the left\nSELECT 1;`)

  const [expandedTable, setExpandedTable] = useState<string | null>(
    initialTableParam ?? null,
  )
  const [structuresByTable, setStructuresByTable] = useState<
    Record<string, TableStructure>
  >({})
  const [tableFilter, setTableFilter] = useState('')
  const [queryResult, setQueryResult] = useState<QueryResult | null>(null)
  const [queryBusy, setQueryBusy] = useState(false)
  const [pageSize, setPageSize] = useState(50)
  const [executedSql, setExecutedSql] = useState('')
  const [activeTab, setActiveTab] = useState<'results' | 'plan'>('results')
  const [plan, setPlan] = useState<unknown[] | null>(null)
  const [rawPlan, setRawPlan] = useState('')
  const [planTab, setPlanTab] = useState<'visual' | 'json'>('visual')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const [copiedCell, setCopiedCell] = useState<string | null>(null)
  const [mobileSchemaOpen, setMobileSchemaOpen] = useState(false)
  const [schemaPickerOpen, setSchemaPickerOpen] = useState(false)

  const lineNumbersRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const lineCount = Math.max(1, sql.split('\n').length)

  function handleScroll() {
    if (textareaRef.current && lineNumbersRef.current) {
      lineNumbersRef.current.scrollTop = textareaRef.current.scrollTop
    }
  }

  async function executeQueryPage(
    targetSql: string,
    targetPage: number,
    targetPageSize: number,
  ) {
    if (!targetSql.trim()) return
    setQueryBusy(true)
    setError('')
    try {
      const result = await api.query(id, targetSql, targetPage, targetPageSize)
      setQueryResult(result)
      setExecutedSql(targetSql)
      setActiveTab('results')
    } catch (caught) {
      setError(
        caught instanceof Error ? caught.message : 'Unable to execute query',
      )
    } finally {
      setQueryBusy(false)
    }
  }

  function runQuery() {
    void executeQueryPage(sql, 1, pageSize)
  }

  function goToPage(targetPage: number) {
    if (targetPage < 1 || !executedSql.trim()) return
    void executeQueryPage(executedSql, targetPage, pageSize)
  }

  function handlePageSizeChange(nextSize: number) {
    setPageSize(nextSize)
    if (executedSql.trim() && queryResult) {
      void executeQueryPage(executedSql, 1, nextSize)
    }
  }

  async function explain() {
    if (!sql.trim()) return
    setBusy(true)
    setError('')
    try {
      const result = await api.explain(id, sql)
      setRawPlan(JSON.stringify(result.statements, null, 2))
      const plans = result.statements.flatMap((s) => s.plan)
      setPlan(plans)
      setActiveTab('plan')
    } catch (caught) {
      setError(
        caught instanceof Error ? caught.message : 'Unable to explain query',
      )
    } finally {
      setBusy(false)
    }
  }

  function handleKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault()
      if (e.shiftKey) {
        void explain()
      } else {
        void runQuery()
      }
    }
  }

  function handleFormat() {
    setEditedSql(formatSQL(sql))
  }

  function handleCopyPlan() {
    if (!rawPlan) return
    void navigator.clipboard?.writeText(rawPlan)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  function copyCellValue(val: string | null, key: string) {
    if (val === null) return
    void navigator.clipboard?.writeText(val)
    setCopiedCell(key)
    setTimeout(() => setCopiedCell(null), 1500)
  }

  function handleExportCsv() {
    if (!queryResult || queryResult.rows.length === 0) return
    const escapeCsv = (val: string | null) => {
      if (val === null) return ''
      if (val.includes(',') || val.includes('"') || val.includes('\n')) {
        return `"${val.replace(/"/g, '""')}"`
      }
      return val
    }
    const header = queryResult.columns.map(escapeCsv).join(',')
    const rows = queryResult.rows
      .map((row) => row.map(escapeCsv).join(','))
      .join('\n')
    const csvContent = `${header}\n${rows}`
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.setAttribute(
      'download',
      `query-result-p${queryResult.page}-${Date.now()}.csv`,
    )
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  }

  async function toggleTableReference(tableName: string) {
    if (expandedTable === tableName) {
      setExpandedTable(null)
      return
    }
    setExpandedTable(tableName)
    if (structuresByTable[tableName]) return
    try {
      const st = await api.structure(id, schema, tableName)
      setStructuresByTable((prev) => ({ ...prev, [tableName]: st }))
    } catch {
      /* ignore sidebar expansion errors */
    }
  }

  function insertSnippet(snippet: string) {
    const el = textareaRef.current
    if (!el) {
      setEditedSql(sql + (sql.endsWith(' ') || sql.endsWith('\n') ? '' : ' ') + snippet)
      return
    }
    const start = el.selectionStart ?? sql.length
    const end = el.selectionEnd ?? sql.length
    const next = sql.slice(0, start) + snippet + sql.slice(end)
    setEditedSql(next)
    window.requestAnimationFrame(() => {
      el.focus()
      const caret = start + snippet.length
      el.setSelectionRange(caret, caret)
    })
  }

  function stageAsMigrationDraft() {
    const params = new URLSearchParams({
      newDraft: '1',
      sql,
    })
    navigate(`/databases/${id}/migrations?${params.toString()}`)
  }

  const visibleTables =
    tables.data?.filter((t) =>
      t.name.toLowerCase().includes(tableFilter.toLowerCase()),
    ) ?? []

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
              <button
                type="button"
                aria-label="Select schema"
                aria-expanded={schemaPickerOpen}
                onClick={() => setSchemaPickerOpen(true)}
                className="inline-flex max-w-[130px] shrink-0 items-center gap-1 rounded-md border border-line bg-canvas px-2 py-0.5 font-mono text-xs font-semibold text-ink transition-colors hover:border-violet hover:text-violet-fg cursor-pointer"
              >
                <span className="truncate">{schema}</span>
                <Icon
                  name="chevrons-up-down"
                  className="size-3 shrink-0 text-muted"
                />
              </button>
            ) : (
              <span className="shrink-0">{schema}</span>
            )}
          </span>
        }
        title="SQL editor"
        actions={
          <>
            {canEdit && (
              <Button
                variant="outline"
                size="sm"
                onClick={stageAsMigrationDraft}
                title="Promote this SQL into a Migration Draft"
              >
                <Icon name="migration" className="size-3.5" />
                Stage Draft
              </Button>
            )}
            <Button
              variant="outline"
              size="sm"
              loading={busy}
              onClick={explain}
              title="Run EXPLAIN (FORMAT JSON) (Ctrl+Shift+Enter)"
            >
              Explain
            </Button>
            <Button
              variant="dark"
              size="sm"
              loading={queryBusy}
              onClick={runQuery}
              title="Execute read-only SQL query (Ctrl+Enter)"
            >
              <Icon name="play" className="size-3.5" />
              Run query
            </Button>
          </>
        }
      />

      {error && <ErrorNotice message={error} onDismiss={() => setError('')} />}

      <div className="grid min-h-[calc(100dvh-130px)] min-w-0 max-w-full grid-cols-[260px_minmax(0,1fr)] overflow-hidden rounded-2xl border border-line bg-white shadow-xs max-[900px]:grid-cols-[minmax(0,1fr)]">
        <aside className="flex min-h-0 flex-col justify-between border-r border-line bg-surface-subtle p-4 text-xs text-muted max-[900px]:hidden">
          <div className="flex min-h-0 flex-1 flex-col">
            <header className="mb-2.5 flex items-center justify-between">
              <span className="font-bold text-xs uppercase tracking-wider text-ink">
                Schema Reference
              </span>
              <span className="font-mono text-[11px] font-semibold text-violet-fg">{schema}</span>
            </header>

            {(tables.data?.length ?? 0) > 6 && (
              <input
                type="text"
                aria-label="Filter tables"
                placeholder="Filter tables…"
                value={tableFilter}
                onChange={(e) => setTableFilter(e.target.value)}
                className="mb-2 h-8 w-full rounded-lg border border-line bg-white px-2.5 text-xs text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25"
              />
            )}

            <div className="min-h-0 flex-1 space-y-1 overflow-y-auto pr-1 [scrollbar-width:thin]">
              {visibleTables.map((t) => {
                const isOpen = expandedTable === t.name
                const st = structuresByTable[t.name]
                return (
                  <div
                    key={t.name}
                    className="rounded-xl border border-line/80 bg-white overflow-hidden shadow-2xs transition-[border-color,box-shadow] hover:border-violet/40 hover:shadow-xs"
                  >
                    <button
                      type="button"
                      onClick={() => void toggleTableReference(t.name)}
                      className="flex w-full items-center justify-between gap-2 px-2.5 py-2 text-left text-xs font-semibold text-ink hover:bg-canvas/60 cursor-pointer"
                    >
                      <span className="flex items-center gap-1.5 min-w-0">
                        <Icon name="table" className="size-3.5 text-violet-fg shrink-0" />
                        <span className="truncate font-mono">{t.name}</span>
                      </span>
                      <Icon
                        name={isOpen ? 'chevron-down' : 'chevron'}
                        className="size-3.5 text-muted shrink-0"
                      />
                    </button>

                    <AnimatePresence initial={false}>
                      {isOpen && (
                        <m.div
                          initial={{ height: 0, opacity: 0 }}
                          animate={{ height: 'auto', opacity: 1 }}
                          exit={{ height: 0, opacity: 0 }}
                          transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
                          className="overflow-hidden border-t border-line bg-surface-subtle"
                        >
                          <div className="px-2.5 py-2 space-y-1.5">
                            {st ? (
                              <div className="space-y-0.5">
                                {st.columns.map((c) => (
                                  <button
                                    key={c.name}
                                    type="button"
                                    onClick={() => insertSnippet(c.name)}
                                    title={`Insert "${c.name}" at cursor`}
                                    className="flex w-full items-center justify-between gap-2 rounded-md px-1.5 py-1 text-left font-mono text-[11px] text-ink hover:bg-white transition-colors cursor-pointer"
                                  >
                                    <span className="truncate">{c.name}</span>
                                    <span className="shrink-0 text-[11px] text-muted">
                                      {c.type}
                                    </span>
                                  </button>
                                ))}
                              </div>
                            ) : (
                              <span className="block py-1 text-[11px] text-muted">
                                Loading columns…
                              </span>
                            )}
                          </div>
                        </m.div>
                      )}
                    </AnimatePresence>
                  </div>
                )
              })}
              {!tables.loading && visibleTables.length === 0 && (
                <p className="m-0 py-3 text-center text-xs text-muted">
                  No tables found in {schema}
                </p>
              )}
            </div>
          </div>

          <div className="mt-6 border-t border-line/70 pt-4 space-y-3.5">
            <p className="m-0 text-xs leading-relaxed text-muted">
              Press{' '}
              <kbd className="rounded-md border border-line bg-white px-1.5 py-0.5 font-mono text-[11px] font-semibold text-ink shadow-2xs">
                Ctrl+Enter
              </kbd>{' '}
              to run query, or{' '}
              <kbd className="rounded-md border border-line bg-white px-1.5 py-0.5 font-mono text-[11px] font-semibold text-ink shadow-2xs">
                Ctrl+Shift+Enter
              </kbd>{' '}
              to explain cost.
            </p>
            <div className="rounded-2xl border border-line/80 bg-white p-3.5 text-ink shadow-2xs">
              <div className="flex items-center gap-1.5 font-bold text-xs">
                <Icon name="shield" className="size-4 text-violet-fg" />
                Read-Only Execution
              </div>
              <p className="mt-1.5 mb-0 text-xs leading-relaxed text-muted">
                Executes in a safe read-only transaction with statement timeout
                and paginated results (up to 100 rows per page).
              </p>
            </div>
          </div>
        </aside>

        <section className="grid min-w-0 grid-rows-[auto_200px_auto_minmax(0,1fr)] sm:grid-rows-[auto_280px_auto_minmax(0,1fr)] overflow-hidden">
          <div className="flex min-h-[44px] min-w-0 flex-wrap items-center justify-between gap-2 border-b border-line bg-canvas/40 px-3.5 py-2 sm:px-4 text-xs font-medium text-muted">
            <span className="flex items-center gap-2">
              <span className="size-2 rounded-full bg-violet" />
              <span className="font-semibold text-ink">SQL</span>
              <span className="text-xs text-muted">
                · {lineCount} {lineCount === 1 ? 'line' : 'lines'}
              </span>
            </span>
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={() => setMobileSchemaOpen(true)}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-line bg-white px-2.5 text-xs font-semibold text-ink transition-colors hover:border-line-strong cursor-pointer min-[901px]:hidden"
                title="Insert column or table from schema"
              >
                <Icon name="columns" className="size-3.5 text-violet-fg" />
                Columns
              </button>
              <button
                type="button"
                onClick={handleFormat}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-line bg-white px-2.5 text-xs font-semibold text-ink transition-colors hover:border-line-strong cursor-pointer"
                title="Format SQL keywords"
              >
                <Icon name="code" className="size-3.5 text-muted" />
                Format
              </button>
            </div>
          </div>

          <div className="grid min-h-0 min-w-0 grid-cols-[46px_minmax(0,1fr)] overflow-hidden bg-editor-bg focus-within:ring-2 focus-within:ring-inset focus-within:ring-violet/70">
            <div
              ref={lineNumbersRef}
              className="overflow-hidden border-r border-editor-border bg-editor-gutter py-3 text-right font-mono text-xs leading-6 text-editor-muted select-none pr-3"
            >
              {Array.from({ length: lineCount }, (_, i) => (
                <div key={i}>{i + 1}</div>
              ))}
            </div>
            <textarea
              ref={textareaRef}
              className="h-full w-full min-w-0 resize-none overflow-auto border-0 bg-transparent p-3 font-mono text-base sm:text-[13px] leading-6 text-editor-fg outline-none [scrollbar-width:thin]"
              value={sql}
              onChange={(e) => setEditedSql(e.target.value)}
              onScroll={handleScroll}
              onKeyDown={handleKeyDown}
              spellCheck={false}
              aria-label="SQL query editor"
            />
          </div>

          <div className="flex min-h-[44px] flex-wrap items-center justify-between gap-2 border-b border-line bg-canvas/40 px-3.5 py-2 sm:px-4 text-xs">
            <div className="flex flex-wrap items-center gap-1.5">
              <button
                type="button"
                onClick={() => setActiveTab('results')}
                className={cn(
                  'flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-semibold transition-colors cursor-pointer',
                  activeTab === 'results'
                    ? 'bg-ink text-white'
                    : 'text-muted hover:text-ink hover:bg-canvas',
                )}
              >
                <span>Results</span>
                {queryResult && (
                  <span
                    className={cn(
                      'rounded-full px-1.5 py-0.5 font-mono text-[11px]',
                      activeTab === 'results'
                        ? 'bg-white/20 text-white'
                        : 'bg-canvas text-ink border border-line',
                    )}
                  >
                    {queryResult.page > 1 ? `p.${queryResult.page} · ` : ''}
                    {queryResult.rows.length}
                    {queryResult.has_next ? '+' : ''}
                  </span>
                )}
              </button>

              <button
                type="button"
                onClick={() => setActiveTab('plan')}
                className={cn(
                  'flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-semibold transition-colors cursor-pointer',
                  activeTab === 'plan'
                    ? 'bg-ink text-white'
                    : 'text-muted hover:text-ink hover:bg-canvas',
                )}
              >
                <span>Explain Plan</span>
              </button>

              {activeTab === 'plan' && rawPlan && (
                <div className="inline-flex items-center rounded-lg border border-line bg-white p-0.5 shadow-2xs sm:ml-2">
                  <button
                    type="button"
                    onClick={() => setPlanTab('visual')}
                    className={cn(
                      'rounded-md px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer',
                      planTab === 'visual'
                        ? 'bg-ink/10 font-semibold text-ink'
                        : 'text-muted hover:text-ink',
                    )}
                  >
                    Visual Summary
                  </button>
                  <button
                    type="button"
                    onClick={() => setPlanTab('json')}
                    className={cn(
                      'rounded-md px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer',
                      planTab === 'json'
                        ? 'bg-ink/10 font-semibold text-ink'
                        : 'text-muted hover:text-ink',
                    )}
                  >
                    Raw JSON
                  </button>
                </div>
              )}
            </div>

            <div className="flex items-center gap-2">
              {activeTab === 'results' &&
                queryResult &&
                queryResult.rows.length > 0 && (
                  <button
                    type="button"
                    onClick={handleExportCsv}
                    className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-muted hover:text-ink transition-colors cursor-pointer"
                    title="Download as CSV"
                  >
                    <Icon name="save" className="size-3.5" />
                    Export CSV
                  </button>
                )}

              {activeTab === 'plan' && rawPlan && (
                <button
                  type="button"
                  onClick={handleCopyPlan}
                  className="flex items-center gap-1 text-xs text-muted hover:text-ink transition-colors cursor-pointer"
                >
                  <Icon
                    name={copied ? 'check' : 'copy'}
                    className={cn('size-3.5', copied && 'text-success-fg')}
                  />
                  {copied ? 'Copied' : 'Copy plan'}
                </button>
              )}
            </div>
          </div>

          <div className="min-h-0 overflow-auto p-4 [scrollbar-width:thin]">
            {activeTab === 'results' ? (
              !queryResult ? (
                <EmptyState
                  icon="play"
                  title="No query executed yet"
                  action={
                    <Button
                      variant="dark"
                      size="sm"
                      onClick={runQuery}
                      loading={queryBusy}
                    >
                      <Icon name="play" className="size-3.5" />
                      Run query
                    </Button>
                  }
                >
                  Run your SQL statement to inspect rows, join data, and preview
                  results.
                </EmptyState>
              ) : (
                <div className="flex flex-col h-full min-h-0 space-y-3">
                  {queryResult.rows.length === 0 ? (
                    <div className="flex-1 flex items-center justify-center">
                      <EmptyState
                        icon="table"
                        title="0 rows returned"
                        action={
                          queryResult.page > 1 ? (
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={queryBusy}
                              onClick={() => goToPage(1)}
                            >
                              Back to page 1
                            </Button>
                          ) : undefined
                        }
                      >
                        {queryResult.page > 1
                          ? `No rows found on page ${queryResult.page}.`
                          : 'Statement executed successfully with no matching records.'}
                      </EmptyState>
                    </div>
                  ) : (
                    <div className="w-full min-w-0 max-w-full flex-1 overflow-auto overscroll-x-contain touch-pan-x [scrollbar-width:thin] rounded-xl border border-line">
                      <div className="sr-only" role="status" aria-live="polite">
                        {copiedCell ? 'Cell value copied to clipboard' : ''}
                      </div>
                      <table className="w-max min-w-full table-fixed border-collapse text-xs">
                        <thead className="sticky top-0 z-10 bg-surface-subtle shadow-2xs">
                          <tr>
                            <th className="h-10 w-[54px] border-r border-b border-line px-3 text-center font-mono text-[11px] text-muted select-none">
                              #
                            </th>
                            {queryResult.columns.map((col) => (
                              <th
                                key={col}
                                className="h-10 border-r border-b border-line px-2.5 sm:px-3.5 text-left font-semibold text-ink whitespace-nowrap"
                              >
                                <span className="font-mono text-xs">{col}</span>
                              </th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {queryResult.rows.map((row, rIdx) => (
                            <tr
                              key={rIdx}
                              className="hover:bg-canvas/50 transition-colors"
                            >
                              <td className="h-9 w-[54px] border-r border-b border-line px-3 text-center font-mono text-[11px] text-muted bg-surface-subtle/40 select-none">
                                {(queryResult.page - 1) * queryResult.page_size +
                                  rIdx +
                                  1}
                              </td>
                              {row.map((cell, cIdx) => {
                                const cellKey = `${rIdx}-${cIdx}`
                                const isCopied = copiedCell === cellKey
                                return (
                                  <td
                                    key={cIdx}
                                    className="h-9 max-w-[260px] sm:max-w-[340px] border-r border-b border-line p-0 font-mono text-xs text-ink"
                                  >
                                    {cell === null ? (
                                      <span className="flex h-full items-center px-2.5 sm:px-3.5">
                                        <NullBadge />
                                      </span>
                                    ) : (
                                      <button
                                        type="button"
                                        onClick={() => copyCellValue(cell, cellKey)}
                                        title={`${cell} (Click to copy)`}
                                        aria-label={`Copy ${queryResult.columns[cIdx]} value`}
                                        className={cn(
                                          'flex h-full w-full items-center truncate px-2.5 sm:px-3.5 text-left cursor-copy active:scale-100 hover:bg-violet-soft/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-violet/60',
                                          cIdx === 0 &&
                                            'text-violet-fg font-semibold',
                                        )}
                                      >
                                        {isCopied ? (
                                          <span className="text-[11px] font-bold text-success-fg flex items-center gap-1">
                                            <Icon name="check" className="size-3" />{' '}
                                            Copied
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
                  )}

                  <footer className="flex min-w-0 flex-wrap items-center justify-between gap-2.5 rounded-xl border border-line bg-surface-subtle px-3.5 py-2 text-xs text-muted shrink-0">
                    <div className="flex flex-wrap items-center gap-2 sm:gap-3">
                      <span>
                        Page{' '}
                        <strong className="text-ink tabular-nums">
                          {queryResult.page}
                        </strong>
                      </span>
                      {queryResult.rows.length > 0 && (
                        <>
                          <span>·</span>
                          <span className="tabular-nums">
                            Rows{' '}
                            <strong className="text-ink">
                              {(queryResult.page - 1) * queryResult.page_size +
                                1}
                            </strong>
                            –
                            <strong className="text-ink">
                              {(queryResult.page - 1) * queryResult.page_size +
                                queryResult.rows.length}
                            </strong>
                          </span>
                        </>
                      )}
                      <span className="hidden sm:inline">·</span>
                      <label className="inline-flex items-center gap-1.5">
                        <span>Per page</span>
                        <select
                          aria-label="Rows per page"
                          value={pageSize}
                          disabled={queryBusy}
                          onChange={(e) =>
                            handlePageSizeChange(Number(e.target.value))
                          }
                          className="h-8 rounded-lg border border-line bg-white px-2 font-mono text-base sm:text-xs font-semibold text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25 cursor-pointer"
                        >
                          {[25, 50, 100].map((size) => (
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
                        disabled={queryBusy || queryResult.page <= 1}
                        onClick={() => goToPage(queryResult.page - 1)}
                      >
                        <Icon name="chevron-left" className="size-3.5" />
                        Previous
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={queryBusy || !queryResult.has_next}
                        onClick={() => goToPage(queryResult.page + 1)}
                      >
                        Next
                        <Icon name="chevron" className="size-3.5" />
                      </Button>
                    </div>
                  </footer>
                </div>
              )
            ) : !rawPlan ? (
              <EmptyState
                icon="code"
                title="No query plan generated yet"
                action={
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={explain}
                    loading={busy}
                  >
                    <Icon name="play" className="size-3.5" />
                    Explain current statement
                  </Button>
                }
              >
                Click &ldquo;Explain&rdquo; to analyze the cost, node types, and
                scan strategies without altering data.
              </EmptyState>
            ) : planTab === 'json' ? (
              <pre className="m-0 font-mono text-xs leading-relaxed text-ink bg-surface-subtle p-4 rounded-xl border border-line overflow-auto">
                {rawPlan}
              </pre>
            ) : (
              <div className="space-y-3">
                {Array.isArray(plan) &&
                  plan.map((item, idx) => {
                    const record = item as Record<string, unknown>
                    const planNode = (record?.Plan ?? record) as ExplainPlanNode
                    return <PlanCard key={idx} node={planNode} index={idx + 1} />
                  })}
              </div>
            )}
          </div>
        </section>
      </div>

      <Modal
        open={mobileSchemaOpen}
        onOpenChange={setMobileSchemaOpen}
        title="Schema Reference"
        description={`Tables in ${schema} schema`}
        size="md"
      >
        <div className="space-y-3">
          {(tables.data?.length ?? 0) > 4 && (
            <input
              type="text"
              aria-label="Filter tables"
              placeholder="Filter tables…"
              value={tableFilter}
              onChange={(e) => setTableFilter(e.target.value)}
              className="h-10 w-full rounded-xl border border-line bg-canvas px-3 text-base sm:text-xs text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25"
            />
          )}
          <div className="space-y-1.5 max-h-[55vh] overflow-y-auto pr-1 [scrollbar-width:thin]">
            {visibleTables.map((t) => {
              const isOpen = expandedTable === t.name
              const st = structuresByTable[t.name]
              return (
                <div
                  key={t.name}
                  className="rounded-xl border border-line bg-white overflow-hidden shadow-2xs"
                >
                  <button
                    type="button"
                    onClick={() => void toggleTableReference(t.name)}
                    className="flex w-full items-center justify-between gap-2 px-3 py-2.5 text-left text-xs font-semibold text-ink hover:bg-canvas/60 cursor-pointer"
                  >
                    <span className="flex items-center gap-2 min-w-0">
                      <Icon name="table" className="size-4 text-violet-fg shrink-0" />
                      <span className="truncate font-mono">{t.name}</span>
                    </span>
                    <Icon
                      name={isOpen ? 'chevron-down' : 'chevron'}
                      className="size-3.5 text-muted shrink-0"
                    />
                  </button>
                  <AnimatePresence initial={false}>
                    {isOpen && (
                      <m.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: 'auto', opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        className="overflow-hidden border-t border-line bg-surface-subtle px-3 py-2 space-y-1"
                      >
                        {st ? (
                          st.columns.map((c) => (
                            <button
                              key={c.name}
                              type="button"
                              onClick={() => {
                                insertSnippet(c.name)
                                setMobileSchemaOpen(false)
                              }}
                              className="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left font-mono text-xs text-ink hover:bg-white transition-colors cursor-pointer"
                            >
                              <span className="truncate">{c.name}</span>
                              <span className="text-[11px] text-muted">{c.type}</span>
                            </button>
                          ))
                        ) : (
                          <span className="text-xs text-muted block py-1">Loading columns…</span>
                        )}
                      </m.div>
                    )}
                  </AnimatePresence>
                </div>
              )
            })}
            {!tables.loading && visibleTables.length === 0 && (
              <p className="m-0 py-4 text-center text-xs text-muted">
                No tables found in {schema}
              </p>
            )}
          </div>
        </div>
      </Modal>

      <Modal
        open={schemaPickerOpen}
        onOpenChange={setSchemaPickerOpen}
        title="Switch Schema"
        description={`Schemas in ${databaseName}`}
        size="sm"
      >
        <div className="max-h-[50dvh] space-y-1 overflow-y-auto pr-1 [scrollbar-width:thin]">
          {(schemas.data ?? []).map((s) => {
            const isCurrent = s === schema
            return (
              <button
                key={s}
                type="button"
                onClick={() => {
                  setSchemaPickerOpen(false)
                  setExpandedTable(null)
                  setEditedSql(null)
                  navigate(`/databases/${id}/${encodeURIComponent(s)}/query`)
                }}
                className={cn(
                  'flex min-h-10 w-full items-center gap-2.5 rounded-xl border px-3 py-2 text-left font-mono text-xs transition-colors cursor-pointer',
                  isCurrent
                    ? 'border-violet bg-violet-soft/60 font-bold text-violet-fg shadow-2xs'
                    : 'border-line/70 bg-white text-ink hover:bg-canvas',
                )}
              >
                <Icon
                  name="schema"
                  className={cn(
                    'size-3.5 shrink-0',
                    isCurrent ? 'text-violet-fg' : 'text-muted',
                  )}
                />
                <span className="flex-1 truncate">{s}</span>
                {isCurrent && (
                  <Icon
                    name="check"
                    className="size-3.5 shrink-0 text-violet-fg"
                  />
                )}
              </button>
            )
          })}
        </div>
      </Modal>
    </>
  )
}
