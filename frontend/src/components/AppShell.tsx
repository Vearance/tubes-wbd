import { useEffect, useRef, useState, type ReactNode } from 'react'
import { AnimatePresence, m, useReducedMotion } from 'motion/react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { api, session } from '../api'
import { useResource } from '../hooks'
import { cn } from '../lib/utils'
import { DatabaseMark, Icon, type IconName } from './Icon'
import type { WorkspaceContextValue } from './workspaceContext'

type NavItem = {
  to: string
  icon: IconName
  label: string
  end?: boolean
  badge?: number
}

const databaseNav = (
  id: string,
  schema = 'public',
  dbPending = 0,
): NavItem[] => [
  { to: `/databases/${id}/${schema}/query`, icon: 'code', label: 'SQL editor' },
  { to: `/databases/${id}/${schema}/schema`, icon: 'schema', label: 'Schema' },
  {
    to: `/databases/${id}/migrations`,
    icon: 'migration',
    label: 'Migrations',
    badge: dbPending > 0 ? dbPending : undefined,
  },
]

const adminNav: NavItem[] = [
  { to: '/admin/users', icon: 'users', label: 'Users' },
  { to: '/admin/groups', icon: 'groups', label: 'Groups' },
  { to: '/admin/databases', icon: 'database', label: 'Databases' },
  { to: '/admin/logs', icon: 'shield', label: 'Governance' },
]

const brandClasses =
  'inline-flex min-h-11 items-center gap-2.5 text-lg font-black tracking-tight text-ink no-underline max-[900px]:col-start-1 max-[900px]:row-start-1'
const pickerClasses =
  'grid min-h-16 w-full grid-cols-[38px_minmax(0,1fr)_auto] items-center gap-2.5 rounded-2xl border border-line/75 bg-white p-2.5 text-left text-ink shadow-2xs transition-colors hover:border-violet/50 hover:bg-violet-soft/20'
const glyphClasses =
  'grid size-[38px] place-items-center rounded-xl bg-violet-soft text-violet shadow-2xs'
const navLabelClasses =
  'mx-3 mb-2 text-[10px] font-extrabold uppercase tracking-wider text-[#a09aa7] max-[900px]:col-span-full max-[900px]:mx-3 max-[900px]:mt-1 max-[900px]:mb-0'

export function AppShell({
  databaseId,
  schema = 'public',
}: {
  databaseId?: string
  schema?: string
}) {
  const user = session.user()!
  const navigate = useNavigate()
  const location = useLocation()
  const reduceMotion = useReducedMotion()
  const isAdminRoute = location.pathname.startsWith('/admin')
  const [tableSearch, setTableSearch] = useState('')
  const [switcherOpen, setSwitcherOpen] = useState(false)
  const switcherRef = useRef<HTMLDivElement>(null)

  const databases = useResource(
    () => (user.is_admin ? Promise.resolve([]) : api.databases()),
    [user.is_admin],
  )
  const pendingApprovals = useResource(
    () => (user.is_admin ? Promise.resolve([]) : api.pendingApprovals()),
    [user.is_admin],
  )
  const tables = useResource(
    () =>
      !user.is_admin && databaseId
        ? api.tables(Number(databaseId), schema)
        : Promise.resolve([]),
    [user.is_admin, databaseId, schema],
  )

  useEffect(() => {
    if (!switcherOpen) return
    function onPointerDown(e: MouseEvent) {
      if (switcherRef.current && !switcherRef.current.contains(e.target as Node)) {
        setSwitcherOpen(false)
      }
    }
    function onKeyDown(e: globalThis.KeyboardEvent) {
      if (e.key === 'Escape') setSwitcherOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [switcherOpen])

  const allDatabases = databases.data ?? []
  const activeDatabase = allDatabases.find(
    (database) => String(database.id) === databaseId,
  )
  const allPending = pendingApprovals.data ?? []
  const dbPendingCount = databaseId
    ? allPending.filter((item) => String(item.database_id) === databaseId).length
    : 0

  const nav: NavItem[] = isAdminRoute
    ? adminNav
    : databaseId
      ? databaseNav(databaseId, schema, dbPendingCount)
      : []

  const filteredTables =
    tables.data?.filter((t) =>
      t.name.toLowerCase().includes(tableSearch.toLowerCase()),
    ) ?? []

  async function logout() {
    try {
      await api.logout()
    } finally {
      session.clear()
      navigate('/login', { replace: true })
    }
  }

  return (
    <div className="grid min-h-dvh grid-cols-[240px_minmax(0,1fr)] max-[900px]:grid-cols-[minmax(0,1fr)]">
      <aside className="sticky top-0 flex h-dvh flex-col border-r border-line/70 bg-white/85 backdrop-blur-md px-3.5 pt-5 pb-4 max-[900px]:z-20 max-[900px]:grid max-[900px]:h-auto max-[900px]:w-full max-[900px]:grid-cols-[minmax(0,1fr)_auto] max-[900px]:grid-rows-[auto] max-[900px]:gap-x-3 max-[900px]:gap-y-2 max-[900px]:border-r-0 max-[900px]:border-b max-[900px]:px-4 max-[900px]:py-3 max-[600px]:gap-2">
        <NavLink
          className={brandClasses}
          to={user.is_admin ? '/admin/users' : '/databases'}
        >
          <img
            src="/bibi.png"
            alt="BScheme mascot"
            className="size-8 object-contain"
          />
          BScheme
        </NavLink>

        {isAdminRoute ? (
          <div className={cn(pickerClasses, 'my-4 max-[900px]:col-span-full max-[900px]:m-0')}>
            <span className={glyphClasses}>
              <Icon name="users" />
            </span>
            <span className="min-w-0">
              <strong className="block truncate text-xs font-bold text-ink">
                Administration
              </strong>
              <small className="mt-0.5 block truncate text-[10px] text-muted">
                Access and connections
              </small>
            </span>
          </div>
        ) : (
          <div
            ref={switcherRef}
            className="relative my-4 max-[900px]:col-span-full max-[900px]:m-0 max-[900px]:w-full"
          >
            <button
              className={cn(
                pickerClasses,
                'cursor-pointer',
                switcherOpen && 'border-violet bg-violet-soft/30',
              )}
              type="button"
              aria-label="Switch database"
              aria-expanded={switcherOpen}
              onClick={() => setSwitcherOpen((prev) => !prev)}
            >
              <DatabaseMark connected={Boolean(databaseId && activeDatabase)} />
              <span className="min-w-0">
                <strong className="block truncate text-xs font-bold text-ink">
                  {activeDatabase?.name ??
                    (databaseId
                      ? `Database #${databaseId}`
                      : allDatabases.length
                        ? 'Select database'
                        : 'No databases')}
                </strong>
                <small className="mt-0.5 block truncate text-[10px] text-muted">
                  {databaseId ? `${schema} · PostgreSQL` : 'Select database'}
                </small>
              </span>
              <Icon
                name="chevrons-up-down"
                className={cn(
                  'size-4 text-muted shrink-0 transition-transform duration-150',
                  switcherOpen && 'text-violet',
                )}
              />
            </button>

            <AnimatePresence>
              {switcherOpen && (
                <m.div
                  initial={reduceMotion ? false : { opacity: 0, y: -6, scale: 0.98 }}
                  animate={{ opacity: 1, y: 0, scale: 1 }}
                  exit={
                    reduceMotion
                      ? undefined
                      : { opacity: 0, y: -4, scale: 0.98, transition: { duration: 0.12 } }
                  }
                  transition={{ duration: 0.16, ease: 'easeOut' }}
                  className="absolute right-0 left-0 top-[calc(100%+6px)] z-40 overflow-hidden rounded-[14px] border border-line-strong bg-white p-1.5 shadow-surface"
                >
                  <div className="max-h-56 overflow-y-auto space-y-0.5 [scrollbar-width:thin]">
                    {allDatabases.map((db) => {
                      const isCurrent = String(db.id) === databaseId
                      const pendingCount = allPending.filter(
                        (item) => item.database_id === db.id,
                      ).length
                      return (
                        <button
                          key={db.id}
                          type="button"
                          onClick={() => {
                            setSwitcherOpen(false)
                            navigate(`/databases/${db.id}/public/tables`)
                          }}
                          className={cn(
                            'flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-xs transition-colors cursor-pointer',
                            isCurrent
                              ? 'bg-violet-soft/60 text-violet font-bold'
                              : 'text-ink hover:bg-canvas',
                          )}
                        >
                          <DatabaseMark
                            connected={isCurrent}
                            className="relative grid size-7 shrink-0 place-items-center rounded-lg bg-violet-soft text-violet"
                          />
                          <span className="min-w-0 flex-1 truncate">
                            {db.name}
                          </span>
                          {pendingCount > 0 && (
                            <span className="inline-flex min-w-4 items-center justify-center rounded-full bg-warning-soft px-1 text-[10px] font-extrabold text-warning">
                              {pendingCount}
                            </span>
                          )}
                        </button>
                      )
                    })}
                    {allDatabases.length === 0 && (
                      <div className="px-3 py-3 text-center text-[11px] text-muted">
                        No databases available
                      </div>
                    )}
                  </div>
                </m.div>
              )}
            </AnimatePresence>
          </div>
        )}

        {!isAdminRoute && databaseId && (
          <div className="flex flex-col min-h-0 flex-1 my-1 max-[900px]:col-span-full">
            <div className="flex items-center justify-between mx-1 mb-1.5">
              <p className={cn(navLabelClasses, 'm-0')}>
                Tables ({tables.data?.length ?? 0})
              </p>
            </div>

            {(tables.data?.length ?? 0) > 6 && (
              <div className="mb-2 px-1">
                <input
                  type="text"
                  placeholder="Filter tables…"
                  value={tableSearch}
                  onChange={(e) => setTableSearch(e.target.value)}
                  className="w-full rounded-md border border-line bg-canvas px-2.5 py-1 text-[11px] placeholder:text-muted/60 outline-none focus:border-violet"
                />
              </div>
            )}

            <nav className="min-h-0 flex-1 overflow-y-auto overscroll-y-contain [scrollbar-width:thin] max-[900px]:flex max-[900px]:max-h-12 max-[900px]:overflow-x-auto max-[900px]:overflow-y-hidden max-[900px]:overscroll-x-contain">
              {tables.loading && !tables.data && (
                <div className="px-3 py-2 text-[11px] text-muted">
                  Loading tables…
                </div>
              )}
              {filteredTables.map((entry) => (
                <NavLink
                  key={`${entry.schema}.${entry.name}`}
                  className={({ isActive }) =>
                    cn(
                      'my-0.5 flex min-h-[34px] w-full items-center gap-2 rounded-lg px-2.5 font-mono text-[11px] text-muted no-underline transition-colors hover:bg-canvas hover:text-ink max-[900px]:w-auto max-[900px]:min-w-[130px] max-[900px]:shrink-0',
                      isActive &&
                        'bg-violet-soft font-semibold text-[#6c3eaa] shadow-xs',
                    )
                  }
                  to={`/databases/${databaseId}/${entry.schema}/${entry.name}`}
                >
                  <span className="size-1.5 shrink-0 rounded-full bg-violet" />
                  <span className="truncate flex-1">{entry.name}</span>
                  <b className="font-sans text-[9px] font-medium text-[#aaa4af] uppercase">
                    {entry.kind === 'view' ? 'View' : ''}
                  </b>
                </NavLink>
              ))}
              {!tables.loading && tables.data?.length === 0 && (
                <div className="px-3 py-2 text-[11px] text-muted italic">
                  No tables found
                </div>
              )}
            </nav>
          </div>
        )}

        {nav.length > 0 ? (
          <div>
            <p
              className={cn(
                navLabelClasses,
                !isAdminRoute && databaseId && 'mt-4 max-[900px]:mt-1',
              )}
            >
              {isAdminRoute ? 'Manage' : 'Workspace'}
            </p>
            <nav className="max-[900px]:col-span-full max-[900px]:flex max-[900px]:overflow-x-auto max-[900px]:overscroll-x-contain max-[900px]:[scrollbar-width:thin] gap-1">
              {nav.map((item) => (
                <NavLink
                  key={item.to}
                  end={item.end}
                  className={({ isActive }) =>
                    cn(
                      'my-1 flex min-h-[42px] items-center gap-3 rounded-xl px-3 text-xs font-semibold text-[#736e7a] no-underline hover:bg-violet-soft/40 hover:text-ink max-[900px]:min-h-10 max-[900px]:shrink-0 max-[900px]:whitespace-nowrap transition-colors',
                      isActive &&
                        'bg-violet text-white hover:bg-violet hover:text-white shadow-xs',
                    )
                  }
                  to={item.to}
                >
                  {({ isActive }) => (
                    <>
                      <Icon name={item.icon} className="size-4 shrink-0" />
                      <span className="flex-1 truncate">{item.label}</span>
                      {item.badge !== undefined && item.badge > 0 && (
                        <span
                          className={cn(
                            'inline-flex min-w-5 items-center justify-center rounded-full px-1.5 py-0.5 text-[10px] font-extrabold tabular-nums',
                            isActive
                              ? 'bg-white/20 text-white'
                              : 'bg-warning-soft text-warning border border-warning/20',
                          )}
                        >
                          {item.badge}
                        </span>
                      )}
                    </>
                  )}
                </NavLink>
              ))}
            </nav>
          </div>
        ) : (
          <div className="flex-1" />
        )}

        <div className="mt-auto grid min-h-[60px] grid-cols-[36px_minmax(0,1fr)_32px] items-center gap-2.5 border-t border-line pt-3 max-[900px]:col-start-2 max-[900px]:row-start-1 max-[900px]:m-0 max-[900px]:min-h-11 max-[900px]:min-w-[190px] max-[900px]:border-t-0 max-[900px]:border-l max-[900px]:pt-0 max-[900px]:pr-0 max-[900px]:pl-3 max-[600px]:min-w-[80px] max-[600px]:grid-cols-[36px_32px]">
          <span className="grid size-9 place-items-center rounded-full bg-violet-soft text-xs font-black text-violet shadow-xs">
            {user.username.slice(0, 2).toUpperCase()}
          </span>
          <span className="min-w-0 max-[600px]:hidden">
            <strong className="block truncate text-xs font-bold text-ink">
              {user.username}
            </strong>
            <small className="block truncate text-[10px] text-muted">
              {user.is_admin ? 'Administrator' : 'Database user'}
            </small>
          </span>
          <button
            className="size-8 grid place-items-center rounded-lg text-muted hover:bg-danger-soft hover:text-danger cursor-pointer"
            aria-label="Log out"
            title="Log out"
            onClick={logout}
          >
            <Icon name="logout" className="size-4" />
          </button>
        </div>
      </aside>

      <main className="min-w-0 px-6 pt-5 pb-10 [font-variant-numeric:tabular-nums] [&_h1]:text-balance [&_h2]:text-balance [&_h3]:text-balance [&_p]:text-pretty max-[900px]:w-full max-[900px]:p-4 max-[600px]:p-3">
        <Outlet
          context={
            {
              databases: allDatabases,
              pendingApprovals: allPending,
              reloadPendingApprovals: pendingApprovals.reload,
            } satisfies WorkspaceContextValue
          }
        />
      </main>
    </div>
  )
}

export function UtilityHeader({
  path,
  title,
  status,
  actions,
  backTo,
}: {
  path: ReactNode
  title: ReactNode
  status?: ReactNode
  actions?: ReactNode
  backTo?: string
}) {
  return (
    <header className="mb-4 flex min-h-[58px] items-center gap-4 border-b border-line/60 pb-3 max-[900px]:flex-col max-[900px]:items-start max-[900px]:pb-2">
      <div className="flex min-w-0 flex-wrap items-center gap-3 max-[900px]:w-full">
        {backTo && (
          <Link
            to={backTo}
            className="size-8.5 rounded-xl border border-line/80 bg-white/90 shadow-2xs grid place-items-center text-muted hover:text-ink hover:border-line-strong transition-colors"
            aria-label="Go back"
          >
            <Icon name="chevron-left" className="size-4" />
          </Link>
        )}
        <span className="inline-flex min-h-[30px] items-center gap-2 rounded-full border border-line/70 bg-white/75 px-3 font-mono text-[11px] text-muted shadow-2xs max-[600px]:min-h-7 max-[600px]:w-full max-[600px]:border-r-0 max-[600px]:pr-0">
          <i className="size-2 rounded-full bg-success shadow-[0_0_0_3px_var(--green-soft)] shrink-0" />
          <span className="truncate">{path}</span>
        </span>
        <h1 className="m-0 text-xl font-bold tracking-tight text-ink max-[600px]:text-lg">
          {title}
        </h1>
        {status && (
          <span className="rounded-full bg-white/80 border border-line/70 px-2.5 py-0.5 text-[10px] font-semibold text-muted shadow-2xs">
            {status}
          </span>
        )}
      </div>
      {actions && (
        <div className="ml-auto flex flex-wrap items-center gap-2 max-[900px]:ml-0 max-[900px]:w-full max-[900px]:justify-start max-[600px]:[&_button]:flex-1">
          {actions}
        </div>
      )}
    </header>
  )
}
