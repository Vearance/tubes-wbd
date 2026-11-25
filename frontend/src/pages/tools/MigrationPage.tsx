import { useEffect, useState } from 'react'
import { useParams, useSearchParams } from 'react-router'
import { AnimatePresence, m } from 'motion/react'
import { api } from '../../api'
import { UtilityHeader } from '../../components/AppShell'
import { AnimatedTabs } from '../../components/aceternity/AnimatedTabs'
import { Icon } from '../../components/Icon'
import { useWorkspaceContext } from '../../components/workspaceContext'
import { Button, ErrorNotice, Loading } from '../../components/ui'
import { useResource } from '../../hooks'
import {
  ConsensusAirlock,
  type AirlockFilter,
} from './migration/ConsensusAirlock'
import {
  DraftWorkbench,
  type InitialDraftSeed,
} from './migration/DraftWorkbench'
import { HeadLedger } from './migration/HeadLedger'

type MigrationTab = 'workbench' | 'airlock' | 'ledger'

export function MigrationPage() {
  const { databaseId = '0' } = useParams()
  const id = Number(databaseId)
  const [searchParams, setSearchParams] = useSearchParams()
  const { databases, reloadPendingApprovals } = useWorkspaceContext()

  const activeDb = databases.find((database) => database.id === id)
  const databaseName = activeDb?.name ?? `Database #${id}`
  const canEdit = activeDb?.permission === 'view_edit'
  const [hideRollback, setHideRollback] = useState(false)

  const history = useResource(
    () => api.historyAll(id, hideRollback),
    [id, hideRollback],
  )
  const drafts = useResource(() => api.drafts(id), [id])
  const executions = useResource(() => api.executions(id), [id])
  const { reload: reloadHistory } = history
  const { reload: reloadDrafts } = drafts
  const { reload: reloadExecutions } = executions

  const tabParam = searchParams.get('tab')
  const executionParam = searchParams.get('executionId')
  const newDraftParam = searchParams.get('newDraft') === '1'

  const initialTab: MigrationTab =
    tabParam === 'airlock' || tabParam === 'ledger' || tabParam === 'workbench'
      ? tabParam
      : executionParam
        ? 'airlock'
        : 'workbench'

  const [activeTab, setActiveTab] = useState<MigrationTab>(initialTab)
  const [airlockFilter, setAirlockFilter] = useState<AirlockFilter>('all')
  const [focusedExecutionId, setFocusedExecutionId] = useState<number | null>(
    executionParam ? Number(executionParam) : null,
  )
  const [showComposer, setShowComposer] = useState(newDraftParam)
  const [bannerSuccess, setBannerSuccess] = useState('')
  const [actionError, setActionError] = useState('')

  const initialSeed: InitialDraftSeed | null = newDraftParam
    ? {
        name: searchParams.get('name') ?? '',
        sql: searchParams.get('sql') ?? '',
        downSql: searchParams.get('downSql') ?? '',
        irreversibleReason: searchParams.get('irreversibleReason') ?? undefined,
      }
    : null

  const draftList = drafts.data ?? []
  const executionList = executions.data ?? []
  const historyList = history.data ?? []
  const latestSuccessfulExecution = executionList.find(
    (execution) => execution.status === 'success',
  )

  const hasLiveQueueOrRunning = executionList.some(
    (e) => e.status === 'queued' || e.status === 'running',
  )

  useEffect(() => {
    if (!hasLiveQueueOrRunning) return
    const timer = window.setInterval(() => {
      reloadExecutions()
      reloadDrafts()
      reloadHistory()
    }, 2500)
    return () => window.clearInterval(timer)
  }, [hasLiveQueueOrRunning, reloadExecutions, reloadDrafts, reloadHistory])

  const isLoading =
    (history.loading || drafts.loading || executions.loading) &&
    history.data === null &&
    drafts.data === null &&
    executions.data === null

  const pendingReviewCount = executionList.filter(
    (e) => e.status === 'pending_review',
  ).length

  async function reloadAll(message?: string) {
    setActionError('')
    await Promise.all([drafts.reload(), executions.reload(), history.reload()])
    reloadPendingApprovals()
    if (message) {
      setBannerSuccess(message)
      window.setTimeout(() => setBannerSuccess(''), 5000)
    }
  }

  function clearSeedParams() {
    if (newDraftParam) {
      const next = new URLSearchParams(searchParams)
      next.delete('newDraft')
      next.delete('name')
      next.delete('sql')
      next.delete('downSql')
      next.delete('irreversibleReason')
      setSearchParams(next, { replace: true })
    }
  }

  function trackDraftInAirlock(draftId: number) {
    const match = executionList.find((e) => e.draft_id === draftId)
    setFocusedExecutionId(match?.id ?? null)
    setAirlockFilter('all')
    setActiveTab('airlock')
  }

  return (
    <>
      <UtilityHeader
        backTo="/databases"
        path={databaseName}
        title="Migrations"
        status={
          isLoading
            ? 'Loading migrations…'
            : `${draftList.length} drafts · ${pendingReviewCount} pending review · ${historyList.length} history entries`
        }
        actions={
          <div className="flex w-full flex-wrap items-center gap-2.5 min-[901px]:w-auto">
            {hasLiveQueueOrRunning && (
              <span className="inline-flex items-center gap-1.5 rounded-full bg-violet-soft px-2.5 py-1 text-[11px] font-bold text-violet">
                <span className="size-2 rounded-full bg-violet animate-ping" />
                Live polling worker…
              </span>
            )}
            <Button
              variant="outline"
              size="sm"
              disabled={isLoading}
              onClick={() => void reloadAll()}
            >
              <Icon name="refresh" className="size-3.5" />
              Refresh
            </Button>
          </div>
        }
      />

      <AnimatePresence mode="wait">
        {isLoading ? (
          <m.div
            key="page-loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
            className="py-16"
          >
            <Loading label="Loading migration pipeline and ledger…" />
          </m.div>
        ) : (
          <m.div
            key="page-content"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.22, ease: 'easeOut' }}
            className="space-y-4"
          >

      {bannerSuccess && (
        <div className="my-2.5 flex items-center justify-between rounded-[10px] border border-success/25 bg-success-soft px-3.5 py-3 text-xs font-semibold text-success">
          <span className="flex items-center gap-2">
            <Icon name="check" className="size-4" />
            {bannerSuccess}
          </span>
          <button
            type="button"
            onClick={() => setBannerSuccess('')}
            className="hover:opacity-75"
            aria-label="Dismiss notification"
          >
            <Icon name="close" className="size-3.5" />
          </button>
        </div>
      )}

      {(history.error || drafts.error || executions.error || actionError) && (
        <ErrorNotice
          message={
            history.error || drafts.error || executions.error || actionError
          }
          onDismiss={() => setActionError('')}
        />
      )}

      <AnimatedTabs
        tabs={[
          {
            value: 'workbench',
            label: `Schema Workbench (${draftList.length})`,
          },
          {
            value: 'airlock',
            label: `Consensus Airlock (${executionList.length})`,
          },
          {
            value: 'ledger',
            label: `Live HEAD Ledger (${historyList.length})`,
          },
        ]}
        value={activeTab}
        onValueChange={(value) => setActiveTab(value as MigrationTab)}
        className="mb-4"
      />

      <AnimatePresence mode="wait">
        {activeTab === 'workbench' && (
          <m.div
            key="workbench"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.16, ease: 'easeOut' }}
          >
            <DraftWorkbench
              key={`${id}-${initialSeed?.sql ?? ''}`}
              databaseId={id}
              databaseName={databaseName}
              canEdit={canEdit}
              drafts={draftList}
              initialSeed={initialSeed}
              showComposer={showComposer}
              onToggleComposer={setShowComposer}
              onClearSeed={clearSeedParams}
              onSaved={async (msg) => {
                await reloadAll(msg)
              }}
              onSubmitted={async (executionId) => {
                setFocusedExecutionId(executionId)
                setAirlockFilter('pending_review')
                setActiveTab('airlock')
                await reloadAll(
                  `Execution #${executionId} submitted to the Consensus Airlock for team leader review.`,
                )
              }}
              onTrackExecution={trackDraftInAirlock}
            />
          </m.div>
        )}

        {activeTab === 'airlock' && (
          <m.div
            key="airlock"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.16, ease: 'easeOut' }}
          >
            <ConsensusAirlock
              executions={executionList}
              canEdit={canEdit}
              filter={airlockFilter}
              onFilterChange={setAirlockFilter}
              focusedExecutionId={focusedExecutionId}
              onMutated={async (msg) => {
                await reloadAll(msg)
              }}
            />
          </m.div>
        )}

        {activeTab === 'ledger' && (
          <m.div
            key="ledger"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.16, ease: 'easeOut' }}
          >
            <HeadLedger
              databaseId={id}
              databaseName={databaseName}
              canEdit={canEdit}
              entries={historyList}
              hideRollback={hideRollback}
              onHideRollbackChange={setHideRollback}
              latestSuccessfulExecution={latestSuccessfulExecution}
              onRollbackProposed={async (executionId) => {
                setFocusedExecutionId(executionId)
                setAirlockFilter('pending_review')
                setActiveTab('airlock')
                await reloadAll(
                  `Rollback execution #${executionId} submitted to Consensus Airlock for team leader sign-off.`,
                )
              }}
              onForwardProposed={async (executionId) => {
                setFocusedExecutionId(executionId)
                setAirlockFilter('pending_review')
                setActiveTab('airlock')
                await reloadAll(
                  `Forward execution #${executionId} submitted to Consensus Airlock for team leader sign-off.`,
                )
              }}
            />
          </m.div>
        )}
      </AnimatePresence>
          </m.div>
        )}
      </AnimatePresence>
    </>
  )
}
