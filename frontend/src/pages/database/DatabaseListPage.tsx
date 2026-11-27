import { useState } from 'react'
import { Link } from 'react-router'
import { AnimatePresence, m } from 'motion/react'
import { api, type MigrationExecution } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { InteractiveLinkCard } from '../../components/aceternity/InteractiveLinkCard'
import { DatabaseMark, Icon } from '../../components/Icon'
import { useWorkspaceContext } from '../../components/workspaceContext'
import {
  Button,
  EmptyState,
  ErrorNotice,
  PageSkeleton,
  Search,
  Status,
} from '../../components/ui'
import { useResource } from '../../hooks'
import { cn } from '../../lib/utils'

function executionTypeMeta(type: MigrationExecution['type']) {
  if (type === 'rollback') {
    return { label: 'Rollback', tone: 'amber' as const }
  }
  if (type === 'forward') {
    return { label: 'Forward', tone: 'violet' as const }
  }
  return { label: 'Schema Up', tone: 'accent' as const }
}

export function DatabaseListPage() {
  const { data, error, loading, reload } = useResource(
    () => api.databases(),
    [],
  )
  const { pendingApprovals } = useWorkspaceContext()
  const [search, setSearch] = useState('')

  const databases = data ?? []
  const dbNameById = new Map(databases.map((db) => [db.id, db.name]))

  const filtered = databases.filter(
    (db) =>
      db.name.toLowerCase().includes(search.toLowerCase()) ||
      db.permission.toLowerCase().includes(search.toLowerCase()),
  )

  return (
    <>
      <UtilityHeader
        path="Workspace"
        title="Databases"
        status={
          loading
            ? 'Loading databases…'
            : `${databases.length} accessible${
                pendingApprovals.length > 0
                  ? ` · ${pendingApprovals.length} awaiting seal`
                  : ''
              }`
        }
        actions={
          databases.length > 3 && !loading ? (
            <Search
              value={search}
              onChange={setSearch}
              placeholder="Filter databases…"
              className="w-full sm:min-w-56"
            />
          ) : undefined
        }
      />

      <AnimatePresence mode="wait">
        {loading ? (
          <m.div
            key="page-loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
          >
            <PageSkeleton variant="cards" label="Loading accessible databases…" />
          </m.div>
        ) : (
          <m.div
            key="page-content"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.22, ease: 'easeOut' }}
            className="space-y-5"
          >

      {error && <ErrorNotice message={error} />}

      {pendingApprovals.length > 0 && (
        <section className="mb-5 overflow-hidden rounded-2xl border border-warning/35 bg-warning-tint shadow-xs">
          <div className="flex items-center justify-between gap-3 border-b border-warning/20 bg-warning-soft/50 px-4 py-3">
            <div className="flex items-center gap-2.5 min-w-0">
              <span className="grid size-8 shrink-0 place-items-center rounded-xl bg-warning text-white shadow-2xs">
                <Icon name="shield" className="size-4" />
              </span>
              <h2 className="m-0 truncate text-xs font-bold uppercase tracking-wider text-ink">
                Migration Execution · Pending Your Sign-off
              </h2>
            </div>
            <Status tone="amber">{pendingApprovals.length}</Status>
          </div>

          <div className="divide-y divide-warning/15">
            {pendingApprovals.map((exec) => {
              const meta = executionTypeMeta(exec.type)
              const dbName =
                dbNameById.get(exec.database_id) ?? `Database #${exec.database_id}`
              return (
                <div
                  key={exec.id}
                  className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 bg-white/80 hover:bg-white transition-colors"
                >
                  <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
                    <span className="rounded-md border border-line bg-canvas px-1.5 py-0.5 font-mono text-[11px] font-bold text-violet-fg shrink-0">
                      #{exec.id}
                    </span>
                    <strong className="truncate text-xs font-bold text-ink">
                      {exec.name}
                    </strong>
                    <Status tone={meta.tone}>{meta.label}</Status>
                    <span className="text-xs text-muted truncate">
                      {dbName}
                    </span>
                  </div>
                  <Link
                    to={`/databases/${exec.database_id}/migrations?tab=airlock&executionId=${exec.id}`}
                    className="inline-flex min-h-8 items-center justify-center gap-1 rounded-xl bg-ink px-3.5 text-xs font-bold text-white no-underline hover:bg-ink-strong transition-colors shrink-0 shadow-xs"
                  >
                    Review
                    <Icon name="chevron" className="size-3.5" />
                  </Link>
                </div>
              )
            })}
          </div>
        </section>
      )}

      {databases.length > 0 && (
        <div className="grid min-w-0 grid-cols-1 sm:grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-3.5">
          {filtered.map((db) => {
            const isEditor = db.permission === 'view_edit'
            const dbPending = pendingApprovals.filter(
              (item) => item.database_id === db.id,
            ).length
            return (
              <InteractiveLinkCard
                key={db.id}
                to={`/databases/${db.id}/public/tables`}
                className="grid min-h-[84px] min-w-0 grid-cols-[44px_minmax(0,1fr)_auto_18px] items-center gap-3.5 p-4 text-left no-underline"
              >
                <DatabaseMark
                  connected={dbPending === 0}
                  className="relative grid size-[44px] shrink-0 place-items-center rounded-2xl bg-violet-soft text-violet-fg shadow-2xs"
                />
                <div className="min-w-0">
                  <strong className="block truncate text-sm font-bold text-ink">
                    {db.name}
                  </strong>
                  <small className="mt-0.5 block text-xs text-muted">
                    PostgreSQL · #{db.id}
                    {dbPending > 0 && (
                      <span className="ml-1.5 font-bold text-warning-fg">
                        · {dbPending} seal{dbPending === 1 ? '' : 's'}
                      </span>
                    )}
                  </small>
                </div>
                <span
                  className={cn(
                    'grid size-7 shrink-0 place-items-center rounded-full border',
                    isEditor
                      ? 'border-violet/25 bg-violet-soft text-violet-fg'
                      : 'border-line bg-canvas text-muted',
                  )}
                  title={isEditor ? 'Editor (View + Edit)' : 'Viewer (View only)'}
                  aria-label={isEditor ? 'Editor' : 'Viewer'}
                >
                  <Icon name={isEditor ? 'edit' : 'eye'} className="size-3.5" />
                </span>
                <Icon name="chevron" className="text-muted/60 size-4" />
              </InteractiveLinkCard>
            )
          })}
        </div>
      )}

      {databases.length > 0 && filtered.length === 0 && (
        <EmptyState
          icon="search"
          title="No databases found"
          action={
            <Button variant="outline" size="sm" onClick={() => setSearch('')}>
              Clear search filter
            </Button>
          }
        >
          No database matched &ldquo;{search}&rdquo;. Try another name.
        </EmptyState>
      )}

      {!loading && !databases.length && (
        <EmptyState
          title="No databases assigned"
          action={
            <Button onClick={reload}>
              <Icon name="refresh" />
              Check again
            </Button>
          }
        >
          Ask an administrator to add your account to a group with database
          access.
        </EmptyState>
      )}
          </m.div>
        )}
      </AnimatePresence>
    </>
  )
}
