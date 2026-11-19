import { Suspense, lazy, useEffect, type ReactNode } from 'react'
import { LazyMotion, MotionConfig } from 'motion/react'
import {
  BrowserRouter,
  Navigate,
  Route,
  Routes,
  useLocation,
  useParams,
} from 'react-router'
import { session } from './api'
import { AppShell } from './components/AppShell'
import { Loading } from './components/ui'

// Lazy-loaded routes for optimal bundle chunking and performance
const LoginPage = lazy(() =>
  import('./pages/LoginPage').then((m) => ({ default: m.LoginPage })),
)
const DatabaseListPage = lazy(() =>
  import('./pages/database/DatabaseListPage').then((m) => ({
    default: m.DatabaseListPage,
  })),
)
const TablePage = lazy(() =>
  import('./pages/database/TablePage').then((m) => ({
    default: m.TablePage,
  })),
)
const EditPage = lazy(() =>
  import('./pages/tools/EditPage').then((m) => ({ default: m.EditPage })),
)
const QueryPage = lazy(() =>
  import('./pages/tools/QueryPage').then((m) => ({ default: m.QueryPage })),
)
const SchemaPage = lazy(() =>
  import('./pages/tools/SchemaPage').then((m) => ({ default: m.SchemaPage })),
)
const MigrationPage = lazy(() =>
  import('./pages/tools/MigrationPage').then((m) => ({
    default: m.MigrationPage,
  })),
)

const AdminUsersPage = lazy(() =>
  import('./pages/admin/AdminUsersPage').then((m) => ({
    default: m.AdminUsersPage,
  })),
)
const AdminGroupsPage = lazy(() =>
  import('./pages/admin/AdminGroupsPage').then((m) => ({
    default: m.AdminGroupsPage,
  })),
)
const AdminDatabasesPage = lazy(() =>
  import('./pages/admin/AdminDatabasesPage').then((m) => ({
    default: m.AdminDatabasesPage,
  })),
)
const AdminLogsPage = lazy(() =>
  import('./pages/admin/AdminLogsPage').then((m) => ({
    default: m.AdminLogsPage,
  })),
)

const loadMotionFeatures = () =>
  import('./motionFeatures').then((module) => module.default)

type RequireAuthProps = {
  children: ReactNode
  admin?: boolean
  regular?: boolean
}

function RequireAuth({
  children,
  admin = false,
  regular = false,
}: RequireAuthProps) {
  const user = session.user()

  if (!user) {
    return <Navigate to="/login" replace />
  }

  if (admin && !user.is_admin) {
    return <Navigate to="/databases" replace />
  }

  if (regular && user.is_admin) {
    return <Navigate to="/admin/users" replace />
  }

  return children
}

function ShellRoute() {
  const { databaseId, schema } = useParams()
  return <AppShell databaseId={databaseId} schema={schema} />
}

function Home() {
  const user = session.user()

  if (!user) {
    return <Navigate to="/login" replace />
  }

  return <Navigate to={user.is_admin ? '/admin/users' : '/databases'} replace />
}

function getPageSection(pathname: string) {
  if (pathname === '/login') return 'Sign in'
  if (pathname.startsWith('/admin/users')) return 'Admin users'
  if (pathname.startsWith('/admin/groups')) return 'Admin groups'
  if (pathname.startsWith('/admin/databases')) return 'Admin databases'
  if (pathname.startsWith('/admin/logs')) return 'Audit logs'
  if (pathname.endsWith('/edit')) return 'Table changes'
  if (pathname.includes('/query')) return 'SQL editor'
  if (pathname.includes('/schema')) return 'Schema'
  if (pathname.includes('/migrations')) return 'Migrations'
  if (pathname === '/databases') return 'Databases'
  return 'Table workspace'
}

function RouteMetadata() {
  const { pathname } = useLocation()
  const section = getPageSection(pathname)

  useEffect(() => {
    document.title = `${section} — BScheme`
  }, [section])

  return null
}

export default function App() {
  return (
    <MotionConfig reducedMotion="user">
      <LazyMotion features={loadMotionFeatures} strict>
        <BrowserRouter>
          <RouteMetadata />
          <Suspense fallback={<Loading page />}>
            <Routes>
              <Route path="/login" element={<LoginPage />} />

              <Route
                element={
                  <RequireAuth regular>
                    <ShellRoute />
                  </RequireAuth>
                }
              >
                <Route path="/databases" element={<DatabaseListPage />} />
                <Route
                  path="/databases/:databaseId/:schema/:table"
                  element={<TablePage />}
                />
                <Route
                  path="/databases/:databaseId/:schema/:table/edit"
                  element={<EditPage />}
                />
                <Route
                  path="/databases/:databaseId/:schema/query"
                  element={<QueryPage />}
                />
                <Route
                  path="/databases/:databaseId/:schema/schema"
                  element={<SchemaPage />}
                />
                <Route
                  path="/databases/:databaseId/migrations"
                  element={<MigrationPage />}
                />
              </Route>

              <Route
                element={
                  <RequireAuth admin>
                    <ShellRoute />
                  </RequireAuth>
                }
              >
                <Route path="/admin/users" element={<AdminUsersPage />} />
                <Route path="/admin/users/:userId" element={<AdminUsersPage />} />
                <Route path="/admin/groups" element={<AdminGroupsPage />} />
                <Route path="/admin/groups/:groupId" element={<AdminGroupsPage />} />
                <Route path="/admin/databases" element={<AdminDatabasesPage />} />
                <Route
                  path="/admin/databases/:databaseId"
                  element={<AdminDatabasesPage />}
                />
                <Route path="/admin/logs" element={<AdminLogsPage />} />
              </Route>

              <Route path="*" element={<Home />} />
            </Routes>
          </Suspense>
        </BrowserRouter>
      </LazyMotion>
    </MotionConfig>
  )
}
