import { Link, useParams } from 'react-router'
import { UtilityHeader } from '../../components/AppShell'
import { Icon } from '../../components/Icon'
import { EmptyState } from '../../components/ui'

export function EditPage() {
  const { databaseId = '0', schema = 'public' } = useParams()
  const id = Number(databaseId)

  return (
    <>
      <UtilityHeader
        backTo={`/databases/${id}/${schema}/tables`}
        path="Table workspace"
        title="Table changes"
        status="Read-only API"
      />
      <div className="my-5 flex min-h-[72px] min-w-0 items-center gap-3.5 rounded-[14px] border border-violet/20 bg-violet-soft/60 px-5 py-4 shadow-xs">
        <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-violet text-white shadow-xs">
          <Icon name="edit" className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <b className="block text-sm font-bold text-ink">
            Direct row mutations disabled
          </b>
          <p className="mt-0.5 mb-0 text-xs text-[#73578e]">
            The platform API exposes table data for reads and schema management via
            migrations. For modifying schemas, use migration drafts.
          </p>
        </div>
        <div className="flex gap-2 shrink-0">
          <Link
            to={`/databases/${id}/${schema}/query`}
            className="rounded-lg border border-line bg-white px-3 py-1.5 text-xs font-semibold text-ink hover:bg-canvas transition-colors"
          >
            SQL Editor
          </Link>
          <Link
            to={`/databases/${id}/migrations`}
            className="rounded-lg bg-violet px-3 py-1.5 text-xs font-semibold text-white hover:bg-violet/90 transition-colors"
          >
            Migrations
          </Link>
        </div>
      </div>
      <EmptyState
        icon="migration"
        title="Manage schema through migrations"
        action={
          <Link
            to={`/databases/${id}/migrations`}
            className="inline-flex min-h-9 items-center justify-center gap-1.5 rounded-lg bg-violet px-4 text-xs font-bold text-white hover:bg-violet/90 transition-colors"
          >
            View Migrations
          </Link>
        }
      >
        Track, review, and execute safe versioned schema alterations.
      </EmptyState>
    </>
  )
}
