import { useState } from 'react'
import {
  api,
  type MigrationExecution,
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

  return (
    <article className="overflow-hidden rounded-[18px] border border-line bg-white p-4.5 shadow-2xs">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3.5 min-w-0 flex-1">
          <div className="min-w-16">
            <span className="block text-[10px] font-bold uppercase tracking-wider text-muted">
              Version
            </span>
            <strong className="mt-0.5 block font-mono text-sm font-extrabold text-violet">
              v{entry.version}
            </strong>
          </div>

          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="m-0 truncate text-sm font-bold text-ink">
                {entry.name}
              </h3>
              <Status tone={entry.status === 'failed' ? 'red' : 'green'}>
                {entry.status.toUpperCase()}
              </Status>
              <Status tone="gray">Attempt {entry.attempt_number}</Status>
              {isHead && (
                <Status tone="violet" className="gap-1">
                  <span className="size-1.5 rounded-full bg-violet animate-pulse" />
                  HEAD · Active Schema
                </Status>
              )}
              {entry.execution_type && (
                <Status
                  tone={
                    entry.execution_type === 'rollback'
                      ? 'amber'
                      : entry.execution_type === 'forward'
                        ? 'violet'
                        : 'green'
                  }
                >
                  {entry.execution_type.toUpperCase()}
                </Status>
              )}
            </div>
            <p className="mt-1 mb-0 text-[11px] text-muted">
              Executed by user #{entry.executed_by} ·{' '}
              {new Date(entry.executed_at).toLocaleString()}
              {entry.execution_id ? ` · Execution #${entry.execution_id}` : ''}
            </p>
          </div>
        </div>

        <div className="flex max-w-full flex-wrap items-center gap-2 sm:shrink-0">
          <span className="rounded-md border border-line bg-canvas px-2.5 py-1 font-mono text-[11px] text-muted tabular-nums">
            {entry.duration_ms} ms
          </span>
          <Button variant="outline" size="sm" onClick={toggleDetail}>
            <Icon name="code" className="size-3.5" />
            {open ? 'Hide SQL' : 'Inspect SQL'}
          </Button>
        </div>
      </div>

      {entry.status === 'failed' && (
        <p className="mt-3 mb-0 rounded-lg border border-danger/20 bg-danger-soft px-3 py-2 text-xs text-danger">
          {entry.error_code ? `${entry.error_code}: ` : ''}
          {entry.error_message || 'This migration attempt failed.'}
        </p>
      )}

      {open && (
        <div className="mt-3 rounded-xl border border-line bg-[#fbfafd] p-4 space-y-3">
          {loading && (
            <span className="text-xs text-muted">Loading SQL snapshot…</span>
          )}
          {error && <ErrorNotice message={error} />}
          {detail && (
            <>
              <div>
                <span className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-muted">
                  Executed SQL
                </span>
                <pre className="m-0 max-h-48 overflow-auto rounded-lg border border-line bg-white p-3 font-mono text-xs leading-relaxed text-ink">
                  {detail.sql}
                </pre>
              </div>
              {detail.down_sql ? (
                <div>
                  <span className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-success">
                    Rollback Plan (Down SQL)
                  </span>
                  <pre className="m-0 max-h-36 overflow-auto rounded-lg border border-line bg-white p-3 font-mono text-xs leading-relaxed text-muted">
                    {detail.down_sql}
                  </pre>
                </div>
              ) : (
                <p className="m-0 text-xs font-medium text-warning">
                  Irreversible:{' '}
                  {detail.irreversible_reason || 'No Down SQL provided.'}
                </p>
              )}
            </>
          )}
        </div>
      )}
    </article>
  )
}

export function HeadLedger({
  databaseId,
  canEdit,
  entries,
  hideRollback,
  onHideRollbackChange,
  latestSuccessfulExecution,
  onRollbackProposed,
  onForwardProposed,
}: {
  databaseId: number
  databaseName: string
  canEdit: boolean
  entries: MigrationHistoryEntry[]
  hideRollback: boolean
  onHideRollbackChange: (hide: boolean) => void
  latestSuccessfulExecution: MigrationExecution | undefined
  onRollbackProposed: (executionId: number) => Promise<void>
  onForwardProposed: (executionId: number) => Promise<void>
}) {
  const latestActive = entries.find((e) => e.active !== false)
  const canProposeRollback = Boolean(latestActive)
  const canProposeForward = latestSuccessfulExecution?.type === 'rollback'

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-[18px] border border-line/80 bg-white p-4.5 shadow-2xs">
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="m-0 text-base font-bold text-ink">Live HEAD Ledger</h2>
          <span className="rounded-full bg-violet-soft px-2.5 py-0.5 text-[11px] font-bold text-violet">
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
                description={`This submits a Forward execution to re-apply "${latestSuccessfulExecution?.name}" into Pending Review for team leader approval.`}
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
          <div className="rounded-[14px] border border-line bg-white py-12 shadow-2xs">
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
