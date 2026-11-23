import { useId, useState, type FormEvent } from 'react'
import { LayoutGroup, m, useReducedMotion } from 'motion/react'
import {
  api,
  type ExplainResponse,
  type MigrationDraft,
  type MigrationDraftInput,
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

function draftStatusTone(status: MigrationDraft['status']) {
  if (status === 'applied' || status === 'approved') return 'green' as const
  if (status === 'failed') return 'red' as const
  if (status === 'pending_review') return 'amber' as const
  if (status === 'queued' || status === 'running') return 'violet' as const
  return 'gray' as const
}

function DraftCard({
  draft,
  canEdit,
  databaseName,
  onEdit,
  onSubmit,
  onTrack,
}: {
  draft: MigrationDraft
  canEdit: boolean
  databaseName: string
  onEdit: (draft: MigrationDraft) => void
  onSubmit: (draft: MigrationDraft) => Promise<void>
  onTrack: (draftId: number) => void
}) {
  const [showSql, setShowSql] = useState(false)
  const isEditable = draft.status === 'draft' || draft.status === 'failed'
  const inFlight =
    draft.status === 'pending_review' ||
    draft.status === 'approved' ||
    draft.status === 'queued' ||
    draft.status === 'running'
  const hasDownSql = Boolean(draft.down_sql && !draft.irreversible)

  return (
    <article className="overflow-hidden rounded-[18px] border border-line bg-white shadow-2xs transition-colors hover:border-line-strong">
      <div className="flex flex-wrap items-center justify-between gap-3 p-4.5">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="rounded-md border border-line bg-canvas px-2 py-0.5 font-mono text-[11px] font-bold text-violet">
              #{draft.id}
            </span>
            <h3 className="m-0 truncate text-sm font-bold text-ink">
              {draft.name}
            </h3>
            <Status tone={draftStatusTone(draft.status)}>
              {draft.status.replace('_', ' ')}
            </Status>
            {hasDownSql ? (
              <Status tone="green" className="gap-1">
                <Icon name="undo" className="size-3" />
                Reversible
              </Status>
            ) : (
              <Status tone="amber" className="gap-1">
                <Icon name="alert" className="size-3" />
                Irreversible
              </Status>
            )}
            {draft.applied_version != null && (
              <Status tone="violet">v{draft.applied_version}</Status>
            )}
          </div>
          <p className="mt-1 mb-0 text-[11px] text-muted">
            Created by user #{draft.created_by} · Updated{' '}
            {new Date(draft.updated_at).toLocaleString()}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2 shrink-0">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowSql((prev) => !prev)}
          >
            <Icon name="code" className="size-3.5" />
            {showSql ? 'Hide SQL' : 'Inspect SQL'}
          </Button>

          {canEdit && isEditable && (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => onEdit(draft)}
              >
                <Icon name="edit" className="size-3.5" />
                Edit
              </Button>
              <ConfirmAction
                title={`Submit "${draft.name}" to Consensus Airlock?`}
                description={`This snapshots Draft #${draft.id} into an immutable execution in Pending Review. Every group with access to "${databaseName}" must sign off via their Keyholder before an Editor can release it to the queue.`}
                confirmLabel="Submit for Review"
                tone="primary"
                onConfirm={() => onSubmit(draft)}
                trigger={
                  <Button variant="dark" size="sm">
                    <Icon name="send" className="size-3.5" />
                    Submit to Airlock
                  </Button>
                }
              />
            </>
          )}

          {inFlight && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => onTrack(draft.id)}
            >
              <Icon name="shield" className="size-3.5" />
              Track in Airlock
            </Button>
          )}
        </div>
      </div>

      {showSql && (
        <div className="border-t border-line bg-[#fbfafd] px-4.5 py-3.5 space-y-2.5">
          <div>
            <span className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-muted">
              Forward Schema Change (Up SQL)
            </span>
            <pre className="m-0 max-h-48 overflow-auto rounded-lg border border-line bg-white p-3 font-mono text-xs leading-relaxed text-ink">
              {draft.sql}
            </pre>
          </div>
          {draft.down_sql && !draft.irreversible && (
            <div>
              <span className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-success">
                Paired Down SQL (Rollback)
              </span>
              <pre className="m-0 max-h-32 overflow-auto rounded-lg border border-line bg-white p-3 font-mono text-xs leading-relaxed text-muted">
                {draft.down_sql}
              </pre>
            </div>
          )}
          {draft.irreversible && draft.irreversible_reason && (
            <p className="m-0 text-xs text-warning font-medium">
              Marked irreversible: {draft.irreversible_reason}
            </p>
          )}
        </div>
      )}
    </article>
  )
}


export type InitialDraftSeed = {
  name: string
  sql: string
  downSql: string
  irreversibleReason?: string
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
  const [name, setName] = useState(initialSeed?.name ?? '')
  const [sql, setSql] = useState(initialSeed?.sql ?? '')
  const [downSql, setDownSql] = useState(initialSeed?.downSql ?? '')
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

    setSaving(true)
    try {
      const payload: MigrationDraftInput = {
        name: name.trim(),
        sql,
        irreversible,
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
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-[18px] border border-line/80 bg-white p-4.5 shadow-2xs">
        <div className="flex items-center gap-3">
          <h2 className="m-0 text-base font-bold text-ink">Schema Workbench</h2>
          <span className="rounded-full bg-violet-soft border border-violet/20 px-2.5 py-0.5 text-[11px] font-bold text-violet">
            {drafts.length} {drafts.length === 1 ? 'draft' : 'drafts'}
          </span>
        </div>
        {canEdit ? (
          <Button
            variant="dark"
            size="sm"
            onClick={() =>
              showComposer ? closeComposer() : openNewComposer()
            }
          >
            <Icon name={showComposer ? 'close' : 'plus'} className="size-3.5" />
            {showComposer ? 'Close Composer' : 'New Draft'}
          </Button>
        ) : (
          <Status tone="accent" className="px-3 py-1">
            Viewer · Read-only
          </Status>
        )}
      </div>

      {showComposer && canEdit && (
        <form
          onSubmit={handleSubmitForm}
          className="rounded-[18px] border border-violet/30 bg-white p-4.5 shadow-2xs space-y-4"
        >
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-line pb-3">
            <div>
              <h3 className="m-0 text-base font-bold text-ink">
                {editingDraft
                  ? `Edit Draft #${editingDraft.id}`
                  : 'New Schema Change Draft'}
              </h3>
              <p className="mt-0.5 mb-0 text-xs text-muted">
                Target database: <strong className="text-ink">{databaseName}</strong>
              </p>
            </div>
            {editingDraft?.status === 'failed' && (
              <Status tone="red">Reopening failed draft for revision</Status>
            )}
          </div>

          {formError && (
            <ErrorNotice
              message={formError}
              onDismiss={() => setFormError('')}
            />
          )}

          <label className="block">
            <span className="mb-1.5 block text-xs font-bold text-ink">
              Migration Name
            </span>
            <input
              type="text"
              required
              placeholder="e.g. 003_add_orders_status_index"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="h-10 w-full rounded-[10px] border border-line-strong bg-white px-3 font-mono text-xs outline-none focus:border-violet"
            />
          </label>

          <div>
            <div className="mb-1.5 flex flex-wrap items-center justify-between gap-2">
              <span className="text-xs font-bold text-ink">
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
              className="w-full rounded-[10px] border border-line-strong bg-white p-3.5 font-mono text-xs leading-5 text-ink outline-none focus:border-violet resize-y"
            />
          </div>

          {explainError && (
            <ErrorNotice
              message={explainError}
              onDismiss={() => setExplainError('')}
            />
          )}

          {explainResult && (
            <div className="rounded-xl border border-success/30 bg-success-soft/35 p-3.5 text-xs">
              <div className="flex items-center justify-between gap-2 mb-2">
                <span className="inline-flex items-center gap-1.5 font-bold text-success">
                  <Icon name="check" className="size-4" />
                  EXPLAIN check passed ({explainResult.statements.length}{' '}
                  statement{explainResult.statements.length === 1 ? '' : 's'})
                </span>
                <button
                  type="button"
                  onClick={() => setExplainResult(null)}
                  className="text-[11px] font-semibold text-muted hover:text-ink"
                >
                  Dismiss
                </button>
              </div>
              <pre className="m-0 max-h-36 overflow-auto rounded-lg border border-line bg-white p-2.5 font-mono text-[11px] text-ink">
                {JSON.stringify(explainResult.statements, null, 2)}
              </pre>
            </div>
          )}

          <div className="rounded-xl border border-line bg-[#fbfafd] p-4 space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="text-xs font-bold text-ink">
                Rollback Strategy
              </span>

              <LayoutGroup id={strategyId}>
                <div className="inline-flex rounded-lg border border-line bg-white p-0.5">
                  <button
                    type="button"
                    onClick={() => setIrreversible(false)}
                    className={cn(
                      'relative rounded-md px-3 py-1.5 text-xs font-bold transition-colors',
                      !irreversible ? 'text-violet' : 'text-muted hover:text-ink',
                    )}
                  >
                    {!irreversible && (
                      <m.span
                        layoutId="rollback-strategy-pill"
                        className="absolute inset-0 rounded-md bg-violet-soft"
                        transition={
                          reduceMotion
                            ? { duration: 0 }
                            : { duration: 0.16, ease: 'easeInOut' }
                        }
                      />
                    )}
                    <span className="relative z-1 inline-flex items-center gap-1">
                      <Icon name="undo" className="size-3.5" />
                      Reversible (Down SQL)
                    </span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setIrreversible(true)}
                    className={cn(
                      'relative rounded-md px-3 py-1.5 text-xs font-bold transition-colors',
                      irreversible ? 'text-warning' : 'text-muted hover:text-ink',
                    )}
                  >
                    {irreversible && (
                      <m.span
                        layoutId="rollback-strategy-pill"
                        className="absolute inset-0 rounded-md bg-warning-soft"
                        transition={
                          reduceMotion
                            ? { duration: 0 }
                            : { duration: 0.16, ease: 'easeInOut' }
                        }
                      />
                    )}
                    <span className="relative z-1 inline-flex items-center gap-1">
                      <Icon name="alert" className="size-3.5" />
                      Irreversible
                    </span>
                  </button>
                </div>
              </LayoutGroup>
            </div>

            {!irreversible ? (
              <label className="block">
                <span className="mb-1 block text-[11px] font-semibold text-ink">
                  Down SQL (Executed if rolled back later)
                </span>
                <textarea
                  rows={4}
                  spellCheck={false}
                  placeholder="ALTER TABLE public.orders DROP COLUMN status;"
                  value={downSql}
                  onChange={(e) => setDownSql(e.target.value)}
                  className="w-full rounded-[10px] border border-line-strong bg-white p-3 font-mono text-xs leading-5 text-ink outline-none focus:border-violet resize-y"
                />
              </label>
            ) : (
              <label className="block">
                <span className="mb-1 block text-[11px] font-semibold text-warning">
                  Irreversible Justification (Required)
                </span>
                <input
                  type="text"
                  placeholder="e.g. Drops deprecated legacy column; data cannot be reconstructed."
                  value={irreversibleReason}
                  onChange={(e) => setIrreversibleReason(e.target.value)}
                  className="h-10 w-full rounded-[10px] border border-warning/40 bg-white px-3 text-xs outline-none focus:border-warning"
                />
              </label>
            )}
          </div>

          <div className="flex flex-wrap justify-end gap-2 pt-1">
            <Button variant="outline" size="sm" onClick={closeComposer}>
              Cancel
            </Button>
            <Button variant="dark" size="sm" type="submit" loading={saving}>
              <Icon name="save" className="size-3.5" />
              {editingDraft ? 'Update draft' : 'Save draft'}
            </Button>
          </div>
        </form>
      )}

      <div className="space-y-3">
        {drafts.map((draft) => (
          <DraftCard
            key={draft.id}
            draft={draft}
            canEdit={canEdit}
            databaseName={databaseName}
            onEdit={startEditingDraft}
            onSubmit={submitDraftToAirlock}
            onTrack={onTrackExecution}
          />
        ))}

        {drafts.length === 0 && (
          <div className="rounded-[18px] border border-line bg-white py-12 shadow-2xs">
            <EmptyState
              icon="migration"
              title="No migration drafts yet"
              action={
                canEdit ? (
                  <Button
                    variant="dark"
                    size="sm"
                    onClick={openNewComposer}
                  >
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
        )}
      </div>
    </div>
  )
}
