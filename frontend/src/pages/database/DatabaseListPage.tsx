import { useState } from 'react'
import { AnimatePresence, m } from 'motion/react'
import { api } from '../../api'
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
} from '../../components/ui'
import { useResource } from '../../hooks'
import { cn } from '../../lib/utils'

export function DatabaseListPage() {
  const { data, error, loading, reload } = useResource(
    () => api.databases(),
    [],
  )
  const { pendingApprovals } = useWorkspaceContext()
  const [search, setSearch] = useState('')

  const databases = data ?? []

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
                  ? ` · ${pendingApprovals.length} awaiting approval`
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
                        · {dbPending} approval{dbPending === 1 ? '' : 's'}
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
