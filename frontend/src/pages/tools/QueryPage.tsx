import {
  useRef,
  useState,
  type KeyboardEvent,
} from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router'
import { AnimatePresence, m } from 'motion/react'
import { api, type TableStructure } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { useWorkspaceContext } from '../../components/workspaceContext'
import { Icon } from '../../components/Icon'
import {
  Button,
  EmptyState,
  ErrorNotice,
  Modal,
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
    <div className="rounded-xl border border-line bg-[#fdfcfe] p-4 shadow-2xs">
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
          <span className="text-[10px] uppercase font-bold text-muted block mb-0.5">
            Total Cost
          </span>
          <span className="font-mono font-semibold text-ink">
            {node['Total Cost'] ?? '—'}
          </span>
        </div>
        <div className="rounded-lg border border-line bg-white p-2.5">
          <span className="text-[10px] uppercase font-bold text-muted block mb-0.5">
            Startup Cost
          </span>
          <span className="font-mono font-semibold text-ink">
            {node['Startup Cost'] ?? '—'}
          </span>
        </div>
        <div className="rounded-lg border border-line bg-white p-2.5">
          <span className="text-[10px] uppercase font-bold text-muted block mb-0.5">
            Est. Rows
          </span>
          <span className="font-mono font-semibold text-ink">
            {node['Plan Rows'] ?? '—'}
          </span>
        </div>
        <div className="rounded-lg border border-line bg-white p-2.5">
          <span className="text-[10px] uppercase font-bold text-muted block mb-0.5">
            Plan Width
          </span>
          <span className="font-mono font-semibold text-ink">
            {node['Plan Width'] ?? '—'}
          </span>
        </div>
      </div>

      {node['Filter'] && (
        <div className="mt-3 rounded-lg bg-amber-soft/60 border border-warning/20 p-2.5 text-xs">
          <span className="text-[10px] font-bold text-warning uppercase block mb-0.5">
            Filter Condition
          </span>
          <code className="font-mono text-[11px] text-ink">{node['Filter']}</code>
        </div>
      )}

      {node['Index Cond'] && (
        <div className="mt-3 rounded-lg bg-success-soft/60 border border-success/20 p-2.5 text-xs">
          <span className="text-[10px] font-bold text-success uppercase block mb-0.5">
            Index Condition ({node['Index Name']})
          </span>
          <code className="font-mono text-[11px] text-ink">{node['Index Cond']}</code>
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

  const tables = useResource(() => api.tables(id, schema), [id, schema])

  const initialSqlParam = searchParams.get('sql')
  const initialTableParam = searchParams.get('table')
  const [editedSql, setEditedSql] = useState<string | null>(
    initialSqlParam ??
      (initialTableParam
        ? `SELECT * FROM ${schema}.${initialTableParam}\nLIMIT 25;`
        : null),
  )

  const defaultTableName = tables.data?.[0]?.name
  const sql =
    editedSql ??
    (defaultTableName
      ? `SELECT * FROM ${schema}.${defaultTableName}\nLIMIT 25;`
      : `-- Select a table from the schema reference on the left\nSELECT 1;`)

  const [expandedTable, setExpandedTable] = useState<string | null>(
    initialTableParam ?? null,
  )
  const [structuresByTable, setStructuresByTable] = useState<
    Record<string, TableStructure>
  >({})
  const [tableFilter, setTableFilter] = useState('')
  const [plan, setPlan] = useState<unknown[] | null>(null)
  const [rawPlan, setRawPlan] = useState('')
  const [planTab, setPlanTab] = useState<'visual' | 'json'>('visual')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const [mobileSchemaOpen, setMobileSchemaOpen] = useState(false)

  const lineNumbersRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const lineCount = Math.max(1, sql.split('\n').length)

  function handleScroll() {
    if (textareaRef.current && lineNumbersRef.current) {
      lineNumbersRef.current.scrollTop = textareaRef.current.scrollTop
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
      void explain()
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
        path={`${databaseName} / ${schema}`}
        title="SQL editor"
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setMobileSchemaOpen(true)}
              className="min-[901px]:hidden"
              title="View schema tables and columns"
            >
              <Icon name="table" className="size-3.5" />
              Tables
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={handleFormat}
              title="Format SQL keywords"
            >
              <Icon name="code" className="size-3.5" />
              Format
            </Button>
            {canEdit && (
              <Button
                variant="outline"
                size="sm"
                onClick={stageAsMigrationDraft}
                title="Promote this SQL into a Migration Draft"
              >
                <Icon name="migration" className="size-3.5" />
                Stage as Migration Draft
              </Button>
            )}
            <Button
              variant="dark"
              size="sm"
              loading={busy}
              onClick={explain}
              title="Run EXPLAIN (FORMAT JSON) (Ctrl+Enter)"
            >
              <Icon name="play" className="size-3.5" />
              Explain query
            </Button>
          </>
        }
      />

      {error && <ErrorNotice message={error} onDismiss={() => setError('')} />}

      <div className="grid min-h-[calc(100dvh-130px)] min-w-0 max-w-full grid-cols-[260px_minmax(0,1fr)] overflow-hidden rounded-[18px] border border-line bg-white shadow-xs max-[900px]:grid-cols-[minmax(0,1fr)]">
        <aside className="flex min-h-0 flex-col justify-between border-r border-line bg-[#fbfafd] p-4 text-xs text-muted max-[900px]:hidden">
          <div className="flex min-h-0 flex-1 flex-col">
            <header className="mb-2.5 flex items-center justify-between">
              <span className="font-bold text-[11px] uppercase tracking-wider text-ink">
                Schema Reference
              </span>
              <span className="font-mono text-[10px] text-violet">{schema}</span>
            </header>

            {(tables.data?.length ?? 0) > 6 && (
              <input
                type="text"
                placeholder="Filter tables…"
                value={tableFilter}
                onChange={(e) => setTableFilter(e.target.value)}
                className="mb-2 h-8 w-full rounded-lg border border-line bg-white px-2.5 text-[11px] text-ink outline-none focus:border-violet"
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
                      className="flex w-full items-center justify-between gap-2 px-2.5 py-2 text-left text-[11px] font-semibold text-ink hover:bg-canvas/60 cursor-pointer"
                    >
                      <span className="flex items-center gap-1.5 min-w-0">
                        <Icon name="table" className="size-3.5 text-violet shrink-0" />
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
                          className="overflow-hidden border-t border-line bg-[#fcfbfd]"
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
                                    className="flex w-full items-center justify-between gap-2 rounded-md px-1.5 py-1 text-left font-mono text-[10px] text-ink hover:bg-white transition-colors cursor-pointer"
                                  >
                                    <span className="truncate">{c.name}</span>
                                    <span className="shrink-0 text-[9px] text-muted">
                                      {c.type}
                                    </span>
                                  </button>
                                ))}
                              </div>
                            ) : (
                              <span className="block py-1 text-[10px] text-muted">
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
                <p className="m-0 py-3 text-center text-[11px] text-muted">
                  No tables found in {schema}
                </p>
              )}
            </div>
          </div>

          <div className="mt-6 border-t border-line/70 pt-4 space-y-3.5">
            <p className="m-0 text-[11px] leading-relaxed text-muted">
              Press{' '}
              <kbd className="rounded-md border border-line bg-white px-1.5 py-0.5 font-mono text-[10px] font-semibold text-ink shadow-2xs">
                Ctrl+Enter
              </kbd>{' '}
              to explain query cost.
            </p>
            <div className="rounded-2xl border border-violet/20 bg-violet-soft/50 p-3.5 text-[#5e3894] shadow-2xs">
              <div className="flex items-center gap-1.5 font-bold text-xs">
                <Icon name="shield" className="size-4" />
                Safe Planning Mode
              </div>
              <p className="mt-1.5 mb-0 text-[11px] leading-relaxed text-[#7350a8]">
                Evaluates via read-only{' '}
                <code className="font-mono text-[10px] font-bold">EXPLAIN</code>{' '}
                without mutating tables.
              </p>
            </div>
          </div>
        </aside>

        <section className="grid min-w-0 grid-rows-[44px_200px_44px_minmax(0,1fr)] sm:grid-rows-[44px_280px_44px_minmax(0,1fr)] overflow-hidden">
          <div className="flex min-w-0 items-center justify-between border-b border-line bg-canvas/40 px-4 text-xs font-medium text-muted">
            <span className="flex items-center gap-2">
              <span className="size-2 rounded-full bg-violet" />
              SQL Editor
            </span>
            <span className="text-[11px] text-muted">
              {lineCount} {lineCount === 1 ? 'line' : 'lines'}
            </span>
          </div>

          <div className="grid min-h-0 min-w-0 grid-cols-[46px_minmax(0,1fr)] overflow-hidden bg-[#242227]">
            <div
              ref={lineNumbersRef}
              className="overflow-hidden border-r border-[#3a3740] bg-[#1d1b20] py-3 text-right font-mono text-xs leading-6 text-[#77717c] select-none pr-3"
            >
              {Array.from({ length: lineCount }, (_, i) => (
                <div key={i}>{i + 1}</div>
              ))}
            </div>
            <textarea
              ref={textareaRef}
              className="h-full w-full min-w-0 resize-none overflow-auto border-0 bg-transparent p-3 font-mono text-[13px] leading-6 text-[#f2edf4] outline-none [scrollbar-width:thin]"
              value={sql}
              onChange={(e) => setEditedSql(e.target.value)}
              onScroll={handleScroll}
              onKeyDown={handleKeyDown}
              spellCheck={false}
              aria-label="SQL query editor"
            />
          </div>

          <div className="flex items-center justify-between border-b border-line bg-canvas/40 px-4 text-xs">
            <div className="flex items-center gap-2">
              <span className="font-bold text-ink">Query Execution Plan</span>
              {rawPlan && (
                <div className="flex items-center gap-1 ml-3 border-l border-line pl-3">
                  <button
                    type="button"
                    onClick={() => setPlanTab('visual')}
                    className={cn(
                      'px-2 py-0.5 rounded text-[11px] font-medium transition-colors',
                      planTab === 'visual'
                        ? 'bg-violet text-white'
                        : 'text-muted hover:text-ink',
                    )}
                  >
                    Visual Summary
                  </button>
                  <button
                    type="button"
                    onClick={() => setPlanTab('json')}
                    className={cn(
                      'px-2 py-0.5 rounded text-[11px] font-medium transition-colors',
                      planTab === 'json'
                        ? 'bg-violet text-white'
                        : 'text-muted hover:text-ink',
                    )}
                  >
                    Raw JSON
                  </button>
                </div>
              )}
            </div>

            {rawPlan && (
              <button
                type="button"
                onClick={handleCopyPlan}
                className="flex items-center gap-1 text-[11px] text-muted hover:text-ink transition-colors cursor-pointer"
              >
                <Icon
                  name={copied ? 'check' : 'copy'}
                  className={cn('size-3.5', copied && 'text-success')}
                />
                {copied ? 'Copied' : 'Copy plan'}
              </button>
            )}
          </div>

          <div className="min-h-0 overflow-auto p-4 [scrollbar-width:thin]">
            {!rawPlan ? (
              <EmptyState
                icon="code"
                title="No query plan generated yet"
                action={
                  <Button variant="outline" size="sm" onClick={explain}>
                    <Icon name="play" className="size-3.5" />
                    Explain current statement
                  </Button>
                }
              >
                Click &ldquo;Explain query&rdquo; to analyze the cost, node
                types, and scan strategies without altering data.
              </EmptyState>
            ) : planTab === 'json' ? (
              <pre className="m-0 font-mono text-xs leading-relaxed text-[#3b3442] bg-[#fbfafd] p-4 rounded-xl border border-line overflow-auto">
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
              placeholder="Filter tables…"
              value={tableFilter}
              onChange={(e) => setTableFilter(e.target.value)}
              className="h-10 w-full rounded-xl border border-line bg-canvas px-3 text-xs text-ink outline-none focus:border-violet"
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
                      <Icon name="table" className="size-4 text-violet shrink-0" />
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
                        className="overflow-hidden border-t border-line bg-[#fcfbfd] px-3 py-2 space-y-1"
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
                              <span className="text-[10px] text-muted">{c.type}</span>
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
    </>
  )
}
