import {
  useRef,
  useState,
  type KeyboardEvent,
} from 'react'
import { useParams } from 'react-router'
import { api } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { useWorkspaceContext } from '../../components/workspaceContext'
import { Icon } from '../../components/Icon'
import {
  Button,
  EmptyState,
  ErrorNotice,
  Status,
} from '../../components/ui'
import { cn } from '../../lib/utils'

function formatSQL(sql: string): string {
  const keywords = [
    'SELECT',
    'FROM',
    'WHERE',
    'AND',
    'OR',
    'ORDER BY',
    'GROUP BY',
    'HAVING',
    'LIMIT',
    'OFFSET',
    'JOIN',
    'LEFT JOIN',
    'RIGHT JOIN',
    'INNER JOIN',
    'FULL OUTER JOIN',
    'CROSS JOIN',
    'ON',
    'INSERT INTO',
    'VALUES',
    'UPDATE',
    'SET',
    'DELETE FROM',
    'CREATE TABLE',
    'ALTER TABLE',
    'DROP TABLE',
    'UNION ALL',
    'UNION',
    'AS',
    'IN',
    'BETWEEN',
    'LIKE',
    'ILIKE',
    'IS NULL',
    'IS NOT NULL',
    'NOT',
    'DISTINCT',
    'EXISTS',
    'CASE',
    'WHEN',
    'THEN',
    'ELSE',
    'END',
  ]

  let formatted = sql.trim()
  keywords.forEach((kw) => {
    const regex = new RegExp(`\\b${kw}\\b`, 'gi')
    formatted = formatted.replace(regex, kw)
  })

  return formatted
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
  const { databaseId = '0', schema = 'public' } = useParams()
  const id = Number(databaseId)
  const { databases } = useWorkspaceContext()
  const databaseName =
    databases.find((database) => database.id === id)?.name ?? `Database #${id}`

  const [sql, setSql] = useState(
    `SELECT * FROM ${schema}.customers\nWHERE active = true\nORDER BY id DESC\nLIMIT 25;`,
  )
  const [plan, setPlan] = useState<unknown[] | null>(null)
  const [rawPlan, setRawPlan] = useState('')
  const [planTab, setPlanTab] = useState<'visual' | 'json'>('visual')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)

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
    setSql((prev) => formatSQL(prev))
  }

  function handleCopyPlan() {
    if (!rawPlan) return
    void navigator.clipboard?.writeText(rawPlan)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

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
              onClick={handleFormat}
              title="Format SQL keywords"
            >
              <Icon name="code" className="size-3.5" />
              Format
            </Button>
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

      <div className="grid min-h-[calc(100dvh-130px)] min-w-0 max-w-full grid-cols-[220px_minmax(0,1fr)] overflow-hidden rounded-[14px] border border-line bg-white shadow-xs max-[900px]:grid-cols-[minmax(0,1fr)]">
        <aside className="border-r border-line bg-[#fbfafd] p-4 text-xs text-muted max-[900px]:hidden flex flex-col justify-between">
          <div>
            <header className="font-bold text-[11px] uppercase tracking-wider text-ink mb-3">
              Context
            </header>
            <div className="rounded-lg border border-line bg-white p-3 shadow-2xs">
              <span className="text-[10px] uppercase font-bold text-violet block mb-1">
                Schema
              </span>
              <p className="font-mono text-xs font-semibold text-ink m-0">
                {schema}
              </p>
              <span className="text-[10px] uppercase font-bold text-muted block mt-2 mb-1">
                Database
              </span>
              <p className="text-xs text-muted m-0 truncate">{databaseName}</p>
            </div>
            <p className="mt-4 text-[11px] leading-relaxed text-muted">
              Press{' '}
              <kbd className="rounded border border-line bg-canvas px-1 py-0.5 font-mono text-[10px]">
                Ctrl+Enter
              </kbd>{' '}
              to run query planning.
            </p>
          </div>

          <div className="rounded-xl border border-violet/20 bg-violet-soft/60 p-3.5 text-[#654095]">
            <div className="flex items-center gap-1.5 font-bold text-xs">
              <Icon name="code" className="size-4" />
              Safe Planning Mode
            </div>
            <p className="mt-1.5 mb-0 text-[11px] leading-relaxed text-[#7854a8]">
              The API evaluates statements using PostgreSQL&apos;s safe{' '}
              <code className="font-mono text-[10px] font-bold">EXPLAIN</code>{' '}
              engine without modifying tables.
            </p>
          </div>
        </aside>

        <section className="grid min-w-0 grid-rows-[44px_280px_44px_minmax(0,1fr)] overflow-hidden">
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
              onChange={(e) => setSql(e.target.value)}
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
    </>
  )
}
