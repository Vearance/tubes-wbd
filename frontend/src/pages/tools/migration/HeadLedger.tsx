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
  Modal,
  Status,
} from '../../../components/ui'
import { cn } from '../../../lib/utils'
import { TablePagination } from './TablePagination'

function HistoryDetailModal({
  databaseId,
  entry,
  isHead,
  onClose,
}: {
  databaseId: number
  entry: MigrationHistoryEntry | null
  isHead: boolean
  onClose: () => void
}) {
  const [detailsByVersion, setDetailsByVersion] = useState<
    Record<number, MigrationHistoryDetail>
  >({})
  const [loadingVersion, setLoadingVersion] = useState<number | null>(null)
  const [error, setError] = useState('')

  const version = entry?.version ?? null
  const detail = version != null ? detailsByVersion[version] ?? null : null
  const isLoading = version != null && loadingVersion === version && !detail

  if (version != null && !detail && loadingVersion !== version && !error) {
    setLoadingVersion(version)
    void api
      .historyEntry(databaseId, version)
      .then((res) => {
        setDetailsByVersion((prev) => ({ ...prev, [version]: res }))
      })
      .catch((err) => {
        setError(
          err instanceof Error ? err.message : 'Unable to load version details',
        )
      })
      .finally(() => {
        setLoadingVersion((curr) => (curr === version ? null : curr))
      })
  }

  const isFailed = entry?.status === 'failed'

  return (
    <Modal
      open={Boolean(entry)}
      onOpenChange={(open) => {
        if (!open) {
          setError('')
          onClose()
        }
      }}
      title={
        entry ? (
          <span className="flex flex-wrap items-center gap-2.5">
            <span
              className={cn(
                'inline-flex items-center rounded-lg px-2.5 py-0.5 font-mono text-xs font-extrabold',
                isHead
                  ? 'bg-violet text-white shadow-xs'
                  : 'bg-violet-soft/80 text-violet-fg',
              )}
            >
              v{entry.version}
            </span>
            <span className="break-words">{entry.name}</span>
          </span>
        ) : (
          'Migration Details'
        )
      }
      description={
        entry?.execution_id ? `Execution #${entry.execution_id}` : undefined
      }
      size="lg"
    >
      {entry && (
        <div className="space-y-6">
          {isFailed && (
            <div className="rounded-2xl border border-danger/25 bg-danger-soft/80 p-4 space-y-1">
              <span className="block text-[11px] font-bold uppercase tracking-wider text-danger-fg">
                Execution Error
              </span>
              <p className="m-0 font-mono text-xs leading-relaxed text-danger-fg break-words">
                {entry.error_code ? `${entry.error_code}: ` : ''}
                {entry.error_message || 'This migration attempt failed.'}
              </p>
            </div>
          )}

          {isLoading && (
            <p className="m-0 py-2 text-xs text-muted">Loading SQL snapshot…</p>
          )}
          {error && <ErrorNotice message={error} />}

          {detail && (
            <div className="space-y-5">
              <div className="space-y-2">
                <span className="block text-[11px] font-bold uppercase tracking-wider text-muted">
                  Executed SQL
                </span>
                <pre className="m-0 max-h-60 overflow-auto rounded-xl border border-line bg-surface-subtle p-4 font-mono text-xs leading-6 text-ink">
                  {detail.sql}
                </pre>
              </div>
              {detail.down_sql ? (
                <div className="space-y-2">
                  <span className="block text-[11px] font-bold uppercase tracking-wider text-success-fg">
                    Rollback Plan (Down SQL)
                  </span>
                  <pre className="m-0 max-h-48 overflow-auto rounded-xl border border-line bg-surface-subtle p-4 font-mono text-xs leading-6 text-muted">
                    {detail.down_sql}
                  </pre>
                </div>
              ) : (
                <div className="rounded-xl border border-warning/25 bg-warning-soft/40 p-4 text-xs font-medium text-warning-fg">
                  Irreversible:{' '}
                  {detail.irreversible_reason || 'No Down SQL provided.'}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </Modal>
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
  const [selectedEntry, setSelectedEntry] =
    useState<MigrationHistoryEntry | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const latestActive = entries.find((e) => e.active !== false)
  const canProposeRollback = Boolean(latestActive)
  const canProposeForward = Boolean(forwardCandidate)

  const totalPages = Math.max(1, Math.ceil(entries.length / pageSize))
  const safePage = Math.min(page, totalPages)
  const paginatedEntries = entries.slice(
    (safePage - 1) * pageSize,
    safePage * pageSize,
  )

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3.5 rounded-2xl border border-line/80 bg-white p-5 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between sm:px-6 sm:py-5 shadow-2xs">
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="m-0 text-base font-bold text-ink">Migration History</h2>
          <span
            className={cn(
              'inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-bold whitespace-nowrap',
              latestActive
                ? 'bg-violet text-white shadow-xs'
                : 'bg-violet-soft text-violet-fg',
            )}
          >
            {latestActive && (
              <span className="size-1.5 rounded-full bg-white animate-pulse" />
            )}
            {latestActive ? `v${latestActive.version} · Active HEAD` : 'Empty schema'}
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

      {entries.length === 0 ? (
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
      ) : (
        <section className="overflow-hidden rounded-2xl border border-line/80 bg-white shadow-2xs">
          <div className="w-full overflow-x-auto overscroll-x-contain touch-pan-x [scrollbar-width:thin]">
            <table className="w-full min-w-[680px] border-collapse text-left text-xs">
              <thead>
                <tr className="border-b border-line bg-surface-subtle text-[11px] font-bold uppercase tracking-wider text-muted">
                  <th className="px-5 py-3.5 whitespace-nowrap">Version</th>
                  <th className="px-5 py-3.5">Migration</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Type</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Status</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Duration</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Executed</th>
                  <th className="px-5 py-3.5 text-right whitespace-nowrap">
                    Details
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {paginatedEntries.map((entry) => {
                  const isHead = latestActive?.version === entry.version
                  const isFailed = entry.status === 'failed'
                  const isRollback = entry.execution_type === 'rollback'

                  return (
                    <tr
                      key={entry.version}
                      className={cn(
                        'transition-colors',
                        isHead
                          ? 'bg-violet-soft/45 hover:bg-violet-soft/60 ring-1 ring-inset ring-violet/25'
                          : 'hover:bg-canvas/60',
                      )}
                    >
                      <td className="px-5 py-4 whitespace-nowrap">
                        <div className="flex items-center gap-2">
                          <span
                            className={cn(
                              'inline-flex items-center rounded-lg px-2.5 py-1 font-mono text-xs font-extrabold',
                              isHead
                                ? 'bg-violet text-white shadow-xs'
                                : 'bg-violet-soft/80 text-violet-fg',
                            )}
                          >
                            v{entry.version}
                          </span>
                          {isHead && (
                            <Status tone="violet" className="gap-1.5 border-violet/35 bg-white/90 shadow-2xs">
                              <span className="size-1.5 rounded-full bg-violet animate-pulse" />
                              HEAD
                            </Status>
                          )}
                        </div>
                      </td>
                      <td className="px-5 py-4 max-w-[280px]">
                        <div
                          className={cn(
                            'truncate',
                            isHead
                              ? 'font-extrabold text-violet-fg'
                              : 'font-bold text-ink',
                          )}
                        >
                          {entry.name}
                        </div>
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap">
                        {entry.execution_type ? (
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
                        ) : (
                          <span className="text-muted">—</span>
                        )}
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap">
                        <Status tone={isFailed ? 'red' : 'green'}>
                          {entry.status.toUpperCase()}
                        </Status>
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap font-mono text-muted tabular-nums">
                        {entry.duration_ms} ms
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap text-muted">
                        <div>{new Date(entry.executed_at).toLocaleString()}</div>
                        <div className="mt-0.5 text-[11px]">
                          User #{entry.executed_by}
                          {entry.attempt_number > 1
                            ? ` · Attempt ${entry.attempt_number}`
                            : ''}
                        </div>
                      </td>
                      <td className="px-5 py-4 text-right whitespace-nowrap">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setSelectedEntry(entry)}
                        >
                          <Icon name="eye" className="size-3.5" />
                          View More
                        </Button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          <TablePagination
            page={safePage}
            pageSize={pageSize}
            totalItems={entries.length}
            onPageChange={setPage}
            onPageSizeChange={setPageSize}
          />
        </section>
      )}

      <HistoryDetailModal
        databaseId={databaseId}
        entry={selectedEntry}
        isHead={
          Boolean(selectedEntry) &&
          latestActive?.version === selectedEntry?.version
        }
        onClose={() => setSelectedEntry(null)}
      />
    </div>
  )
}
