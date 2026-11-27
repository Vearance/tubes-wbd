import { useState } from 'react'
import {
  api,
  type MigrationForwardCandidate,
  type MigrationHistoryDetail,
  type MigrationHistoryEntry,
} from '../../../api'
import { Icon } from '../../../components/Icon'
import {
  Button,
  ConfirmAction,
  EmptyState,
  ErrorNotice,
  Status,
} from '../../../components/ui'
import { cn } from '../../../lib/utils'

function HistoryEntryRow({
  databaseId,
  entry,
  isHead,
}: {
  databaseId: number
  entry: MigrationHistoryEntry
  isHead: boolean
}) {
  const [open, setOpen] = useState(false)
  const [detail, setDetail] = useState<MigrationHistoryDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  async function toggleDetail() {
    if (open) {
      setOpen(false)
      return
    }
    setOpen(true)
    if (detail) return
    setLoading(true)
    setError('')
    try {
      const res = await api.historyEntry(databaseId, entry.version)
      setDetail(res)
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'Unable to load version details',
      )
    } finally {
      setLoading(false)
    }
  }

  const isFailed = entry.status === 'failed'
  const isRollback = entry.execution_type === 'rollback'

  return (
    <article
      className={cn(
        'overflow-hidden rounded-2xl border bg-white shadow-2xs transition-colors',
        isHead
          ? 'border-violet/40 ring-1 ring-violet/10'
          : isFailed
            ? 'border-danger/30'
            : 'border-line',
      )}
    >
      {/* Card Header */}
      <header className="flex flex-col gap-2.5 border-b border-line bg-surface-subtle/40 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4 sm:px-5 sm:py-3.5">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
          <span className="inline-flex shrink-0 items-center rounded-md bg-violet-soft/80 px-2 py-0.5 font-mono text-xs font-extrabold text-violet-fg">
            v{entry.version}
          </span>
          <h3 className="m-0 min-w-0 break-words text-sm font-bold text-ink">
            {entry.name}
          </h3>
          {isHead && (
            <Status tone="violet" className="gap-1">
              <span className="size-1.5 rounded-full bg-violet animate-pulse" />
              HEAD
            </Status>
          )}
          <Status tone={isFailed ? 'red' : 'green'}>
            {entry.status.toUpperCase()}
          </Status>
          {entry.attempt_number > 1 && (
            <Status tone="gray">Attempt {entry.attempt_number}</Status>
          )}
        </div>

        <div className="flex items-center justify-between gap-2 sm:shrink-0 sm:justify-end">
          <span className="rounded-lg border border-line bg-white px-2.5 py-1 font-mono text-xs text-muted tabular-nums">
            {entry.duration_ms} ms
          </span>
          <Button variant="outline" size="sm" onClick={toggleDetail}>
            <Icon name="code" className="size-3.5" />
            {open ? 'Hide SQL' : 'Inspect SQL'}
          </Button>
        </div>
      </header>

      {/* Card Body */}
      <div className="px-4 py-3 sm:px-5 sm:py-3.5 space-y-3">
        <p className="m-0 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs leading-relaxed text-muted">
          {entry.execution_type && (
            <>
              <span
                className={cn(
                  'font-bold uppercase',
                  isRollback
                    ? 'text-warning-fg'
                    : entry.execution_type === 'forward'
                      ? 'text-violet-fg'
                      : isFailed
                        ? 'text-danger-fg'
                        : 'text-success-fg',
                )}
              >
                {entry.execution_type}
              </span>
              <span aria-hidden="true">·</span>
            </>
          )}
          <span>Executed by User #{entry.executed_by}</span>
          <span aria-hidden="true">·</span>
          <span>{new Date(entry.executed_at).toLocaleString()}</span>
          {entry.execution_id && (
            <>
              <span aria-hidden="true">·</span>
              <span className="font-mono text-[11px]">
                Exec #{entry.execution_id}
              </span>
            </>
          )}
        </p>

        {isFailed && (
          <p className="m-0 rounded-xl border border-danger/20 bg-danger-soft/80 px-3.5 py-2.5 font-mono text-xs leading-relaxed text-danger-fg break-words">
            {entry.error_code ? `${entry.error_code}: ` : ''}
            {entry.error_message || 'This migration attempt failed.'}
          </p>
        )}

        {open && (
          <div className="rounded-xl border border-line bg-surface-subtle p-3.5 sm:p-4 space-y-3">
            {loading && (
              <span className="text-xs text-muted">Loading SQL snapshot…</span>
            )}
            {error && <ErrorNotice message={error} />}
            {detail && (
              <>
                <div>
                  <span className="mb-1 block text-[11px] font-bold uppercase tracking-wider text-muted">
                    Executed SQL
                  </span>
                  <pre className="m-0 max-h-48 overflow-auto rounded-lg border border-line bg-white p-3 font-mono text-xs leading-relaxed text-ink">
                    {detail.sql}
                  </pre>
                </div>
                {detail.down_sql ? (
                  <div>
                    <span className="mb-1 block text-[11px] font-bold uppercase tracking-wider text-success-fg">
                      Rollback Plan (Down SQL)
                    </span>
                    <pre className="m-0 max-h-36 overflow-auto rounded-lg border border-line bg-white p-3 font-mono text-xs leading-relaxed text-muted">
                      {detail.down_sql}
                    </pre>
                  </div>
                ) : (
                  <p className="m-0 text-xs font-medium text-warning-fg">
                    Irreversible:{' '}
                    {detail.irreversible_reason || 'No Down SQL provided.'}
                  </p>
                )}
              </>
            )}
          </div>
        )}
      </div>
    </article>
  )
}

export function HeadLedger({
  databaseId,
  canEdit,
  entries,
  hideRollback,
  onHideRollbackChange,
  forwardCandidate,
  onRollbackProposed,
  onForwardProposed,
}: {
  databaseId: number
  databaseName: string
  canEdit: boolean
  entries: MigrationHistoryEntry[]
  hideRollback: boolean
  onHideRollbackChange: (hide: boolean) => void
  forwardCandidate: MigrationForwardCandidate | undefined
  onRollbackProposed: (executionId: number) => Promise<void>
  onForwardProposed: (executionId: number) => Promise<void>
}) {
  const latestActive = entries.find((e) => e.active !== false)
  const canProposeRollback = Boolean(latestActive)
  const canProposeForward = Boolean(forwardCandidate)

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 rounded-2xl border border-line/80 bg-white p-4 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between sm:p-4.5 shadow-2xs">
        <div className="flex flex-wrap items-center gap-2.5 sm:gap-3">
          <h2 className="m-0 text-base font-bold text-ink">Migration History</h2>
          <span className="rounded-full bg-violet-soft px-2.5 py-0.5 text-xs font-bold text-violet-fg whitespace-nowrap">
            {latestActive ? `v${latestActive.version} HEAD` : 'Empty schema'}
          </span>
          <label className="inline-flex cursor-pointer items-center gap-2 text-xs font-semibold text-muted">
            <input
              type="checkbox"
              checked={hideRollback}
              onChange={(event) => onHideRollbackChange(event.currentTarget.checked)}
              className="size-4 accent-violet"
            />
            <span title="Hide successful rollback entries and the executions they cancel">
              Hide rollback
            </span>
          </label>
        </div>

        {canEdit && (
          <div className="flex flex-wrap items-center gap-2">
            {canProposeRollback && (
              <ConfirmAction
                title={`Propose Rollback of v${latestActive?.version}?`}
                description={`This creates a Rollback execution for "${latestActive?.name}" in Pending Review. Every attached team Keyholder must approve it before the Down SQL can be queued.`}
                confirmLabel="Submit Rollback for Review"
                tone="danger"
                onConfirm={async () => {
                  const exec = await api.rollback(databaseId)
                  await onRollbackProposed(exec.id)
                }}
                trigger={
                  <Button variant="dark" size="sm">
                    <Icon name="undo" className="size-3.5" />
                    Propose Rollback
                  </Button>
                }
              />
            )}

            {canProposeForward && (
              <ConfirmAction
                title="Propose Forward of Latest Rollback?"
                description={`This submits a Forward execution to re-apply "${forwardCandidate?.name}" into Pending Review for team leader approval.`}
                confirmLabel="Submit Forward for Review"
                tone="primary"
                onConfirm={async () => {
                  const exec = await api.forward(databaseId)
                  await onForwardProposed(exec.id)
                }}
                trigger={
                  <Button variant="dark" size="sm">
                    <Icon name="redo" className="size-3.5" />
                    Propose Forward
                  </Button>
                }
              />
            )}
          </div>
        )}
      </div>

      <div className="space-y-3">
        {entries.map((entry) => (
          <HistoryEntryRow
            key={entry.version}
            databaseId={databaseId}
            entry={entry}
            isHead={latestActive?.version === entry.version}
          />
        ))}

        {entries.length === 0 && (
          <div className="rounded-2xl border border-line bg-white py-12 shadow-2xs">
            {hideRollback ? (
              <EmptyState icon="migration" title="No visible history entries">
                Turn off Hide rollback to show the complete migration history.
              </EmptyState>
            ) : (
              <EmptyState icon="migration" title="No migration history yet">
                Migration attempts will appear here after they are run.
              </EmptyState>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
