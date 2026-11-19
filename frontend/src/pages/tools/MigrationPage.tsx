import { useState, type FormEvent } from 'react'
import { useParams } from 'react-router'
import { api, type MigrationDraft } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { useWorkspaceContext } from '../../components/workspaceContext'
import { Icon } from '../../components/Icon'
import {
  Button,
  ConfirmAction,
  EmptyState,
  ErrorNotice,
  Loading,
  Status,
} from '../../components/ui'
import { useResource } from '../../hooks'

export function MigrationPage() {
  const { databaseId = '0' } = useParams()
  const id = Number(databaseId)
  const { databases } = useWorkspaceContext()
  const databaseName =
    databases.find((database) => database.id === id)?.name ?? `Database #${id}`

  const history = useResource(() => api.history(id), [id])
  const drafts = useResource(() => api.drafts(id), [id])

  const [showNewDraft, setShowNewDraft] = useState(false)
  const [draftName, setDraftName] = useState('')
  const [draftSql, setDraftSql] = useState('')
  const [draftError, setDraftError] = useState('')
  const [creating, setCreating] = useState(false)
  const [actionSuccess, setActionSuccess] = useState('')

  if (history.loading || drafts.loading) {
    return <Loading page label="Loading migration history and drafts…" />
  }

  async function handleCreateDraft(e: FormEvent) {
    e.preventDefault()
    setCreating(true)
    setDraftError('')
    try {
      await api.createDraft(id, draftName, draftSql)
      setDraftName('')
      setDraftSql('')
      setShowNewDraft(false)
      setActionSuccess('Migration draft created successfully.')
      await drafts.reload()
      setTimeout(() => setActionSuccess(''), 4000)
    } catch (err) {
      setDraftError(
        err instanceof Error ? err.message : 'Unable to create migration draft',
      )
    } finally {
      setCreating(false)
    }
  }

  async function handleExecuteDraft(draft: MigrationDraft) {
    try {
      await api.executeDraft(draft.id)
      setActionSuccess(`Migration #${draft.id} executed successfully.`)
      await Promise.all([drafts.reload(), history.reload()])
      setTimeout(() => setActionSuccess(''), 4000)
    } catch (err) {
      setDraftError(
        err instanceof Error ? err.message : 'Migration execution failed',
      )
    }
  }

  return (
    <>
      <UtilityHeader
        backTo="/databases"
        path={databaseName}
        title="Migration history"
        status={`${history.data?.entries.length ?? 0} applied · ${drafts.data?.length ?? 0} drafts`}
        actions={
          <Button
            variant="dark"
            size="sm"
            onClick={() => setShowNewDraft((prev) => !prev)}
          >
            <Icon name="plus" className="size-3.5" />
            {showNewDraft ? 'Close' : 'New draft'}
          </Button>
        }
      />

      {actionSuccess && (
        <div className="my-2.5 rounded-[10px] bg-success-soft border border-success/20 px-3.5 py-3 text-xs font-semibold text-success flex items-center justify-between">
          <span className="flex items-center gap-2">
            <Icon name="check" className="size-4" /> {actionSuccess}
          </span>
          <button
            type="button"
            onClick={() => setActionSuccess('')}
            className="hover:opacity-75"
          >
            <Icon name="close" className="size-3" />
          </button>
        </div>
      )}

      {(history.error || drafts.error || draftError) && (
        <ErrorNotice
          message={history.error || drafts.error || draftError}
          onDismiss={() => setDraftError('')}
        />
      )}

      {showNewDraft && (
        <form
          onSubmit={handleCreateDraft}
          className="mb-6 rounded-[14px] border border-line bg-white p-5 shadow-xs space-y-4"
        >
          <div className="flex items-center justify-between border-b border-line pb-3">
            <h3 className="text-base font-bold text-ink m-0">
              Create Migration Draft
            </h3>
            <span className="text-xs text-muted">
              Database: <b>{databaseName}</b>
            </span>
          </div>

          <label className="block">
            <span className="text-xs font-bold text-ink block mb-1">
              Migration Name
            </span>
            <input
              type="text"
              required
              placeholder="e.g. 001_create_posts_table"
              value={draftName}
              onChange={(e) => setDraftName(e.target.value)}
              className="h-10 w-full rounded-lg border border-line bg-white px-3 text-xs font-mono outline-none focus:border-violet"
            />
          </label>

          <label className="block">
            <span className="text-xs font-bold text-ink block mb-1">
              SQL Statements
            </span>
            <textarea
              required
              rows={5}
              placeholder="CREATE TABLE posts (id serial PRIMARY KEY, title text NOT NULL);"
              value={draftSql}
              onChange={(e) => setDraftSql(e.target.value)}
              className="w-full rounded-lg border border-line bg-white p-3 font-mono text-xs outline-none focus:border-violet resize-y"
            />
          </label>

          <div className="flex justify-end gap-2 pt-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setShowNewDraft(false)}
            >
              Cancel
            </Button>
            <Button variant="dark" size="sm" type="submit" loading={creating}>
              <Icon name="save" className="size-3.5" />
              Save draft
            </Button>
          </div>
        </form>
      )}

      <div className="mx-auto my-5 grid w-full max-w-[1100px] min-w-0 gap-8">
        <section className="grid min-w-0 gap-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-bold text-ink m-0">Pending Drafts</h2>
            <span className="text-xs text-muted">
              {drafts.data?.length ?? 0} available
            </span>
          </div>

          {drafts.data?.map((draft) => (
            <article
              className="grid grid-cols-[160px_minmax(0,1fr)_auto] gap-5 rounded-[14px] border border-accent/30 bg-white p-5 shadow-xs max-[600px]:grid-cols-[minmax(0,1fr)] max-[600px]:gap-3"
              key={draft.id}
            >
              <div>
                <span className="block text-[10px] font-bold uppercase tracking-wider text-muted">
                  Draft ID
                </span>
                <strong className="mt-1 block font-mono text-xs font-bold text-violet">
                  #{draft.id}
                </strong>
                <span className="mt-2 block text-[10px] text-muted">
                  Created {new Date(draft.created_at).toLocaleDateString()}
                </span>
              </div>

              <div className="min-w-0">
                <div className="flex items-center gap-2 mb-1.5">
                  <h3 className="m-0 text-sm font-bold text-ink">{draft.name}</h3>
                  <Status tone={draft.status === 'failed' ? 'red' : 'amber'}>
                    {draft.status}
                  </Status>
                </div>
                <pre className="mt-2 mb-0 overflow-auto rounded-lg bg-canvas p-3 font-mono text-xs text-muted leading-relaxed max-h-36">
                  {draft.sql}
                </pre>
              </div>

              <div className="flex flex-col justify-center items-end gap-2">
                <ConfirmAction
                  title={`Execute migration #${draft.id}?`}
                  description={`This will run the migration "${draft.name}" against target database "${databaseName}". This action alters database schema.`}
                  confirmLabel="Execute Migration"
                  onConfirm={() => handleExecuteDraft(draft)}
                  tone="primary"
                  trigger={
                    <Button variant="dark" size="sm">
                      <Icon name="play" className="size-3.5" />
                      Execute
                    </Button>
                  }
                />
              </div>
            </article>
          ))}

          {!drafts.data?.length && (
            <EmptyState
              icon="migration"
              title="No migration drafts"
              action={
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowNewDraft(true)}
                >
                  <Icon name="plus" className="size-3.5" />
                  Create first draft
                </Button>
              }
            >
              Drafts allow you to review and stage DDL schema changes before
              executing them against the database.
            </EmptyState>
          )}
        </section>

        <section className="grid min-w-0 gap-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-bold text-ink m-0">Applied Versions</h2>
            <span className="text-xs text-muted">
              {history.data?.entries.length ?? 0} applied
            </span>
          </div>

          <div className="overflow-hidden rounded-[14px] border border-line bg-white shadow-xs divide-y divide-line">
            {history.data?.entries.map((entry) => (
              <article
                className="grid grid-cols-[160px_minmax(0,1fr)_auto] items-center gap-5 p-4.5 max-[600px]:grid-cols-[minmax(0,1fr)] max-[600px]:gap-2"
                key={entry.version}
              >
                <div>
                  <span className="block text-[10px] font-bold uppercase tracking-wider text-muted">
                    Version
                  </span>
                  <strong className="mt-0.5 block font-mono text-xs font-bold text-violet">
                    v{entry.version}
                  </strong>
                </div>

                <div className="min-w-0">
                  <h3 className="m-0 text-sm font-bold text-ink">{entry.name}</h3>
                  <p className="mt-1 mb-0 text-[11px] text-muted">
                    Executed by user #{entry.executed_by} ·{' '}
                    {new Date(entry.executed_at).toLocaleString()}
                  </p>
                </div>

                <div className="flex items-center gap-2">
                  <span className="rounded-md bg-canvas border border-line px-2 py-1 font-mono text-[10px] text-muted">
                    {entry.duration_ms} ms
                  </span>
                  <Status tone="green">Applied</Status>
                </div>
              </article>
            ))}

            {!history.data?.entries.length && (
              <div className="py-8">
                <EmptyState icon="migration" title="No applied migrations">
                  No migrations have been recorded for this database yet.
                </EmptyState>
              </div>
            )}
          </div>
        </section>
      </div>
    </>
  )
}
