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
  'grid size-[38px] place-items-center rounded-xl bg-violet-soft text-violet-fg shadow-2xs'
const navLabelClasses =
  'mx-3 mb-2 text-[11px] font-extrabold uppercase tracking-wider text-muted max-[900px]:col-span-full max-[900px]:mx-3 max-[900px]:mt-1 max-[900px]:mb-0'

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
  const [mobileSheet, setMobileSheet] = useState<'none' | 'menu' | 'tables'>('none')
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

  // Close mobile drawer on route navigation
  const [prevPathname, setPrevPathname] = useState(location.pathname)
  if (prevPathname !== location.pathname) {
    setPrevPathname(location.pathname)
    setMobileSheet('none')
  }

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

  const isTableRoute = Boolean(
    databaseId &&
      !isAdminRoute &&
      !location.pathname.endsWith('/query') &&
      !location.pathname.endsWith('/schema') &&
      !location.pathname.includes('/migrations'),
  )

  async function logout() {
    try {
      await api.logout()
    } finally {
      session.clear()
      navigate('/login', { replace: true })
    }
  }

  return (
    <div className="min-h-dvh overflow-x-clip min-[901px]:grid min-[901px]:grid-cols-[240px_minmax(0,1fr)]">
      {/* ─── DESKTOP SIDEBAR (>= 901px) ─── */}
      <aside className="sticky top-0 hidden h-dvh flex-col border-r border-line/70 bg-white/85 backdrop-blur-md px-3.5 pt-5 pb-4 min-[901px]:flex">
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
          <div className={cn(pickerClasses, 'my-4')}>
            <span className={glyphClasses}>
              <Icon name="users" />
            </span>
            <span className="min-w-0">
              <strong className="block truncate text-xs font-bold text-ink">
                Administration
              </strong>
              <small className="mt-0.5 block truncate text-[11px] text-muted">
                Access and connections
              </small>
            </span>
          </div>
        ) : (
          <div
            ref={switcherRef}
            className="relative my-4"
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
                <small className="mt-0.5 block truncate text-[11px] text-muted">
                  {databaseId ? `${schema} · PostgreSQL` : 'Select database'}
                </small>
              </span>
              <Icon
                name="chevrons-up-down"
                className={cn(
                  'size-4 text-muted shrink-0 transition-transform duration-150',
                  switcherOpen && 'text-violet-fg',
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
                  className="absolute right-0 left-0 top-[calc(100%+6px)] z-40 overflow-hidden rounded-xl border border-line-strong bg-white p-1.5 shadow-surface"
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
                              ? 'bg-violet-soft/60 text-violet-fg font-bold'
                              : 'text-ink hover:bg-canvas',
                          )}
                        >
                          <DatabaseMark
                            connected={isCurrent}
                            className="relative grid size-7 shrink-0 place-items-center rounded-lg bg-violet-soft text-violet-fg"
                          />
                          <span className="min-w-0 flex-1 truncate">
                            {db.name}
                          </span>
                          {pendingCount > 0 && (
                            <span className="inline-flex min-w-4 items-center justify-center rounded-full bg-warning-soft px-1.5 text-[11px] font-extrabold text-warning-fg">
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
          <div className="flex flex-col min-h-0 flex-1 my-1">
            <div className="flex items-center justify-between mx-1 mb-1.5">
              <p className={cn(navLabelClasses, 'm-0')}>
                Tables ({tables.data?.length ?? 0})
              </p>
            </div>

            {(tables.data?.length ?? 0) > 6 && (
              <div className="mb-2 px-1">
                <input
                  type="text"
                  aria-label="Filter tables"
                  placeholder="Filter tables…"
                  value={tableSearch}
                  onChange={(e) => setTableSearch(e.target.value)}
                  className="w-full rounded-lg border border-line bg-canvas px-2.5 py-1.5 text-xs text-ink placeholder:text-muted/70 outline-none focus:border-violet focus:ring-2 focus:ring-violet/25"
                />
              </div>
            )}

            <nav className="min-h-0 flex-1 overflow-y-auto overscroll-y-contain [scrollbar-width:thin]">
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
                      'my-0.5 flex min-h-[34px] w-full items-center gap-2 rounded-lg px-2.5 font-mono text-[11px] text-muted no-underline transition-colors hover:bg-canvas hover:text-ink',
                      isActive &&
                        'bg-violet-soft font-semibold text-violet-fg shadow-xs',
                    )
                  }
                  to={`/databases/${databaseId}/${entry.schema}/${entry.name}`}
                >
                  <span className="size-1.5 shrink-0 rounded-full bg-violet" />
                  <span className="truncate flex-1">{entry.name}</span>
                  <b className="font-sans text-[11px] font-semibold text-muted uppercase">
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
                !isAdminRoute && databaseId && 'mt-4',
              )}
            >
              {isAdminRoute ? 'Manage' : 'Workspace'}
            </p>
            <nav className="space-y-1">
              {nav.map((item) => (
                <NavLink
                  key={item.to}
                  end={item.end}
                  className={({ isActive }) =>
                    cn(
                      'my-1 flex min-h-[42px] items-center gap-3 rounded-xl px-3 text-xs font-semibold text-muted no-underline hover:bg-violet-soft/40 hover:text-ink transition-colors',
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
                            'inline-flex min-w-5 items-center justify-center rounded-full px-1.5 py-0.5 text-[11px] font-extrabold tabular-nums',
                            isActive
                              ? 'bg-white/20 text-white'
                              : 'bg-warning-soft text-warning-fg border border-warning/25',
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

        <div className="mt-auto grid min-h-[60px] grid-cols-[36px_minmax(0,1fr)_32px] items-center gap-2.5 border-t border-line pt-3">
          <span className="grid size-9 place-items-center rounded-full bg-violet-soft text-xs font-black text-violet-fg shadow-xs">
            {user.username.slice(0, 2).toUpperCase()}
          </span>
          <span className="min-w-0">
            <strong className="block truncate text-xs font-bold text-ink">
              {user.username}
            </strong>
            <small className="block truncate text-xs text-muted">
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

      {/* ─── MOBILE STICKY TOP APP BAR (< 901px) ─── */}
      <header className="sticky top-0 z-40 flex h-14 w-full items-center justify-between gap-2 border-b border-line/80 bg-white/92 px-3.5 backdrop-blur-md min-[901px]:hidden shadow-2xs">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <NavLink
            className="inline-flex items-center gap-2 text-base font-black tracking-tight text-ink no-underline shrink-0"
            to={user.is_admin ? '/admin/users' : '/databases'}
          >
            <img
              src="/bibi.png"
              alt="BScheme mascot"
              className="size-7 object-contain"
            />
            <span className="font-black">BScheme</span>
          </NavLink>

          {!isAdminRoute && activeDatabase && (
            <button
              type="button"
              onClick={() => setMobileSheet('menu')}
              className="flex min-w-0 items-center gap-1.5 rounded-full border border-line bg-canvas/70 px-2.5 py-1.5 text-xs font-semibold text-ink max-w-[170px] sm:max-w-[230px] cursor-pointer hover:bg-canvas"
            >
              <span className="size-1.5 rounded-full bg-success shrink-0" />
              <span className="truncate">{activeDatabase.name}</span>
              <Icon name="chevrons-up-down" className="size-3 text-muted shrink-0" />
            </button>
          )}

          {isAdminRoute && (
            <span className="rounded-full border border-violet/20 bg-violet-soft/80 px-2.5 py-0.5 text-[11px] font-bold text-violet-fg">
              Admin
            </span>
          )}
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {dbPendingCount > 0 && !isAdminRoute && (
            <Link
              to={`/databases/${databaseId}/migrations`}
              className="relative grid size-10 place-items-center rounded-xl bg-warning-soft/80 text-warning-fg border border-warning/25 no-underline"
              aria-label={`${dbPendingCount} pending approvals`}
            >
              <Icon name="migration" className="size-4" />
              <span className="absolute -top-1 -right-1 grid min-w-4.5 h-4.5 px-1 place-items-center rounded-full bg-warning-fg text-[11px] font-black text-white shadow-xs">
                {dbPendingCount}
              </span>
            </Link>
          )}

          <button
            type="button"
            onClick={() =>
              setMobileSheet((prev) => (prev === 'menu' ? 'none' : 'menu'))
            }
            className="grid size-10 place-items-center rounded-xl border border-line/80 bg-white text-ink hover:bg-canvas transition-colors cursor-pointer shadow-2xs"
            aria-label={mobileSheet === 'menu' ? 'Close menu' : 'Open menu'}
            aria-expanded={mobileSheet === 'menu'}
          >
            <Icon name={mobileSheet === 'menu' ? 'close' : 'grid'} className="size-4" />
          </button>
        </div>
      </header>

      {/* ─── MOBILE DRAWER MENU / TABLES SHEET (< 901px) ─── */}
      <AnimatePresence>
        {mobileSheet !== 'none' && (
          <>
            <m.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.15 }}
              className="fixed inset-0 z-40 bg-black/40 backdrop-blur-xs min-[901px]:hidden"
              onClick={() => setMobileSheet('none')}
            />
            {mobileSheet === 'menu' ? (
              <m.div
                key="mobile-menu-sheet"
                initial={reduceMotion ? { opacity: 0 } : { y: -12, opacity: 0 }}
                animate={{ y: 0, opacity: 1 }}
                exit={reduceMotion ? { opacity: 0 } : { y: -12, opacity: 0 }}
                transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
                className="fixed inset-x-0 top-14 z-50 max-h-[calc(100dvh-60px)] overflow-y-auto border-b border-line-strong bg-white p-4 shadow-2xl min-[901px]:hidden flex flex-col space-y-4 [scrollbar-width:thin]"
              >
                {/* Database Switcher in Mobile Drawer */}
                {!isAdminRoute && allDatabases.length > 0 && (
                  <div className="space-y-1.5 rounded-2xl border border-line bg-canvas/40 p-3">
                    <div className="flex items-center justify-between px-1">
                      <span className="text-[11px] font-extrabold uppercase tracking-wider text-muted">
                        Target Database
                      </span>
                      <Link
                        to="/databases"
                        onClick={() => setMobileSheet('none')}
                        className="text-xs font-bold text-violet-fg hover:underline"
                      >
                        All databases →
                      </Link>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5 max-h-44 overflow-y-auto pr-1 [scrollbar-width:thin]">
                      {allDatabases.map((db) => {
                        const isCurrent = String(db.id) === databaseId
                        return (
                          <button
                            key={db.id}
                            type="button"
                            onClick={() => {
                              setMobileSheet('none')
                              navigate(`/databases/${db.id}/public/tables`)
                            }}
                            className={cn(
                              'flex items-center gap-2 rounded-xl px-3 py-2 text-left text-xs transition-colors cursor-pointer border',
                              isCurrent
                                ? 'border-violet bg-violet-soft/60 text-violet-fg font-bold'
                                : 'border-line/70 bg-white text-ink hover:bg-canvas',
                            )}
                          >
                            <DatabaseMark
                              connected={isCurrent}
                              className="relative grid size-6 shrink-0 place-items-center rounded-lg bg-violet-soft text-violet-fg"
                            />
                            <span className="truncate flex-1 font-semibold">{db.name}</span>
                            {isCurrent && (
                              <Icon name="check" className="size-3.5 text-violet-fg shrink-0" />
                            )}
                          </button>
                        )
                      })}
                    </div>
                  </div>
                )}

                {/* Workspace Navigation Links */}
                {nav.length > 0 && (
                  <div className="space-y-1.5">
                    <span className="px-1 text-[11px] font-extrabold uppercase tracking-wider text-muted block">
                      {isAdminRoute ? 'Admin Management' : 'Workspace Views'}
                    </span>
                    <div className="grid grid-cols-2 gap-1.5">
                      {nav.map((item) => (
                        <NavLink
                          key={item.to}
                          end={item.end}
                          onClick={() => setMobileSheet('none')}
                          className={({ isActive }) =>
                            cn(
                              'flex min-h-11 items-center gap-2.5 rounded-xl px-3 text-xs font-semibold no-underline transition-colors border',
                              isActive
                                ? 'bg-violet text-white border-violet shadow-xs'
                                : 'border-line/80 bg-canvas/30 text-ink hover:bg-violet-soft/30',
                            )
                          }
                          to={item.to}
                        >
                          {({ isActive }) => (
                            <>
                              <Icon name={item.icon} className="size-4 shrink-0" />
                              <span className="truncate flex-1">{item.label}</span>
                              {item.badge !== undefined && item.badge > 0 && (
                                <span
                                  className={cn(
                                    'inline-flex min-w-5 items-center justify-center rounded-full px-1.5 text-[11px] font-black',
                                    isActive
                                      ? 'bg-white/20 text-white'
                                      : 'bg-warning-fg text-white',
                                  )}
                                >
                                  {item.badge}
                                </span>
                              )}
                            </>
                          )}
                        </NavLink>
                      ))}
                    </div>
                  </div>
                )}

                {/* User Profile & Logout in Drawer */}
                <div className="flex items-center justify-between border-t border-line pt-3 px-1">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <span className="grid size-9 place-items-center rounded-full bg-violet-soft text-xs font-black text-violet-fg shadow-xs shrink-0">
                      {user.username.slice(0, 2).toUpperCase()}
                    </span>
                    <div className="min-w-0">
                      <strong className="block truncate text-xs font-bold text-ink">
                        {user.username}
                      </strong>
                      <small className="block truncate text-xs text-muted">
                        {user.is_admin ? 'Administrator' : 'Database user'}
                      </small>
                    </div>
                  </div>

                  <button
                    type="button"
                    onClick={logout}
                    className="flex min-h-10 items-center gap-1.5 rounded-xl border border-danger/20 bg-danger-soft/60 px-3.5 py-1.5 text-xs font-bold text-danger-fg hover:bg-danger-soft cursor-pointer transition-colors"
                  >
                    <Icon name="logout" className="size-3.5" />
                    <span>Log out</span>
                  </button>
                </div>
              </m.div>
            ) : !isAdminRoute && databaseId ? (
              <m.div
                key="mobile-tables-sheet"
                initial={reduceMotion ? { opacity: 0 } : { y: 16, opacity: 0 }}
                animate={{ y: 0, opacity: 1 }}
                exit={reduceMotion ? { opacity: 0 } : { y: 16, opacity: 0 }}
                transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
                className="fixed inset-x-0 bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-50 max-h-[70dvh] overflow-y-auto rounded-t-2xl border-t border-line-strong bg-white p-4 shadow-2xl min-[901px]:hidden flex flex-col space-y-3 [scrollbar-width:thin]"
              >
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className="text-xs font-extrabold uppercase tracking-wider text-muted">
                      Tables ({tables.data?.length ?? 0})
                    </span>
                    <span className="rounded-md border border-line bg-surface-subtle px-2 py-0.5 font-mono text-[11px] font-semibold text-violet-fg">
                      {schema}
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={() => setMobileSheet('none')}
                    className="grid size-9 place-items-center rounded-xl border border-line bg-canvas text-muted hover:text-ink cursor-pointer"
                    aria-label="Close tables list"
                  >
                    <Icon name="close" className="size-4" />
                  </button>
                </div>

                {(tables.data?.length ?? 0) > 4 && (
                  <input
                    type="text"
                    aria-label="Filter tables"
                    placeholder="Filter tables…"
                    value={tableSearch}
                    onChange={(e) => setTableSearch(e.target.value)}
                    className="w-full rounded-xl border border-line bg-surface-subtle px-3 py-2 text-base sm:text-xs text-ink placeholder:text-muted/70 outline-none focus:border-violet focus:ring-2 focus:ring-violet/25"
                  />
                )}

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5 overflow-y-auto pr-1 [scrollbar-width:thin]">
                  {tables.loading && !tables.data && (
                    <div className="col-span-full py-4 text-center text-xs text-muted">
                      Loading tables…
                    </div>
                  )}
                  {filteredTables.map((entry) => (
                    <NavLink
                      key={`${entry.schema}.${entry.name}`}
                      onClick={() => setMobileSheet('none')}
                      className={({ isActive }) =>
                        cn(
                          'flex min-h-10 items-center gap-2 rounded-xl px-3 font-mono text-xs text-muted no-underline transition-colors hover:bg-canvas hover:text-ink',
                          isActive &&
                            'bg-violet-soft font-semibold text-violet-fg shadow-2xs',
                        )
                      }
                      to={`/databases/${databaseId}/${entry.schema}/${entry.name}`}
                    >
                      <span className="size-1.5 shrink-0 rounded-full bg-violet" />
                      <span className="truncate flex-1">{entry.name}</span>
                      {entry.kind === 'view' && (
                        <b className="font-sans text-[11px] font-bold text-muted uppercase">
                          View
                        </b>
                      )}
                    </NavLink>
                  ))}
                  {!tables.loading && filteredTables.length === 0 && (
                    <div className="col-span-full py-3 text-center text-xs text-muted italic">
                      No tables match
                    </div>
                  )}
                </div>
              </m.div>
            ) : null}
          </>
        )}
      </AnimatePresence>

      {/* ─── MOBILE BOTTOM NAVIGATION BAR (< 901px) ─── */}
      {!isAdminRoute && databaseId ? (
        <nav className="fixed inset-x-0 bottom-0 z-30 flex min-h-14 items-center justify-around border-t border-line/80 bg-white/95 px-2 pt-1 pb-[max(0.25rem,env(safe-area-inset-bottom))] backdrop-blur-md min-[901px]:hidden shadow-[0_-2px_10px_rgba(0,0,0,0.04)]">
          <NavLink
            to={`/databases/${databaseId}/${schema}/query`}
            className={({ isActive }) =>
              cn(
                'flex min-h-11 min-w-14 flex-col items-center justify-center gap-0.5 rounded-lg py-1 px-2.5 text-[11px] font-semibold transition-colors no-underline',
                isActive
                  ? 'text-violet-fg font-bold'
                  : 'text-muted hover:text-ink',
              )
            }
          >
            <Icon name="code" className="size-4.5" />
            <span>Editor</span>
          </NavLink>

          <NavLink
            to={`/databases/${databaseId}/${schema}/schema`}
            className={({ isActive }) =>
              cn(
                'flex min-h-11 min-w-14 flex-col items-center justify-center gap-0.5 rounded-lg py-1 px-2.5 text-[11px] font-semibold transition-colors no-underline',
                isActive
                  ? 'text-violet-fg font-bold'
                  : 'text-muted hover:text-ink',
              )
            }
          >
            <Icon name="schema" className="size-4.5" />
            <span>Schema</span>
          </NavLink>

          <NavLink
            to={`/databases/${databaseId}/migrations`}
            className={({ isActive }) =>
              cn(
                'relative flex min-h-11 min-w-14 flex-col items-center justify-center gap-0.5 rounded-lg py-1 px-2.5 text-[11px] font-semibold transition-colors no-underline',
                isActive
                  ? 'text-violet-fg font-bold'
                  : 'text-muted hover:text-ink',
              )
            }
          >
            <Icon name="migration" className="size-4.5" />
            <span>Migrations</span>
            {dbPendingCount > 0 && (
              <span className="absolute top-1.5 right-2.5 size-2 rounded-full bg-warning ring-2 ring-white" />
            )}
          </NavLink>

          <button
            type="button"
            aria-expanded={mobileSheet === 'tables'}
            onClick={() =>
              setMobileSheet((prev) => (prev === 'tables' ? 'none' : 'tables'))
            }
            className={cn(
              'flex min-h-11 min-w-14 flex-col items-center justify-center gap-0.5 rounded-lg py-1 px-2.5 text-[11px] font-semibold transition-colors cursor-pointer',
              isTableRoute || mobileSheet === 'tables'
                ? 'text-violet-fg font-bold'
                : 'text-muted hover:text-ink',
            )}
          >
            <Icon name="table" className="size-4.5" />
            <span>Tables</span>
          </button>
        </nav>
      ) : isAdminRoute ? (
        <nav className="fixed inset-x-0 bottom-0 z-30 flex min-h-14 items-center justify-around border-t border-line/80 bg-white/95 px-2 pt-1 pb-[max(0.25rem,env(safe-area-inset-bottom))] backdrop-blur-md min-[901px]:hidden shadow-[0_-2px_10px_rgba(0,0,0,0.04)]">
          {adminNav.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) =>
                cn(
                  'flex min-h-11 min-w-14 flex-col items-center justify-center gap-0.5 rounded-lg py-1 px-2.5 text-[11px] font-semibold transition-colors no-underline',
                  isActive
                    ? 'text-violet-fg font-bold'
                    : 'text-muted hover:text-ink',
                )
              }
            >
              <Icon name={item.icon} className="size-4.5" />
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>
      ) : null}

      {/* ─── MAIN CONTENT VIEWPORT ─── */}
      <main className="min-w-0 px-3.5 pt-4 pb-[calc(5.25rem+env(safe-area-inset-bottom))] sm:px-6 sm:pt-5 min-[901px]:pb-10 [font-variant-numeric:tabular-nums] [&_h1]:text-balance [&_h2]:text-balance [&_h3]:text-balance [&_p]:text-pretty max-[900px]:w-full">
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
    <header className="mb-5 flex min-w-0 flex-col gap-3.5 border-b border-line/60 pb-4 min-[901px]:mb-4 min-[901px]:min-h-[58px] min-[901px]:flex-row min-[901px]:items-center min-[901px]:justify-between min-[901px]:gap-4 min-[901px]:pb-3">
      <div className="flex min-w-0 flex-col gap-2 min-[901px]:flex-row min-[901px]:flex-wrap min-[901px]:items-center min-[901px]:gap-3">
        <div className="flex min-w-0 items-center gap-2">
          {backTo && (
            <Link
              to={backTo}
              className="grid size-9 shrink-0 place-items-center rounded-xl border border-line/80 bg-white/90 text-muted shadow-2xs transition-colors hover:border-line-strong hover:text-ink"
              aria-label="Go back"
            >
              <Icon name="chevron-left" className="size-4" />
            </Link>
          )}
          <span className="inline-flex min-h-7 min-w-0 max-w-full items-center gap-1.5 rounded-full border border-line/70 bg-white/75 px-2.5 font-mono text-xs text-muted shadow-2xs min-[901px]:min-h-[30px] min-[901px]:gap-2 min-[901px]:px-3">
            <i className="size-1.5 shrink-0 rounded-full bg-success ring-[3px] ring-success-soft min-[901px]:size-2" />
            <span className="min-w-0 truncate">{path}</span>
          </span>
        </div>

        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <h1 className="m-0 min-w-0 break-words text-xl leading-tight font-bold tracking-tight text-ink">
            {title}
          </h1>
          {status && (
            <span className="max-w-full truncate rounded-full border border-line/70 bg-white/80 px-2.5 py-0.5 text-[11px] font-semibold text-muted shadow-2xs">
              {status}
            </span>
          )}
        </div>
      </div>

      {actions && (
        <div className="flex min-w-0 w-full flex-wrap items-center gap-2.5 min-[901px]:ml-auto min-[901px]:w-auto min-[901px]:justify-end">
          {actions}
        </div>
      )}
    </header>
  )
}
