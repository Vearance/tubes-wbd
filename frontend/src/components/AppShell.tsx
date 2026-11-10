import type { ReactNode } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { api, session } from '../api'
import { useResource } from '../hooks'
import { Icon } from './Icon'

const userNav = [
  ['/databases','grid','Databases'],
] as const
const databaseNav = (id: string, schema='public') => [
  [`/databases/${id}/${schema}/query`,'code','SQL editor'],
  [`/databases/${id}/${schema}/schema`,'schema','Schema'],
  [`/databases/${id}/migrations`,'migration','Migrations'],
] as const
const adminNav = [['/admin/users','users','Users'],['/admin/groups','groups','Groups'],['/admin/databases','database','Databases'],['/admin/logs','logs','Logs']] as const

export function AppShell({databaseId, schema}: {databaseId?: string; schema?: string}) {
  const user = session.user()!
  const navigate = useNavigate()
  const location = useLocation()
  const isAdminRoute = location.pathname.startsWith('/admin')
  const databases = useResource(() => user.is_admin ? Promise.resolve([]) : api.databases(), [user.is_admin])
  const tables = useResource(() => !user.is_admin&&databaseId ? api.tables(Number(databaseId),schema??'public') : Promise.resolve([]), [user.is_admin,databaseId,schema])
  const activeDatabase = databases.data?.find(database => String(database.id) === databaseId)
  const nav = isAdminRoute ? adminNav : databaseId ? databaseNav(databaseId, schema) : userNav
  async function logout() { try { await api.logout() } finally { session.clear(); navigate('/login', {replace:true}) } }
  return <div className="app-frame">
    <aside className={`side-nav ${isAdminRoute?'admin-side-nav':''}`}>
      <NavLink className="brand" to={user.is_admin ? '/admin/users' : '/databases'}><span className="brand-mark"><img src="/bibi.png" alt=""/></span>BScheme</NavLink>
      {isAdminRoute?<div className="database-picker"><span className="database-glyph"><Icon name="users"/></span><span><strong>Administration</strong><small>Access and connections</small></span></div>:<button className="database-picker" type="button" aria-label="Choose database" title="Choose database" onClick={()=>navigate('/databases')}><span className="database-glyph"><Icon name="database"/></span><span><strong>{activeDatabase?.name??(databaseId?`Database ${databaseId}`:databases.data?.length?'Select database':'No databases')}</strong>{databaseId&&<small>Connected · PostgreSQL</small>}</span><Icon name="chevron"/></button>}
      {!isAdminRoute&&databaseId&&<><p className="nav-label tables-label">Tables</p><nav className="table-nav">{tables.data?.map(entry=><NavLink key={`${entry.schema}.${entry.name}`} className={({isActive})=>`tree-table ${isActive?'selected':''}`} to={`/databases/${databaseId}/${entry.schema}/${entry.name}`}><span/>{entry.name}<b>{entry.kind==='view'?'View':'Table'}</b></NavLink>)}</nav></>}
      <p className={`nav-label workspace-label ${isAdminRoute?'admin-label':!isAdminRoute&&databaseId?'spaced':''}`}>{isAdminRoute?'Manage':'Workspace'}</p>
      <nav className="primary-nav">{nav.map(([to,icon,label])=><NavLink key={to} className={({isActive})=>`side-item ${isActive?'active':''}`} to={to}><Icon name={icon}/>{label}</NavLink>)}</nav>

      <div className="side-user"><span className="avatar">{user.username.slice(0,2).toUpperCase()}</span><span><strong>{user.username}</strong><small>{user.is_admin?'Administrator':'Database user'}</small></span><button aria-label="Log out" onClick={logout}><Icon name="logout"/></button></div>
    </aside>
    <main className="main-view"><Outlet context={{databases:databases.data??[]}}/></main>
  </div>
}


export function UtilityHeader({path,title,status,actions}: {path:string; title:string; status?:string; actions?:ReactNode}) {
  return <header className="utility-bar"><div className="utility-identity"><span className="utility-path"><i className="live-dot"/>{path}</span><h1>{title}</h1>{status&&<span className="utility-status">{status}</span>}</div>{actions&&<div className="header-actions">{actions}</div>}</header>
}
