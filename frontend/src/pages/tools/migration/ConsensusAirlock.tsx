import { useEffect, useState } from 'react'
import {
  api,
  type MigrationApproval,
  type MigrationApprovalSummary,
  type MigrationExecution,
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

export type AirlockFilter =
  | 'all'
  | 'pending_review'
  | 'approved'
  | 'queue_running'
  | 'completed'

function statusMeta(
  status: MigrationExecution['status'],
  type: MigrationExecution['type'],
) {
  switch (status) {
    case 'pending_review':
      return { label: 'Pending Review', tone: 'amber' as const }
    case 'approved':
      return { label: 'Approved · Ready to Queue', tone: 'green' as const }
    case 'queued':
      return { label: 'Pending (Queue)', tone: 'violet' as const }
    case 'running':
      return { label: 'Running', tone: 'violet' as const }
    case 'success':
      return { label: 'Success', tone: 'green' as const }
    case 'failed':
      return { label: 'Failed', tone: 'red' as const }
    case 'rejected':
      return { label: 'Rejected', tone: 'red' as const }
    case 'partial':
      return {
        label: type === 'rollback' ? 'Partially Reverted' : 'Partially Applied',
        tone: 'amber' as const,
      }
    case 'compensation_pending':
      return { label: 'Compensation Pending Review', tone: 'amber' as const }
    case 'compensating':
      return { label: 'Compensating', tone: 'violet' as const }
    case 'compensated':
      return { label: 'Compensated', tone: 'green' as const }
  }
}

function typeLabel(type: MigrationExecution['type']) {
  if (type === 'rollback') return 'Rollback'
  if (type === 'forward') return 'Forward'
  return 'Schema Up'
}

function SealSlot({ approval }: { approval: MigrationApproval }) {
  const isApproved = approval.decision === 'approved'
  const isRejected = approval.decision === 'rejected'

  return (
    <div
      className={cn(
        'flex flex-col justify-between rounded-xl border p-3.5 transition-colors',
        isApproved
          ? 'border-success/35 bg-success-soft/35'
          : isRejected
            ? 'border-danger/35 bg-danger-soft/35'
            : 'border-warning/35 bg-warning-tint',
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <strong className="block truncate text-xs font-bold text-ink">
            {approval.group_name}
          </strong>
          <span className="mt-0.5 block text-xs text-muted">
            Keyholder:{' '}
            <strong className="text-ink">
              {approval.leader_username
                ? `@${approval.leader_username}`
                : approval.leader_user_id
                  ? `User #${approval.leader_user_id}`
                  : 'Unassigned'}
            </strong>
          </span>
        </div>

        <span
          className={cn(
            'grid size-7 shrink-0 place-items-center rounded-lg',
            isApproved
              ? 'bg-success text-white'
              : isRejected
                ? 'bg-danger text-white'
                : 'bg-warning-soft text-warning-fg border border-warning/30',
          )}
        >
          <Icon
            name={isApproved ? 'lock' : isRejected ? 'close' : 'clock'}
            className="size-3.5"
          />
        </span>
      </div>

      <div className="mt-2.5 pt-2 border-t border-black/5 text-xs">
        {isApproved && (
          <span className="font-semibold text-success-fg">
            Approved by @
            {approval.decided_by_username ?? `user #${approval.decided_by}`}
            {approval.decided_at
              ? ` · ${new Date(approval.decided_at).toLocaleTimeString()}`
              : ''}
          </span>
        )}
        {isRejected && (
          <span className="font-semibold text-danger-fg">
            Rejected by @
            {approval.decided_by_username ?? `user #${approval.decided_by}`}
          </span>
        )}
        {!isApproved && !isRejected && (
          <span className="font-medium text-warning-fg">
            Awaiting leader decision
          </span>
        )}
        {approval.note && (
          <p className="mt-1.5 mb-0 rounded-lg bg-white/85 px-2.5 py-1.5 text-xs text-ink border border-line/60">
            &ldquo;{approval.note}&rdquo;
          </p>
        )}
      </div>
    </div>
  )
}

function ExecutionDetailModal({
  execution,
  canEdit,
  onClose,
  onMutated,
}: {
  execution: MigrationExecution | null
  canEdit: boolean
  onClose: () => void
  onMutated: (message: string) => Promise<void>
}) {
  const [summary, setSummary] = useState<MigrationApprovalSummary | null>(null)
  const [summaryUnavailable, setSummaryUnavailable] = useState(false)
  const [summaryLoading, setSummaryLoading] = useState(false)
  const [note, setNote] = useState('')
  const [actionBusy, setActionBusy] = useState(false)
  const [actionError, setActionError] = useState('')

  const execId = execution?.id ?? null
  const execStatus = execution?.status ?? null
  const execUpdated = execution?.updated_at ?? null

  useEffect(() => {
    if (execId == null) return
    let active = true
    queueMicrotask(async () => {
      setSummaryLoading(true)
      setSummaryUnavailable(false)
      setActionError('')
      try {
        const res = await api.executionApprovals(execId)
        if (active) setSummary(res)
      } catch {
        if (active) setSummaryUnavailable(true)
      } finally {
        if (active) setSummaryLoading(false)
      }
    })
    return () => {
      active = false
    }
  }, [execId, execStatus, execUpdated])

  async function handleApprove() {
    if (!execution) return
    setActionBusy(true)
    setActionError('')
    try {
      const updated = await api.approveExecution(execution.id, note)
      setSummary(updated)
      setNote('')
      await onMutated(
        updated.execution_status === 'approved'
          ? `Execution #${execution.id} received all group approvals and is ready to queue!`
          : `Approval recorded for Execution #${execution.id}. Waiting for remaining team Keyholders.`,
      )
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : 'Unable to record approval',
      )
    } finally {
      setActionBusy(false)
    }
  }

  async function handleReject() {
    if (!execution) return
    if (!note.trim()) {
      setActionError(
        'Please enter a rejection reason so the author knows what to fix.',
      )
      return
    }
    setActionBusy(true)
    setActionError('')
    try {
      const updated = await api.rejectExecution(execution.id, note)
      setSummary(updated)
      setNote('')
      await onMutated(
        `Execution #${execution.id} rejected. The draft has been reopened in the Workbench.`,
      )
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : 'Unable to reject execution',
      )
    } finally {
      setActionBusy(false)
    }
  }

  async function handleEnqueue(resumingPartial = false) {
    if (!execution) return
    setActionBusy(true)
    setActionError('')
    try {
      await api.enqueueExecution(execution.id)
      await onMutated(
        resumingPartial
          ? `Execution #${execution.id} resumed from its saved batch checkpoint.`
          : `Execution #${execution.id} released to the RabbitMQ worker queue.`,
      )
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : 'Unable to queue execution',
      )
    } finally {
      setActionBusy(false)
    }
  }

  async function handleCompensate() {
    if (!execution) return
    setActionBusy(true)
    setActionError('')
    try {
      const compensation = await api.compensateExecution(execution.id)
      await onMutated(
        `Compensation Execution #${compensation.id} submitted for group approval.`,
      )
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : 'Unable to submit compensation',
      )
    } finally {
      setActionBusy(false)
    }
  }

  const approvedCount =
    summary?.approvals.filter((a) => a.decision === 'approved').length ?? 0
  const totalCount = summary?.approvals.length ?? 0

  return (
    <Modal
      open={Boolean(execution)}
      onOpenChange={(open) => {
        if (!open) {
          setSummary(null)
          setNote('')
          setActionError('')
          onClose()
        }
      }}
      title={execution ? execution.name : 'Execution Details'}
      description={
        execution
          ? `Execution #${execution.id} · Draft #${execution.draft_id}`
          : undefined
      }
      size="xl"
    >
      {execution && (
        <div className="space-y-6">

          {actionError && (
            <ErrorNotice
              message={actionError}
              onDismiss={() => setActionError('')}
            />
          )}

          {summaryLoading && !summary && (
            <p className="m-0 text-xs text-muted">Loading group approvals…</p>
          )}

          {summaryUnavailable && (
            <p className="m-0 rounded-xl border border-dashed border-line p-3 text-xs text-muted">
              Per-group Keyholder approval details are unavailable from the
              current backend instance.
            </p>
          )}

          {summary && summary.approvals.length > 0 && (
            <div className="space-y-3">
              <div className="space-y-1.5">
                <div className="flex items-center justify-between gap-2 text-xs">
                  <span className="font-bold text-ink">
                    Group Approvals ({approvedCount}/{totalCount})
                  </span>
                  <span className="font-mono text-[11px] text-muted tabular-nums">
                    {totalCount > 0
                      ? `${Math.round((approvedCount / totalCount) * 100)}%`
                      : '0%'}
                  </span>
                </div>
                <div className="h-2 w-full overflow-hidden rounded-full bg-canvas border border-line">
                  <div
                    className={cn(
                      'h-full rounded-full transition-[width,background-color] duration-300 ease-out',
                      approvedCount === totalCount
                        ? 'bg-success'
                        : 'bg-violet',
                    )}
                    style={{
                      width: `${totalCount > 0 ? (approvedCount / totalCount) * 100 : 0}%`,
                    }}
                  />
                </div>
              </div>
              <div className="grid grid-cols-1 gap-2.5">
                {summary.approvals.map((approval) => (
                  <SealSlot key={approval.group_id} approval={approval} />
                ))}
              </div>
            </div>
          )}

          {execution.status === 'pending_review' && summary?.can_decide && (
            <div className="rounded-xl border border-violet/30 bg-violet-soft/30 p-4 space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <strong className="text-xs font-bold text-ink">
                  Keyholder Decision
                </strong>
                <Status tone="violet" className="gap-1">
                  <Icon name="crown" className="size-3" />
                  Keyholder
                </Status>
              </div>

              <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <input
                  type="text"
                  maxLength={2000}
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  placeholder="Optional approval note, or required rejection reason…"
                  className="h-10 flex-1 rounded-xl border border-line-strong bg-white px-3 text-base sm:text-xs text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25"
                />
                <div className="flex flex-wrap items-center gap-2 sm:shrink-0">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={actionBusy}
                    onClick={handleReject}
                    className="flex-1 sm:flex-initial border-danger/40 text-danger-fg hover:bg-danger-soft"
                  >
                    <Icon name="close" className="size-3.5" />
                    Reject Execution
                  </Button>
                  <Button
                    variant="dark"
                    size="sm"
                    loading={actionBusy}
                    onClick={handleApprove}
                    className="flex-1 sm:flex-initial"
                  >
                    <Icon name="shield" className="size-3.5" />
                    Approve
                  </Button>
                </div>
              </div>
            </div>
          )}

          {execution.status === 'approved' && (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-success/35 bg-success-soft/45 p-4">
              <div className="flex items-center gap-3 min-w-0">
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-success text-white">
                  <Icon name="unlock" className="size-4" />
                </span>
                <strong className="text-xs font-bold text-ink">
                  {canEdit
                    ? 'All Groups Approved · Ready to Queue'
                    : 'All Groups Approved · Awaiting Editor to Queue'}
                </strong>
              </div>
              {canEdit && (
                <ConfirmAction
                  title={`Queue Execution #${execution.id} for Worker?`}
                  description={`Places "${execution.name}" onto the worker execution queue.`}
                  confirmLabel="Release to Queue"
                  tone="primary"
                  onConfirm={() => handleEnqueue()}
                  trigger={
                    <Button variant="dark" size="sm" loading={actionBusy}>
                      <Icon name="play" className="size-3.5" />
                      Release to Queue
                    </Button>
                  }
                />
              )}
            </div>
          )}

          {(execution.status === 'queued' ||
            execution.status === 'running' ||
            execution.status === 'success' ||
            execution.status === 'failed' ||
            execution.status === 'partial' ||
            execution.status === 'compensation_pending' ||
            execution.status === 'compensating' ||
            execution.status === 'compensated') && (
            <div className="rounded-xl border border-line bg-canvas/70 p-3.5 text-xs space-y-2.5">
              <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
                <span className="font-bold text-ink">
                  Worker Telemetry · Attempt #{execution.attempt}
                </span>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-muted">
                  {execution.queued_at && (
                    <span>
                      Queued {new Date(execution.queued_at).toLocaleTimeString()}
                    </span>
                  )}
                  {execution.started_at && (
                    <span>
                      Started{' '}
                      {new Date(execution.started_at).toLocaleTimeString()}
                    </span>
                  )}
                  {execution.finished_at && (
                    <span>
                      Finished{' '}
                      {new Date(execution.finished_at).toLocaleTimeString()}
                    </span>
                  )}
                  {execution.version != null && (
                    <Status tone="green">Version v{execution.version}</Status>
                  )}
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                {canEdit && execution.status === 'partial' && execution.batch && (
                  <ConfirmAction
                    title={`Resume partial execution #${execution.id}?`}
                    description="Continues from the saved batch checkpoint; batches that already completed will not run again."
                    confirmLabel={
                      execution.compensation
                        ? 'Resume Compensation'
                        : 'Resume Execution'
                    }
                    tone="primary"
                    onConfirm={() => handleEnqueue(true)}
                    trigger={
                      <Button variant="dark" size="sm" loading={actionBusy}>
                        <Icon name="play" className="size-3.5" />
                        {execution.compensation
                          ? 'Resume Compensation'
                          : 'Resume Partial Execution'}
                      </Button>
                    }
                  />
                )}
                {canEdit &&
                  execution.status === 'partial' &&
                  execution.type === 'up' &&
                  execution.batch && (
                    <ConfirmAction
                      title={`Compensate partial execution #${execution.id}?`}
                      description="Creates a new rollback execution using Down SQL for committed batches only, in reverse order. It must receive the same group approvals before it can run."
                      confirmLabel="Submit Compensation for Review"
                      tone="primary"
                      onConfirm={handleCompensate}
                      trigger={
                        <Button
                          variant="outline"
                          size="sm"
                          loading={actionBusy}
                        >
                          <Icon name="undo" className="size-3.5" />
                          Compensate Committed Batches
                        </Button>
                      }
                    />
                  )}
              </div>
              {execution.error_message && (
                <div className="rounded-lg border border-danger/25 bg-danger-soft px-3 py-2 font-mono text-xs text-danger-fg break-words">
                  {execution.error_code ? `[${execution.error_code}] ` : ''}
                  {execution.error_message}
                </div>
              )}
            </div>
          )}

          <div className="space-y-5 border-t border-line pt-5">
            <div className="space-y-2">
              <span className="block text-[11px] font-bold uppercase tracking-wider text-muted">
                Immutable Execution Snapshot SQL
              </span>
              <pre className="m-0 max-h-56 overflow-auto rounded-xl border border-line bg-surface-subtle p-4 font-mono text-xs leading-6 text-ink">
                {execution.sql}
              </pre>
            </div>
            {execution.down_sql && (
              <div className="space-y-2">
                <span className="block text-[11px] font-bold uppercase tracking-wider text-success-fg">
                  Paired Down SQL
                </span>
                <pre className="m-0 max-h-44 overflow-auto rounded-xl border border-line bg-surface-subtle p-4 font-mono text-xs leading-6 text-muted">
                  {execution.down_sql}
                </pre>
              </div>
            )}
            {execution.irreversible && execution.irreversible_reason && (
              <div className="rounded-xl border border-warning/25 bg-warning-soft/40 p-4 text-xs font-medium text-warning-fg">
                Marked irreversible: {execution.irreversible_reason}
              </div>
            )}
          </div>
        </div>
      )}
    </Modal>
  )
}

export function ConsensusAirlock({
  executions,
  canEdit,
  filter,
  onFilterChange,
  focusedExecutionId,
  onMutated,
}: {
  executions: MigrationExecution[]
  canEdit: boolean
  filter: AirlockFilter
  onFilterChange: (filter: AirlockFilter) => void
  focusedExecutionId: number | null
  onMutated: (message: string) => Promise<void>
}) {
  const [selectedId, setSelectedId] = useState<number | null>(
    focusedExecutionId,
  )
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [prevFocusedId, setPrevFocusedId] = useState<number | null>(
    focusedExecutionId,
  )
  if (prevFocusedId !== focusedExecutionId) {
    setPrevFocusedId(focusedExecutionId)
    if (focusedExecutionId != null) {
      setSelectedId(focusedExecutionId)
    }
  }

  const selectedExecution =
    selectedId != null
      ? executions.find((e) => e.id === selectedId) ?? null
      : null

  const counts = {
    all: executions.length,
    pending_review: executions.filter(
      (e) => e.status === 'pending_review' || e.status === 'compensation_pending',
    ).length,
    approved: executions.filter((e) => e.status === 'approved').length,
    queue_running: executions.filter(
      (e) =>
        e.status === 'queued' ||
        e.status === 'running' ||
        e.status === 'partial' ||
        e.status === 'compensating',
    ).length,
    completed: executions.filter(
      (e) =>
        e.status === 'success' ||
        e.status === 'failed' ||
        e.status === 'rejected' ||
        e.status === 'compensated',
    ).length,
  }

  const filtered = executions.filter((e) => {
    if (filter === 'all') return true
    if (filter === 'pending_review') {
      return e.status === 'pending_review' || e.status === 'compensation_pending'
    }
    if (filter === 'approved') return e.status === 'approved'
    if (filter === 'queue_running') {
      return (
        e.status === 'queued' ||
        e.status === 'running' ||
        e.status === 'partial' ||
        e.status === 'compensating'
      )
    }
    return (
      e.status === 'success' ||
      e.status === 'failed' ||
      e.status === 'rejected' ||
      e.status === 'compensated'
    )
  })

  const filterOptions: Array<{
    key: AirlockFilter
    label: string
    count: number
  }> = [
    { key: 'all', label: 'All', count: counts.all },
    {
      key: 'pending_review',
      label: 'Awaiting Sign-off',
      count: counts.pending_review,
    },
    { key: 'approved', label: 'Ready to Queue', count: counts.approved },
    {
      key: 'queue_running',
      label: 'In Progress',
      count: counts.queue_running,
    },
    { key: 'completed', label: 'Completed', count: counts.completed },
  ]

  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize))
  const safePage = Math.min(page, totalPages)
  const paginatedExecutions = filtered.slice(
    (safePage - 1) * pageSize,
    safePage * pageSize,
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-line/80 bg-white px-5 py-4 sm:px-6 sm:py-5 shadow-2xs">
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="m-0 text-base font-bold text-ink">
            Migration Execution
          </h2>
          <span className="rounded-full bg-violet-soft border border-violet/20 px-3 py-1 text-xs font-bold text-violet-fg whitespace-nowrap">
            {counts.pending_review > 0
              ? `${counts.pending_review} awaiting sign-off`
              : 'All settled'}
          </span>
        </div>
      </div>

      <div
        className="flex items-center gap-2 overflow-x-auto py-0.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden sm:flex-wrap sm:gap-2.5 sm:overflow-visible"
        role="group"
        aria-label="Filter executions by status"
      >
        {filterOptions.map((opt) => {
          const active = filter === opt.key
          return (
            <button
              key={opt.key}
              type="button"
              onClick={() => onFilterChange(opt.key)}
              className={cn(
                'inline-flex shrink-0 items-center gap-2 whitespace-nowrap rounded-xl border px-3.5 py-2 text-xs font-semibold transition-colors cursor-pointer',
                active
                  ? 'border-ink bg-ink text-white shadow-xs'
                  : 'border-line/90 bg-white text-muted hover:border-line-strong hover:text-ink shadow-2xs',
              )}
            >
              <span>{opt.label}</span>
              <span
                className={cn(
                  'inline-flex min-w-5 items-center justify-center rounded-full px-1.5 py-0.5 font-mono text-[11px] font-bold tabular-nums',
                  active
                    ? 'bg-white/20 text-white'
                    : 'bg-canvas text-muted',
                )}
              >
                {opt.count}
              </span>
            </button>
          )
        })}
      </div>

      {filtered.length === 0 ? (
        <div className="rounded-2xl border border-line bg-white py-12 shadow-2xs">
          <EmptyState icon="shield" title="No executions in this stage">
            {filter === 'all'
              ? 'Submit a migration draft, rollback, or forward to start a Migration Execution.'
              : 'No executions currently match this filter.'}
          </EmptyState>
        </div>
      ) : (
        <section className="overflow-hidden rounded-2xl border border-line/80 bg-white shadow-2xs">
          <div className="w-full overflow-x-auto overscroll-x-contain touch-pan-x [scrollbar-width:thin]">
            <table className="w-full min-w-[680px] border-collapse text-left text-xs">
              <thead>
                <tr className="border-b border-line bg-surface-subtle text-[11px] font-bold uppercase tracking-wider text-muted">
                  <th className="px-5 py-3.5 whitespace-nowrap">Exec</th>
                  <th className="px-5 py-3.5">Migration</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Type</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Status</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Submitted</th>
                  <th className="px-5 py-3.5 text-right whitespace-nowrap">
                    Details
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {paginatedExecutions.map((exec) => {
                  const meta = statusMeta(exec.status, exec.type)
                  return (
                    <tr
                      key={exec.id}
                      className={cn(
                        'transition-colors hover:bg-canvas/60',
                        focusedExecutionId === exec.id && 'bg-violet-soft/20',
                      )}
                    >
                      <td className="px-5 py-4 whitespace-nowrap font-mono font-bold text-violet-fg">
                        #{exec.id}
                      </td>
                      <td className="px-5 py-4 max-w-[280px]">
                        <div className="truncate font-bold text-ink">
                          {exec.name}
                        </div>
                        <div className="mt-0.5 text-[11px] text-muted">
                          Draft #{exec.draft_id}
                          {exec.version != null ? ` · v${exec.version}` : ''}
                        </div>
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap">
                        <span
                          className={cn(
                            'font-semibold',
                            exec.type === 'rollback'
                              ? 'text-warning-fg'
                              : exec.type === 'forward'
                                ? 'text-violet-fg'
                                : 'text-ink',
                          )}
                        >
                          {exec.compensation
                            ? 'Compensation'
                            : typeLabel(exec.type)}
                        </span>
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap">
                        <Status tone={meta.tone}>{meta.label}</Status>
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap text-muted">
                        <div>{new Date(exec.created_at).toLocaleString()}</div>
                        <div className="mt-0.5 text-[11px]">
                          User #{exec.executed_by}
                        </div>
                      </td>
                      <td className="px-5 py-4 text-right whitespace-nowrap">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setSelectedId(exec.id)}
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
            totalItems={filtered.length}
            onPageChange={setPage}
            onPageSizeChange={setPageSize}
          />
        </section>
      )}

      <ExecutionDetailModal
        execution={selectedExecution}
        canEdit={canEdit}
        onClose={() => setSelectedId(null)}
        onMutated={onMutated}
      />
    </div>
  )
}
