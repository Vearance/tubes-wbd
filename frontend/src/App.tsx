import { useEffect, type ReactNode } from 'react'
import { LazyMotion, MotionConfig } from 'motion/react'
import { BrowserRouter, Navigate, Route, Routes, useLocation, useParams } from 'react-router'
import { session } from './api'
import { AppShell } from './components/AppShell'
import { LoginPage } from './pages/LoginPage'
import { DatabaseListPage, TablePage } from './pages/DatabasePages'
import { EditPage, MigrationPage, QueryPage, SchemaPage } from './pages/ToolPages'
import { AdminDatabasesPage, AdminGroupsPage, AdminLogsPage, AdminUsersPage } from './pages/AdminPages'
import './App.css'

const loadMotionFeatures=()=>import('./motionFeatures').then(module=>module.default)

function RequireAuth({children,admin=false,regular=false}:{children:ReactNode;admin?:boolean;regular?:boolean}){const user=session.user();if(!user)return <Navigate to="/login" replace/>;if(admin&&!user.is_admin)return <Navigate to="/databases" replace/>;if(regular&&user.is_admin)return <Navigate to="/admin/users" replace/>;return children}
function ShellRoute(){const params=useParams();return <AppShell databaseId={params.databaseId} schema={params.schema}/>}
function Home(){const user=session.user();return <Navigate to={user?.is_admin?'/admin/users':user?'/databases':'/login'} replace/>}
function RouteMetadata(){
  const {pathname}=useLocation()
  const section=pathname==='/login'?'Sign in':pathname.startsWith('/admin/users')?'Admin users':pathname.startsWith('/admin/groups')?'Admin groups':pathname.startsWith('/admin/databases')?'Admin databases':pathname.startsWith('/admin/logs')?'Audit logs':pathname.endsWith('/edit')?'Table changes':pathname.includes('/query')?'SQL editor':pathname.includes('/schema')?'Schema':pathname.includes('/migrations')?'Migrations':pathname==='/databases'?'Databases':'Table workspace'
  useEffect(()=>{document.title=`${section} — BScheme`},[section])
  return null
}
export default function App(){return <MotionConfig reducedMotion="user"><LazyMotion features={loadMotionFeatures} strict><BrowserRouter><RouteMetadata/><Routes><Route path="/login" element={<LoginPage/>}/><Route element={<RequireAuth regular><ShellRoute/></RequireAuth>}><Route path="/databases" element={<DatabaseListPage/>}/><Route path="/databases/:databaseId/:schema/:table" element={<TablePage/>}/><Route path="/databases/:databaseId/:schema/:table/edit" element={<EditPage/>}/><Route path="/databases/:databaseId/:schema/query" element={<QueryPage/>}/><Route path="/databases/:databaseId/:schema/schema" element={<SchemaPage/>}/><Route path="/databases/:databaseId/migrations" element={<MigrationPage/>}/></Route><Route element={<RequireAuth admin><ShellRoute/></RequireAuth>}><Route path="/admin/users" element={<AdminUsersPage/>}/><Route path="/admin/users/:userId" element={<AdminUsersPage/>}/><Route path="/admin/groups" element={<AdminGroupsPage/>}/><Route path="/admin/groups/:groupId" element={<AdminGroupsPage/>}/><Route path="/admin/databases" element={<AdminDatabasesPage/>}/><Route path="/admin/databases/:databaseId" element={<AdminDatabasesPage/>}/><Route path="/admin/logs" element={<AdminLogsPage/>}/></Route><Route path="*" element={<Home/>}/></Routes></BrowserRouter></LazyMotion></MotionConfig>}
