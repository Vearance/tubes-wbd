import { useState } from 'react'
import { api } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { InteractiveLinkCard } from '../../components/aceternity/InteractiveLinkCard'
import { Icon } from '../../components/Icon'
import {
  Button,
  EmptyState,
  ErrorNotice,
  Loading,
  Search,
  Status,
} from '../../components/ui'
import { useResource } from '../../hooks'

export function DatabaseListPage() {
  const { data, error, loading, reload } = useResource(
    () => api.databases(),
    [],
  )
  const [search, setSearch] = useState('')

  if (loading) return <Loading page label="Loading accessible databases…" />

  const filtered =
    data?.filter(
      (db) =>
        db.name.toLowerCase().includes(search.toLowerCase()) ||
        db.permission.toLowerCase().includes(search.toLowerCase()),
    ) ?? []

  return (
    <>
      <UtilityHeader
        path="Workspace"
        title="Databases"
        status={`${data?.length ?? 0} accessible`}
        actions={
          (data?.length ?? 0) > 3 ? (
            <Search
              value={search}
              onChange={setSearch}
              placeholder="Filter databases…"
              className="min-w-56"
            />
          ) : undefined
        }
      />

      {error && <ErrorNotice message={error} />}

      {data && data.length > 0 && (
        <div className="grid min-w-0 grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-3.5">
          {filtered.map((db) => (
            <InteractiveLinkCard
              className="grid min-h-[84px] min-w-0 grid-cols-[40px_minmax(0,1fr)_auto_18px] items-center gap-3 p-4 text-left no-underline"
              to={`/databases/${db.id}/public/tables`}
              key={db.id}
            >
              <span className="grid size-10 place-items-center rounded-xl bg-violet-soft text-violet shadow-xs">
                <Icon name="database" className="size-5" />
              </span>
              <div className="min-w-0">
                <strong className="block truncate text-sm font-bold text-ink">
                  {db.name}
                </strong>
                <small className="mt-0.5 block text-[10px] text-muted">
                  PostgreSQL database #{db.id}
                </small>
              </div>
              <Status tone={db.permission === 'view_edit' ? 'violet' : 'accent'}>
                {db.permission === 'view_edit' ? 'Editor' : 'Viewer'}
              </Status>
              <Icon name="chevron" className="text-muted/60 size-4" />
            </InteractiveLinkCard>
          ))}
        </div>
      )}

      {data && data.length > 0 && filtered.length === 0 && (
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

      {!loading && !data?.length && (
        <EmptyState
          title="No databases assigned"
          action={
            <Button onClick={reload}>
              <Icon name="refresh" />
              Check again
            </Button>
          }
        >
          You do not have access to any databases yet. Ask an administrator to
          assign your account to a group with database access.
        </EmptyState>
      )}
    </>
  )
}
