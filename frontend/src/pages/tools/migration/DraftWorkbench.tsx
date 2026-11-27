import { useId, useState, type FormEvent } from 'react'
import { AnimatePresence, LayoutGroup, m, useReducedMotion } from 'motion/react'
import {
  api,
  type ExplainResponse,
  type MigrationBatchDefinition,
  type MigrationDraft,
  type MigrationDraftInput,
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

function draftStatusTone(status: MigrationDraft['status']) {
  if (status === 'applied' || status === 'approved') return 'green' as const
  if (status === 'failed') return 'red' as const
  if (status === 'pending_review') return 'amber' as const
  if (status === 'queued' || status === 'running') return 'violet' as const
  return 'gray' as const
}

function DraftDetailModal({
  draft,
  canEdit,
  databaseName,
  onClose,
  onEdit,
  onSubmit,
  onTrack,
}: {
  draft: MigrationDraft | null
  canEdit: boolean
  databaseName: string
  onClose: () => void
  onEdit: (draft: MigrationDraft) => void
  onSubmit: (draft: MigrationDraft) => Promise<void>
  onTrack: (draftId: number) => void
}) {
  if (!draft) return null
  const isEditable = draft.status === 'draft' || draft.status === 'failed'
  const inFlight =
    draft.status === 'pending_review' ||
    draft.status === 'approved' ||
    draft.status === 'queued' ||
    draft.status === 'running'

  return (
    <Modal
      open={Boolean(draft)}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={draft.name}
      description={`Draft #${draft.id} · ${databaseName}`}
      size="lg"
    >
      <div className="space-y-6">
        <div className="space-y-5">
          <div className="space-y-2">
            <span className="block text-[11px] font-bold uppercase tracking-wider text-muted">
              Forward Schema Change (Up SQL)
            </span>
            <pre className="m-0 max-h-60 overflow-auto rounded-xl border border-line bg-surface-subtle p-4 font-mono text-xs leading-6 text-ink">
              {draft.sql}
            </pre>
          </div>
          {draft.down_sql && !draft.irreversible && (
            <div className="space-y-2">
              <span className="block text-[11px] font-bold uppercase tracking-wider text-success-fg">
                Paired Down SQL (Rollback)
              </span>
              <pre className="m-0 max-h-44 overflow-auto rounded-xl border border-line bg-surface-subtle p-4 font-mono text-xs leading-6 text-muted">
                {draft.down_sql}
              </pre>
            </div>
          )}
          {draft.irreversible && draft.irreversible_reason && (
            <div className="rounded-xl border border-warning/25 bg-warning-soft/40 p-4 text-xs font-medium text-warning-fg">
              Marked irreversible: {draft.irreversible_reason}
            </div>
          )}
        </div>

        <div className="flex flex-wrap items-center justify-end gap-2.5 border-t border-line/80 pt-5">
          {canEdit && isEditable && (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  onClose()
                  onEdit(draft)
                }}
              >
                <Icon name="edit" className="size-3.5" />
                Edit Draft
              </Button>
              <ConfirmAction
                title={`Submit "${draft.name}" to Migration Execution?`}
                description={`This snapshots Draft #${draft.id} into an immutable execution in Pending Review. Every group with access to "${databaseName}" must sign off via their Keyholder before an Editor can release it to the queue.`}
                confirmLabel="Submit for Review"
                tone="primary"
                onConfirm={async () => {
                  await onSubmit(draft)
                  onClose()
                }}
                trigger={
                  <Button variant="dark" size="sm">
                    <Icon name="send" className="size-3.5" />
                    Submit to Migration Execution
                  </Button>
                }
              />
            </>
          )}

          {inFlight && (
            <Button
              variant="dark"
              size="sm"
              onClick={() => {
                onClose()
                onTrack(draft.id)
              }}
            >
              <Icon name="shield" className="size-3.5" />
              Track in Migration Execution
            </Button>
          )}
        </div>
      </div>
    </Modal>
  )
}


export type InitialDraftSeed = {
  name: string
  sql: string
  downSql: string
  irreversibleReason?: string
  batch?: MigrationBatchDefinition
}

export function DraftWorkbench({
  databaseId,
  databaseName,
  canEdit,
  drafts,
  initialSeed,
  showComposer,
  onToggleComposer,
  onClearSeed,
  onSaved,
  onSubmitted,
  onTrackExecution,
}: {
  databaseId: number
  databaseName: string
  canEdit: boolean
  drafts: MigrationDraft[]
  initialSeed: InitialDraftSeed | null
  showComposer: boolean
  onToggleComposer: (open: boolean) => void
  onClearSeed: () => void
  onSaved: (message: string) => Promise<void>
  onSubmitted: (executionId: number) => Promise<void>
  onTrackExecution: (draftId: number) => void
}) {
  const strategyId = useId()
  const reduceMotion = useReducedMotion()

  const [editingDraft, setEditingDraft] = useState<MigrationDraft | null>(null)
  const [selectedDraft, setSelectedDraft] = useState<MigrationDraft | null>(
    null,
  )
  const [name, setName] = useState(initialSeed?.name ?? '')
  const [sql, setSql] = useState(initialSeed?.sql ?? '')
  const [downSql, setDownSql] = useState(initialSeed?.downSql ?? '')
  const [batchEnabled, setBatchEnabled] = useState(Boolean(initialSeed?.batch))
  const [batchSchema, setBatchSchema] = useState(initialSeed?.batch?.schema ?? 'public')
  const [batchTable, setBatchTable] = useState(initialSeed?.batch?.table ?? '')
  const [batchKeyColumn, setBatchKeyColumn] = useState(initialSeed?.batch?.key_column ?? 'id')
  const [batchScopeSQL, setBatchScopeSQL] = useState(initialSeed?.batch?.scope_sql ?? '')
  const [batchSize, setBatchSize] = useState(String(initialSeed?.batch?.batch_size ?? 500))
  const [irreversible, setIrreversible] = useState(
    Boolean(initialSeed?.irreversibleReason),
  )
  const [irreversibleReason, setIrreversibleReason] = useState(
    initialSeed?.irreversibleReason ?? '',
  )
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState('')
  const [explainBusy, setExplainBusy] = useState(false)
  const [explainResult, setExplainResult] = useState<ExplainResponse | null>(
    null,
  )
  const [explainError, setExplainError] = useState('')

  function openNewComposer() {
    setEditingDraft(null)
    setName(initialSeed?.name ?? '')
    setSql(initialSeed?.sql ?? '')
    setDownSql(initialSeed?.downSql ?? '')
    setBatchEnabled(Boolean(initialSeed?.batch))
    setBatchSchema(initialSeed?.batch?.schema ?? 'public')
    setBatchTable(initialSeed?.batch?.table ?? '')
    setBatchKeyColumn(initialSeed?.batch?.key_column ?? 'id')
    setBatchScopeSQL(initialSeed?.batch?.scope_sql ?? '')
    setBatchSize(String(initialSeed?.batch?.batch_size ?? 500))
    setIrreversible(false)
    setIrreversibleReason('')
    setFormError('')
    setExplainResult(null)
    setExplainError('')
    onToggleComposer(true)
  }

  function startEditingDraft(draft: MigrationDraft) {
    setEditingDraft(draft)
    setName(draft.name)
    setSql(draft.sql)
    setDownSql(draft.down_sql ?? '')
    setBatchEnabled(Boolean(draft.batch))
    setBatchSchema(draft.batch?.schema ?? 'public')
    setBatchTable(draft.batch?.table ?? '')
    setBatchKeyColumn(draft.batch?.key_column ?? 'id')
    setBatchScopeSQL(draft.batch?.scope_sql ?? '')
    setBatchSize(String(draft.batch?.batch_size ?? 500))
    setIrreversible(Boolean(draft.irreversible))
    setIrreversibleReason(draft.irreversible_reason ?? '')
    setFormError('')
    setExplainResult(null)
    setExplainError('')
    onToggleComposer(true)
  }

  function closeComposer() {
    setEditingDraft(null)
    setFormError('')
    setExplainResult(null)
    setExplainError('')
    onClearSeed()
    onToggleComposer(false)
  }

  async function runExplain() {
    if (!sql.trim()) return
    setExplainBusy(true)
    setExplainError('')
    setExplainResult(null)
    try {
      const res = await api.explain(databaseId, sql)
      setExplainResult(res)
    } catch (err) {
      setExplainError(
        err instanceof Error ? err.message : 'Unable to run EXPLAIN check',
      )
    } finally {
      setExplainBusy(false)
    }
  }

  async function handleSubmitForm(e: FormEvent) {
    e.preventDefault()
    setFormError('')

    if (!irreversible && !downSql.trim()) {
      setFormError(
        'Provide Down SQL for safe rollbacks, or switch to Irreversible and explain why.',
      )
      return
    }
    if (irreversible && !irreversibleReason.trim()) {
      setFormError('Irreversible migrations require a clear justification.')
      return
    }
    if (batchEnabled && irreversible) {
      setFormError('Batch migrations need Down SQL so committed batches can be rolled back.')
      return
    }
    if (batchEnabled && (!batchSchema.trim() || !batchTable.trim() || !batchKeyColumn.trim())) {
      setFormError('Batch migrations require a schema, table, and key column.')
      return
    }
    const parsedBatchSize = Number(batchSize)
    if (batchEnabled && (!Number.isInteger(parsedBatchSize) || parsedBatchSize < 1 || parsedBatchSize > 10000)) {
      setFormError('Batch size must be an integer between 1 and 10000.')
      return
    }

    setSaving(true)
    try {
      const payload: MigrationDraftInput = {
        name: name.trim(),
        sql,
        irreversible,
        ...(batchEnabled
          ? {
              batch: {
                schema: batchSchema.trim(),
                table: batchTable.trim(),
                key_column: batchKeyColumn.trim(),
                scope_sql: batchScopeSQL.trim(),
                batch_size: parsedBatchSize,
              },
            }
          : {}),
        ...(irreversible
          ? { irreversible_reason: irreversibleReason.trim(), down_sql: '' }
          : { down_sql: downSql }),
      }

      if (editingDraft) {
        await api.updateDraft(editingDraft.id, payload)
        await onSaved(`Updated migration draft "${payload.name}".`)
      } else {
        await api.createDraft(databaseId, payload)
        await onSaved(`Saved migration draft "${payload.name}" in Workbench.`)
      }
      closeComposer()
    } catch (err) {
      setFormError(
        err instanceof Error ? err.message : 'Unable to save migration draft',
      )
    } finally {
      setSaving(false)
    }
  }

  async function submitDraftToAirlock(draft: MigrationDraft) {
    setFormError('')
    const execution = await api.executeDraft(draft.id)
    await onSubmitted(execution.id)
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-line/80 bg-white p-5 sm:px-6 sm:py-5 shadow-2xs">
        <div className="flex items-center gap-3">
          <h2 className="m-0 text-base font-bold text-ink">Schema Workbench</h2>
          <span className="rounded-full bg-violet-soft border border-violet/20 px-2.5 py-0.5 text-xs font-bold text-violet-fg">
            {drafts.length} {drafts.length === 1 ? 'draft' : 'drafts'}
          </span>
        </div>
        {canEdit ? (
          <Button
            variant="dark"
            size="sm"
            onClick={() => openNewComposer()}
          >
            <Icon name="plus" className="size-3.5" />
            New Draft
          </Button>
        ) : (
          <Status tone="accent" className="px-3 py-1">
            Viewer · Read-only
          </Status>
        )}
      </div>

      <Modal
        open={showComposer && canEdit}
        onOpenChange={(open) => {
          if (!open) closeComposer()
        }}
        title={
          editingDraft
            ? `Edit Draft #${editingDraft.id}`
            : 'New Schema Change Draft'
        }
        description={`Target database: ${databaseName}`}
        size="xl"
      >
        <form onSubmit={handleSubmitForm} className="space-y-6">
          {editingDraft?.status === 'failed' && (
            <div className="flex items-center justify-between gap-2 rounded-xl border border-warning/30 bg-warning-soft/30 px-3.5 py-2 text-xs font-semibold text-warning-fg">
              <span>Reopening failed draft for revision</span>
            </div>
          )}

          {formError && (
            <ErrorNotice
              message={formError}
              onDismiss={() => setFormError('')}
            />
          )}

          <label className="block">
            <span className="mb-2 block text-sm font-bold text-ink">
              Migration Name
            </span>
            <input
              type="text"
              required
              placeholder="e.g. 003_add_orders_status_index"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="h-11 w-full rounded-xl border border-line-strong bg-white px-3.5 font-mono text-base sm:text-xs text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25"
            />
          </label>

          <div>
            <div className="mb-2.5 flex flex-wrap items-center justify-between gap-2">
              <span className="text-sm font-bold text-ink">
                Up SQL (Forward Schema Change)
              </span>
              <Button
                variant="outline"
                size="sm"
                loading={explainBusy}
                disabled={!sql.trim()}
                onClick={runExplain}
              >
                <Icon name="code" className="size-3.5" />
                Dry-run with EXPLAIN
              </Button>
            </div>
            <textarea
              required
              rows={6}
              spellCheck={false}
              placeholder="ALTER TABLE public.orders ADD COLUMN status text NOT NULL DEFAULT 'pending';"
              value={sql}
              onChange={(e) => setSql(e.target.value)}
              className="w-full rounded-xl border border-line-strong bg-white p-4 font-mono text-base sm:text-xs leading-6 text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25 resize-y"
            />
          </div>

          <section className="rounded-xl border border-line bg-[#fbfafd] p-4 space-y-3">
            <label className="flex items-center gap-2 text-xs font-bold text-ink">
              <input
                type="checkbox"
                checked={batchEnabled}
                onChange={(e) => setBatchEnabled(e.target.checked)}
                className="size-4 accent-violet"
              />
              Run as a batch migration
            </label>
            {batchEnabled && (
              <div className="space-y-3">
                <p className="m-0 text-[11px] leading-5 text-muted">
                  The platform snapshots matching keys before applying the first batch. The key must be a NOT NULL bigint primary key or single-column unique key.
                </p>
                <div className="grid gap-3 sm:grid-cols-2">
                  <label className="block">
                    <span className="mb-1 block text-[11px] font-semibold text-ink">Schema</span>
                    <input value={batchSchema} onChange={(e) => setBatchSchema(e.target.value)} required className="h-9 w-full rounded-[10px] border border-line-strong bg-white px-3 font-mono text-xs outline-none focus:border-violet" />
                  </label>
                  <label className="block">
                    <span className="mb-1 block text-[11px] font-semibold text-ink">Table</span>
                    <input value={batchTable} onChange={(e) => setBatchTable(e.target.value)} required className="h-9 w-full rounded-[10px] border border-line-strong bg-white px-3 font-mono text-xs outline-none focus:border-violet" />
                  </label>
                  <label className="block">
                    <span className="mb-1 block text-[11px] font-semibold text-ink">Key column</span>
                    <input value={batchKeyColumn} onChange={(e) => setBatchKeyColumn(e.target.value)} required className="h-9 w-full rounded-[10px] border border-line-strong bg-white px-3 font-mono text-xs outline-none focus:border-violet" />
                  </label>
                  <label className="block">
                    <span className="mb-1 block text-[11px] font-semibold text-ink">Rows per batch</span>
                    <input type="number" min={1} max={10000} value={batchSize} onChange={(e) => setBatchSize(e.target.value)} required className="h-9 w-full rounded-[10px] border border-line-strong bg-white px-3 font-mono text-xs outline-none focus:border-violet" />
                  </label>
                </div>
                <label className="block">
                  <span className="mb-1 block text-[11px] font-semibold text-ink">Scope WHERE predicate (optional)</span>
                  <textarea
                    rows={2}
                    spellCheck={false}
                    placeholder="status = 'pending'"
                    value={batchScopeSQL}
                    onChange={(e) => setBatchScopeSQL(e.target.value)}
                    className="w-full rounded-[10px] border border-line-strong bg-white p-3 font-mono text-xs leading-5 text-ink outline-none focus:border-violet resize-y"
                  />
                  <span className="mt-1 block text-[10px] text-muted">Leave blank to snapshot every key in the table.</span>
                </label>
              </div>
            )}
          </section>

          {explainError && (
            <ErrorNotice
              message={explainError}
              onDismiss={() => setExplainError('')}
            />
          )}

          {explainResult && (
            <div className="rounded-xl border border-success/30 bg-success-soft/35 p-3.5 text-xs">
              <div className="flex items-center justify-between gap-2 mb-2">
                <span className="inline-flex items-center gap-1.5 font-bold text-success-fg">
                  <Icon name="check" className="size-4" />
                  EXPLAIN check passed ({explainResult.statements.length}{' '}
                  statement{explainResult.statements.length === 1 ? '' : 's'})
                </span>
                <button
                  type="button"
                  onClick={() => setExplainResult(null)}
                  className="text-xs font-semibold text-muted hover:text-ink"
                >
                  Dismiss
                </button>
              </div>
              <pre className="m-0 max-h-36 overflow-auto rounded-lg border border-line bg-white p-2.5 font-mono text-xs text-ink">
                {JSON.stringify(explainResult.statements, null, 2)}
              </pre>
            </div>
          )}

          <div className="rounded-2xl border border-line/90 bg-surface-subtle p-4 sm:p-6 space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span className="text-sm font-bold text-ink">
                Rollback Strategy
              </span>

              <LayoutGroup id={strategyId}>
                <div className="flex w-full flex-wrap rounded-xl border border-line bg-white p-1 shadow-2xs sm:inline-flex sm:w-auto">
                  <button
                    type="button"
                    onClick={() => setIrreversible(false)}
                    className={cn(
                      'relative flex-1 justify-center rounded-lg px-2.5 py-1.5 text-xs font-bold transition-colors cursor-pointer sm:flex-initial sm:px-3.5 inline-flex items-center',
                      !irreversible ? 'text-violet-fg' : 'text-muted hover:text-ink',
                    )}
                  >
                    {!irreversible && (
                      <m.span
                        layoutId="rollback-strategy-pill"
                        className="absolute inset-0 rounded-lg bg-violet-soft"
                        transition={
                          reduceMotion
                            ? { duration: 0 }
                            : { duration: 0.16, ease: 'easeInOut' }
                        }
                      />
                    )}
                    <span className="relative z-1 inline-flex items-center gap-1.5">
                      <Icon name="undo" className="size-3.5" />
                      Reversible (Down SQL)
                    </span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setIrreversible(true)}
                    className={cn(
                      'relative flex-1 justify-center rounded-lg px-2.5 py-1.5 text-xs font-bold transition-colors cursor-pointer sm:flex-initial sm:px-3.5 inline-flex items-center',
                      irreversible ? 'text-warning-fg' : 'text-muted hover:text-ink',
                    )}
                  >
                    {irreversible && (
                      <m.span
                        layoutId="rollback-strategy-pill"
                        className="absolute inset-0 rounded-lg bg-warning-soft"
                        transition={
                          reduceMotion
                            ? { duration: 0 }
                            : { duration: 0.16, ease: 'easeInOut' }
                        }
                      />
                    )}
                    <span className="relative z-1 inline-flex items-center gap-1.5">
                      <Icon name="alert" className="size-3.5" />
                      Irreversible
                    </span>
                  </button>
                </div>
              </LayoutGroup>
            </div>

            <div className="min-h-[142px]">
              <AnimatePresence mode="wait" initial={false}>
                {!irreversible ? (
                  <m.div
                    key="reversible-block"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.14 }}
                  >
                    <label className="block">
                      <span className="mb-2 block text-xs font-bold text-ink">
                        Down SQL (Executed if rolled back later)
                      </span>
                      <textarea
                        rows={4}
                        spellCheck={false}
                        placeholder="ALTER TABLE public.orders DROP COLUMN status;"
                        value={downSql}
                        onChange={(e) => setDownSql(e.target.value)}
                        className="w-full rounded-xl border border-line-strong bg-white p-3.5 font-mono text-base sm:text-xs leading-5 text-ink outline-none focus:border-violet focus:ring-2 focus:ring-violet/25 resize-none"
                      />
                    </label>
                  </m.div>
                ) : (
                  <m.div
                    key="irreversible-block"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.14 }}
                  >
                    <label className="block">
                      <span className="mb-2 block text-xs font-bold text-warning-fg">
                        Irreversible Justification (Required)
                      </span>
                      <textarea
                        rows={4}
                        spellCheck={false}
                        placeholder="e.g. Drops deprecated legacy column; data cannot be reconstructed without manual backup."
                        value={irreversibleReason}
                        onChange={(e) => setIrreversibleReason(e.target.value)}
                        className="w-full rounded-xl border border-warning/40 bg-white p-3.5 text-base sm:text-xs leading-5 text-ink outline-none focus:border-warning focus:ring-2 focus:ring-warning/25 resize-none"
                      />
                    </label>
                  </m.div>
                )}
              </AnimatePresence>
            </div>
          </div>

          <div className="flex flex-wrap justify-end gap-3 pt-5 border-t border-line/80">
            <Button variant="outline" onClick={closeComposer}>
              Cancel
            </Button>
            <Button variant="dark" type="submit" loading={saving}>
              <Icon name="save" className="size-4" />
              {editingDraft ? 'Update draft' : 'Save draft'}
            </Button>
          </div>
        </form>
      </Modal>

      {drafts.length === 0 ? (
        <div className="rounded-2xl border border-line bg-white py-12 shadow-2xs">
          <EmptyState
            icon="migration"
            title="No migration drafts yet"
            action={
              canEdit ? (
                <Button variant="dark" size="sm" onClick={openNewComposer}>
                  <Icon name="plus" className="size-3.5" />
                  Compose first draft
                </Button>
              ) : undefined
            }
          >
            Drafts let Editors stage DDL changes, verify them with EXPLAIN,
            and define Down SQL before submitting for team leader approval.
          </EmptyState>
        </div>
      ) : (
        <section className="overflow-hidden rounded-2xl border border-line/80 bg-white shadow-2xs">
          <div className="w-full overflow-x-auto overscroll-x-contain touch-pan-x [scrollbar-width:thin]">
            <table className="w-full min-w-[640px] border-collapse text-left text-xs">
              <thead>
                <tr className="border-b border-line bg-surface-subtle text-[11px] font-bold uppercase tracking-wider text-muted">
                  <th className="px-5 py-3.5 whitespace-nowrap">Draft</th>
                  <th className="px-5 py-3.5">Name</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Status</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Rollback</th>
                  <th className="px-5 py-3.5 whitespace-nowrap">Updated</th>
                  <th className="px-5 py-3.5 text-right whitespace-nowrap">
                    Details
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {drafts.map((draft) => {
                  const hasDownSql = Boolean(
                    draft.down_sql && !draft.irreversible,
                  )
                  return (
                    <tr
                      key={draft.id}
                      className="transition-colors hover:bg-canvas/60"
                    >
                      <td className="px-5 py-4 whitespace-nowrap font-mono font-bold text-violet-fg">
                        #{draft.id}
                      </td>
                      <td className="px-5 py-4 max-w-[280px]">
                        <div className="truncate font-bold text-ink">
                          {draft.name}
                        </div>
                        <div className="mt-0.5 text-[11px] text-muted">
                          User #{draft.created_by}
                          {draft.applied_version != null
                            ? ` · v${draft.applied_version}`
                            : ''}
                        </div>
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap">
                        <Status tone={draftStatusTone(draft.status)}>
                          {draft.status.replace('_', ' ')}
                        </Status>
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap">
                        {hasDownSql ? (
                          <span className="inline-flex items-center gap-1 font-semibold text-success-fg">
                            <Icon name="undo" className="size-3" />
                            Reversible
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 font-semibold text-warning-fg">
                            <Icon name="alert" className="size-3" />
                            Irreversible
                          </span>
                        )}
                      </td>
                      <td className="px-5 py-4 whitespace-nowrap text-muted">
                        {new Date(draft.updated_at).toLocaleString()}
                      </td>
                      <td className="px-5 py-4 text-right whitespace-nowrap">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setSelectedDraft(draft)}
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
        </section>
      )}

      <DraftDetailModal
        draft={selectedDraft}
        canEdit={canEdit}
        databaseName={databaseName}
        onClose={() => setSelectedDraft(null)}
        onEdit={startEditingDraft}
        onSubmit={submitDraftToAirlock}
        onTrack={onTrackExecution}
      />
    </div>
  )
}
