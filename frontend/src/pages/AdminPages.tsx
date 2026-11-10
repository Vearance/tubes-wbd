import { useId, useState, type ReactNode } from 'react'
import { AnimatePresence, LayoutGroup, m, useReducedMotion } from 'motion/react'
import { Link } from 'react-router'
import { api, type AdminUser, type DatabasePermission, type Group, type ManagedDatabase, type ManagedDatabaseInput, type PermissionLevel } from '../api'
import { UtilityHeader } from '../components/AppShell'
import { Icon } from '../components/Icon'
import { Button, ConfirmAction, EmptyState, ErrorNotice, Loading, Search, Status } from '../components/ui'
import { useResource } from '../hooks'


export function AdminUsersPage(){
  const users=useResource(()=>api.adminUsers(),[])
  const groups=useResource(()=>api.adminGroups(),[])
  const databases=useResource(()=>api.adminDatabases(),[])
  const [search,setSearch]=useState('')
  const [selected,setSelected]=useState<AdminUser|null>(null)
  if(users.loading||groups.loading||databases.loading)return <Loading page/>
  const shown=users.data?.filter(user=>user.username.toLowerCase().includes(search.toLowerCase()))??[]
  async function toggle(user:AdminUser){const updated=await api.updateUser(user.id,!user.disabled);setSelected(updated);await users.reload()}
  return <>
    <UtilityHeader path="Administration" title="Users" status={`${users.data?.length??0} users`}/>
    {(users.error||groups.error||databases.error)&&<ErrorNotice message={users.error||groups.error||databases.error}/>} 
    <div className="user-admin-layout">
      <section className="surface user-picker-list"><header><Search value={search} onChange={setSearch} placeholder="Search users"/></header>{shown.map(user=><button className={`user-account-select ${selected?.id===user.id?'selected':''}`} key={user.id} onClick={()=>setSelected(user)}><span className="avatar">{user.username.slice(0,2).toUpperCase()}</span><span><b>{user.username}</b><code>#{user.id}</code></span><Icon name="chevron"/></button>)}</section>
      <section className="surface user-detail"><AnimatePresence initial={false} mode="popLayout">{selected?<m.div className="admin-detail-panel" key={`user-${selected.id}`} initial={{opacity:0}} animate={{opacity:1}} exit={{opacity:0,transition:{duration:.12,ease:'easeOut'}}} transition={{duration:.15,ease:'easeOut'}}><UserAccessDetail user={selected} groups={groups.data??[]} databases={databases.data??[]} onToggle={()=>toggle(selected)}/></m.div>:<m.div className="admin-detail-panel" key="user-empty" initial={{opacity:0}} animate={{opacity:1}} exit={{opacity:0,transition:{duration:.14,ease:'easeOut'}}}><EmptyState icon="users" title="Select a user" action={shown[0]?<Button onClick={()=>setSelected(shown[0])}>View first user</Button>:undefined}>Choose an account to inspect status and access.</EmptyState></m.div>}</AnimatePresence></section>
    </div>
  </>
}

type UserGroupAccess = { group: Group; permissions: DatabasePermission[] }

function PickerAction({open,label,onClick,variant=''}:{open:boolean;label:string;onClick:()=>void;variant?:string}){
  const reduceMotion=useReducedMotion()
  return <Button variant={variant} onClick={onClick}><span className="picker-action-icon"><AnimatePresence initial={false} mode="sync"><m.span key={open?'close':'plus'} initial={reduceMotion?false:{opacity:0,scale:.95}} animate={{opacity:1,scale:1}} exit={reduceMotion?undefined:{opacity:0,scale:.95,transition:{duration:.1,ease:'easeOut'}}} transition={{duration:.13,ease:'easeOut'}}><Icon name={open?'close':'plus'}/></m.span></AnimatePresence></span>{label}</Button>
}

function PickerPopover({children}:{children:ReactNode}){
  const reduceMotion=useReducedMotion()
  return <m.div className="assignment-picker" initial={reduceMotion?false:{opacity:0,scale:.95,y:-4}} animate={{opacity:1,scale:1,y:0}} exit={reduceMotion?undefined:{opacity:0,scale:.95,y:-4,transition:{duration:.14,ease:'easeOut'}}} transition={{duration:.18,ease:'easeOut'}} style={{transformOrigin:'top right'}}>{children}</m.div>
}

function StaggerRow({index,className,children}:{index:number;className:string;children:ReactNode}){
  const reduceMotion=useReducedMotion()
  return <m.article className={className} initial={reduceMotion?false:{opacity:0,y:3}} animate={{opacity:1,y:0}} transition={{duration:.15,ease:'easeOut',delay:Math.min(index,4)*.03}}>{children}</m.article>
}

function UserAccessDetail({user,groups,databases,onToggle}:{user:AdminUser;groups:Group[];databases:ManagedDatabase[];onToggle:()=>Promise<void>}){
  const access=useResource(async()=>{
    if(user.is_admin)return []
    const memberships=await Promise.all(groups.map(async group=>{
      const members=await api.groupMembers(group.id)
      return members.some(member=>member.id===user.id)?{group,permissions:await api.groupDatabases(group.id)}:null
    }))
    return memberships.filter((entry):entry is UserGroupAccess=>entry!==null)
  },[user.id,user.is_admin,groups])
  const [groupPicker,setGroupPicker]=useState(false)
  const [membershipError,setMembershipError]=useState('')
  const [accountError,setAccountError]=useState('')
  const availableGroups=groups.filter(group=>!access.data?.some(entry=>entry.group.id===group.id))
  const databaseAccess=(access.data??[]).flatMap(entry=>entry.permissions.map(permission=>({group:entry.group,permission,database:databases.find(database=>database.id===permission.database_id)})))
  async function addMembership(groupId:number){setMembershipError('');setGroupPicker(false);try{await api.addGroupMember(groupId,user.id);await access.reload()}catch(e){setMembershipError(e instanceof Error?e.message:'Unable to add group membership');setGroupPicker(true)}}
  async function removeMembership(groupId:number){setMembershipError('');try{await api.removeGroupMember(groupId,user.id);await access.reload()}catch(e){setMembershipError(e instanceof Error?e.message:'Unable to remove group membership')}}
  async function toggleAccount(){setAccountError('');try{await onToggle()}catch(e){setAccountError(e instanceof Error?e.message:'Unable to update account')}}
  return <>
    <header><span className="avatar">{user.username.slice(0,2).toUpperCase()}</span><span><small>{user.is_admin?'Administrator':'Database user'}</small><h2>{user.username}</h2></span><Status tone={user.disabled?'red':'green'}>{user.disabled?'Disabled':'Active'}</Status></header>
    {!user.is_admin&&<>
      <section className="assignment-section"><div className="assignment-heading"><h3>Group membership</h3><PickerAction open={groupPicker} label="Add group" variant="user-section-action" onClick={()=>setGroupPicker(!groupPicker)}/></div>{(access.error||membershipError)&&<ErrorNotice message={access.error||membershipError}/>} {access.loading?<Loading/>:<div className="assigned-list">{access.data?.map((entry,index)=><StaggerRow index={index} className="assigned-row" key={entry.group.id}><span className="group-symbol violet"><Icon name="groups"/></span><span><b>{entry.group.name}</b><small>{entry.permissions.length} database{entry.permissions.length===1?'':'s'}</small></span><ConfirmAction trigger={<Icon name="close"/>} triggerClassName="remove-assignment" triggerAriaLabel={`Remove ${user.username} from ${entry.group.name}`} title={`Remove ${user.username} from ${entry.group.name}?`} description="The user will lose access inherited through this group." confirmLabel="Remove membership" onConfirm={()=>removeMembership(entry.group.id)}/></StaggerRow>)}</div>}{!access.loading&&!access.data?.length&&<p className="assignment-empty">No groups</p>}<AnimatePresence>{groupPicker&&<PickerPopover><div className="picker-options">{availableGroups.map(group=><button className="picker-option" key={group.id} onClick={()=>addMembership(group.id)}><span className="group-symbol violet"><Icon name="groups"/></span><span><b>{group.name}</b><small>Group #{group.id}</small></span><em>Add</em></button>)}{!availableGroups.length&&<p className="picker-empty">No groups available</p>}</div></PickerPopover>}</AnimatePresence></section>
      <section className="assignment-section"><div className="assignment-heading"><h3>Database access</h3></div><div className="assigned-list">{databaseAccess.map(({group,permission,database},index)=><StaggerRow index={index} className="assigned-row access-row" key={`${group.id}-${permission.database_id}`}><span className="database-glyph"><Icon name="database"/></span><span><b>{database?.name??`Database #${permission.database_id}`}</b><small>{group.name}</small></span><span className={`permission-badge ${permission.permission==='view_edit'?'editor':'viewer'}`}>{permission.permission==='view_edit'?'Editor':'Viewer'}</span></StaggerRow>)}</div>{!access.loading&&!databaseAccess.length&&<p className="assignment-empty">No database access</p>}</section>
    </>}
    <section className="account-section"><div className="assignment-heading"><h3>Account</h3>{user.disabled?<Button variant="dark user-section-action" onClick={toggleAccount}>Enable account</Button>:<ConfirmAction trigger="Disable account" triggerClassName="btn user-section-action" title={`Disable ${user.username}?`} description="This user will be signed out until the account is enabled again." confirmLabel="Disable account" onConfirm={toggleAccount}/>}</div>{accountError&&<ErrorNotice message={accountError}/>}</section>
  </>
}

export function AdminGroupsPage(){
  const groups=useResource(()=>api.adminGroups(),[])
  const users=useResource(()=>api.adminUsers(),[])
  const databases=useResource(()=>api.adminDatabases(),[])
  const [selected,setSelected]=useState<Group|null>(null)
  const [showCreate,setShowCreate]=useState(false)
  const [name,setName]=useState('')
  const [error,setError]=useState('')
  async function createGroup(event:React.FormEvent){event.preventDefault();setError('');try{const created=await api.createGroup(name);setName('');setShowCreate(false);await groups.reload();setSelected(created)}catch(e){setError(e instanceof Error?e.message:'Unable to create group')}}
  if(groups.loading||users.loading||databases.loading)return <Loading page/>
  return <>
    <UtilityHeader path="Administration" title="Groups" status={`${groups.data?.length??0} groups`} actions={<Button variant="dark" onClick={()=>setShowCreate(true)}><Icon name="plus"/>Add group</Button>}/>
    {(groups.error||users.error||databases.error||error)&&<ErrorNotice message={groups.error||users.error||databases.error||error}/>} 
    {showCreate&&<form className="surface create-group-form" onSubmit={createGroup}><label><span>Group name</span><input value={name} onChange={e=>setName(e.target.value)} required autoFocus placeholder="Customer editors"/></label><div><Button onClick={()=>setShowCreate(false)}>Cancel</Button><Button type="submit" variant="dark">Create group</Button></div></form>}
    <div className="group-admin-layout"><section className="surface group-list"><header><h2>Access groups</h2><span>{groups.data?.length??0} groups</span></header>{groups.data?.map(g=><button className={`access-group-row ${selected?.id===g.id?'selected':''}`} key={g.id} onClick={()=>setSelected(g)}><span className="group-symbol violet"><Icon name="groups"/></span><span><b>{g.name}</b><small>Group #{g.id}</small></span><em>Manage</em><Icon name="chevron"/></button>)}</section><section className="surface group-detail"><AnimatePresence initial={false} mode="popLayout">{selected?<m.div className="admin-detail-panel" key={`group-${selected.id}`} initial={{opacity:0}} animate={{opacity:1}} exit={{opacity:0,transition:{duration:.12,ease:'easeOut'}}} transition={{duration:.15,ease:'easeOut'}}><GroupDetail group={selected} users={users.data??[]} databases={databases.data??[]}/></m.div>:<m.div className="admin-detail-panel" key="group-empty" initial={{opacity:0}} animate={{opacity:1}} exit={{opacity:0,transition:{duration:.14,ease:'easeOut'}}}><EmptyState icon="groups" title="Select a group" action={groups.data?.[0]?<Button onClick={()=>setSelected(groups.data![0])}>View first group</Button>:undefined}>Choose a group to manage members and database access.</EmptyState></m.div>}</AnimatePresence></section></div>
  </>
}

function GroupDetail({group,users,databases}:{group:Group;users:AdminUser[];databases:ManagedDatabase[]}){
  const members=useResource(()=>api.groupMembers(group.id),[group.id])
  const permissions=useResource(()=>api.groupDatabases(group.id),[group.id])
  const [memberPicker,setMemberPicker]=useState(false)
  const [databasePicker,setDatabasePicker]=useState(false)
  const [memberSearch,setMemberSearch]=useState('')
  const [databaseSearch,setDatabaseSearch]=useState('')
  const [memberError,setMemberError]=useState('')
  const [databaseError,setDatabaseError]=useState('')
  const availableMembers=users.filter(user=>!user.is_admin&&!members.data?.some(member=>member.id===user.id)&&user.username.toLowerCase().includes(memberSearch.toLowerCase()))
  const availableDatabases=databases.filter(database=>!permissions.data?.some(item=>item.database_id===database.id)&&database.name.toLowerCase().includes(databaseSearch.toLowerCase()))
  async function addMember(userId:number){setMemberError('');setMemberPicker(false);try{await api.addGroupMember(group.id,userId);await members.reload()}catch(e){setMemberError(e instanceof Error?e.message:'Unable to add member');setMemberPicker(true)}}
  async function removeMember(userId:number){setMemberError('');try{await api.removeGroupMember(group.id,userId);await members.reload()}catch(e){setMemberError(e instanceof Error?e.message:'Unable to remove member')}}
  async function setDatabasePermission(databaseId:number,permission:PermissionLevel,isNew=false){setDatabaseError('');if(isNew)setDatabasePicker(false);try{await api.setGroupDatabase(group.id,databaseId,permission);await permissions.reload();return true}catch(e){setDatabaseError(e instanceof Error?e.message:'Unable to update database access');if(isNew)setDatabasePicker(true);return false}}
  async function removeDatabase(databaseId:number){setDatabaseError('');try{await api.removeGroupDatabase(group.id,databaseId);await permissions.reload()}catch(e){setDatabaseError(e instanceof Error?e.message:'Unable to remove database')}}
  return <>
    <header><div><span>Group</span><h2>{group.name}</h2></div></header>
    <section className="assignment-section"><div className="assignment-heading"><h3>Members</h3><PickerAction open={memberPicker} label="Add member" onClick={()=>setMemberPicker(!memberPicker)}/></div>{(members.error||memberError)&&<ErrorNotice message={members.error||memberError}/>}<div className="assigned-list">{members.loading?<Loading/>:members.data?.map((member,index)=><StaggerRow index={index} className="assigned-row" key={member.id}><span className="avatar">{member.username.slice(0,2).toUpperCase()}</span><span><b>{member.username}</b><small>User #{member.id}</small></span><ConfirmAction trigger={<Icon name="close"/>} triggerClassName="remove-assignment" triggerAriaLabel={`Remove ${member.username}`} title={`Remove ${member.username} from ${group.name}?`} description="The user will lose access inherited through this group." confirmLabel="Remove member" onConfirm={()=>removeMember(member.id)}/></StaggerRow>)}</div>{!members.loading&&!members.data?.length&&<p className="assignment-empty">No members</p>}<AnimatePresence>{memberPicker&&<PickerPopover><Search value={memberSearch} onChange={setMemberSearch} placeholder="Search users"/><div className="picker-options">{availableMembers.map(user=><button className="picker-option" key={user.id} onClick={()=>addMember(user.id)}><span className="avatar">{user.username.slice(0,2).toUpperCase()}</span><span><b>{user.username}</b><small>User #{user.id}</small></span><em>Add</em></button>)}{!availableMembers.length&&<p className="picker-empty">No users available</p>}</div></PickerPopover>}</AnimatePresence></section>
    <section className="assignment-section"><div className="assignment-heading"><h3>Database access</h3><PickerAction open={databasePicker} label="Add database" onClick={()=>setDatabasePicker(!databasePicker)}/></div>{(permissions.error||databaseError)&&<ErrorNotice message={permissions.error||databaseError}/>}<div className="assigned-list">{permissions.loading?<Loading/>:permissions.data?.map((item,index)=>{const database=databases.find(entry=>entry.id===item.database_id);return <StaggerRow index={index} className="assigned-row database-permission-row" key={item.database_id}><span className="database-glyph"><Icon name="database"/></span><span><b>{database?.name??`Database #${item.database_id}`}</b><small>{database?`${database.host}:${database.port}`:`#${item.database_id}`}</small></span><PermissionChoice value={item.permission} onChange={permission=>setDatabasePermission(item.database_id,permission)}/><ConfirmAction trigger={<Icon name="close"/>} triggerClassName="remove-assignment" triggerAriaLabel={`Remove ${database?.name??'database'}`} title={`Remove ${database?.name??'this database'}?`} description={`Members of ${group.name} will lose access to this database.`} confirmLabel="Remove database" onConfirm={()=>removeDatabase(item.database_id)}/></StaggerRow>})}</div>{!permissions.loading&&!permissions.data?.length&&<p className="assignment-empty">No database access</p>}<AnimatePresence>{databasePicker&&<PickerPopover><Search value={databaseSearch} onChange={setDatabaseSearch} placeholder="Search databases"/><div className="picker-options">{availableDatabases.map((database,index)=><StaggerRow index={index} className="permission-picker-option" key={database.id}><span className="database-glyph"><Icon name="database"/></span><span><b>{database.name}</b><small>{database.host}:{database.port}</small></span><PermissionChoice onChange={permission=>setDatabasePermission(database.id,permission,true)}/></StaggerRow>)}{!availableDatabases.length&&<p className="picker-empty">No databases available</p>}</div></PickerPopover>}</AnimatePresence></section>
  </>
}

function PermissionChoice({value,onChange}:{value?:PermissionLevel;onChange:(permission:PermissionLevel)=>Promise<boolean>}){
  const groupId=useId()
  const reduceMotion=useReducedMotion()
  const [optimisticValue,setOptimisticValue]=useState(value)
  const [busy,setBusy]=useState(false)
  async function choose(permission:PermissionLevel){if(busy||permission===optimisticValue)return;const previous=optimisticValue;setOptimisticValue(permission);setBusy(true);if(!await onChange(permission))setOptimisticValue(previous);setBusy(false)}
  return <LayoutGroup id={groupId}><div className="permission-choice" aria-label="Database permission"><button disabled={busy} className={`viewer ${optimisticValue==='view_only'?'active':''}`} onClick={()=>void choose('view_only')}>{optimisticValue==='view_only'&&<m.span className="permission-knob" layoutId="permission-knob" initial={false} transition={{layout:{duration:.16,ease:'easeInOut'}}}><m.span className="permission-knob-pulse" animate={reduceMotion?undefined:{scaleX:[1,1.12,1]}} transition={{duration:.16,ease:'easeInOut'}}/></m.span>}<span className="permission-label">Viewer</span></button><button disabled={busy} className={`editor ${optimisticValue==='view_edit'?'active':''}`} onClick={()=>void choose('view_edit')}>{optimisticValue==='view_edit'&&<m.span className="permission-knob" layoutId="permission-knob" initial={false} transition={{layout:{duration:.16,ease:'easeInOut'}}}><m.span className="permission-knob-pulse" animate={reduceMotion?undefined:{scaleX:[1,1.12,1]}} transition={{duration:.16,ease:'easeInOut'}}/></m.span>}<span className="permission-label">Editor</span></button></div></LayoutGroup>
}

export function AdminDatabasesPage(){
  const databases=useResource(()=>api.adminDatabases(),[])
  const [selected,setSelected]=useState<ManagedDatabase|null>(null)
  const [showForm,setShowForm]=useState(false)
  const [message,setMessage]=useState('')
  async function test(id:number){setMessage('Testing connection…');try{await api.testStoredDatabase(id);setMessage('Connection is ready.')}catch(e){setMessage(e instanceof Error?e.message:'Connection failed')}}
  if(databases.loading)return <Loading page/>
  return <>
    <UtilityHeader path="Administration" title="Databases" status={`${databases.data?.length??0} connections`} actions={databases.data?.length&&!showForm?<Button variant="dark" onClick={()=>setShowForm(true)}><Icon name="plus"/>Add database</Button>:undefined}/>
    {databases.error&&<ErrorNotice message={databases.error}/>} 
    {showForm&&<ConnectionForm onCancel={()=>setShowForm(false)} onCreated={async()=>{setShowForm(false);await databases.reload()}}/>}
    <div className="database-admin-grid">{databases.data?.map(db=><button className={`database-admin-card ${selected?.id===db.id?'selected':''}`} onClick={()=>setSelected(db)} key={db.id}><span className="database-glyph"><Icon name="database"/></span><span><strong>{db.name}</strong><small>{db.username}@{db.host}:{db.port}/{db.db_name}</small></span><Status tone="green">{db.ssl_mode}</Status></button>)}</div>
    {!databases.data?.length&&!showForm&&<section className="surface database-empty"><EmptyState icon="database" title="No databases" action={<Button variant="dark" onClick={()=>setShowForm(true)}><Icon name="plus"/>Add database</Button>}>Register a PostgreSQL database for group access.</EmptyState></section>}
    {selected&&!showForm&&<section className="surface connection-detail"><div><p className="kicker">Connection</p><h2>{selected.name}</h2><p>{selected.host}:{selected.port} · {selected.db_name}</p></div><Button onClick={()=>test(selected.id)}><Icon name="refresh"/>Test connection</Button>{message&&<p className="connection-message" role="status">{message}</p>}</section>}
  </>
}

function ConnectionForm({onCancel,onCreated}:{onCancel:()=>void;onCreated:()=>Promise<void>}){
  const [value,setValue]=useState<ManagedDatabaseInput>({name:'',host:'',port:5432,username:'',password:'',db_name:'',ssl_mode:'disable'})
  const [message,setMessage]=useState('')
  const [busy,setBusy]=useState(false)
  async function test(){setBusy(true);setMessage('Testing connection…');try{await api.testDatabase(value);setMessage('Connection succeeded. You can save it now.')}catch(e){setMessage(e instanceof Error?e.message:'Connection failed')}finally{setBusy(false)}}
  async function save(event:React.FormEvent){event.preventDefault();setBusy(true);setMessage('');try{await api.createDatabase(value);await onCreated()}catch(e){setMessage(e instanceof Error?e.message:'Unable to save connection');setBusy(false)}}
  return <form className="surface connection-form" onSubmit={save}>
    <header><h2>Add PostgreSQL database</h2></header>
    <div className="connection-fields">
      <label><span>Display name</span><input value={value.name} onChange={e=>setValue({...value,name:e.target.value})} required placeholder="Customer DB"/></label>
      <label><span>Host</span><input value={value.host} onChange={e=>setValue({...value,host:e.target.value})} required placeholder="test-db"/></label>
      <label><span>Port</span><input type="number" min="1" max="65535" value={value.port} onChange={e=>setValue({...value,port:Number(e.target.value)})} required/></label>
      <label><span>Database name</span><input value={value.db_name} onChange={e=>setValue({...value,db_name:e.target.value})} required placeholder="targetdb"/></label>
      <label><span>Username</span><input value={value.username} onChange={e=>setValue({...value,username:e.target.value})} required autoComplete="username" placeholder="target"/></label>
      <label><span>Password</span><input type="password" value={value.password} onChange={e=>setValue({...value,password:e.target.value})} required autoComplete="new-password"/></label>
      <label><span>SSL mode</span><select value={value.ssl_mode} onChange={e=>setValue({...value,ssl_mode:e.target.value})}><option value="disable">Disable</option><option value="prefer">Prefer</option><option value="require">Require</option><option value="verify-ca">Verify CA</option><option value="verify-full">Verify full</option></select></label>
    </div>
    {message&&<p className="form-feedback" role="status">{message}</p>}
    <footer><Button onClick={onCancel}>Cancel</Button><Button disabled={busy} onClick={test}><Icon name="refresh"/>Test connection</Button><Button type="submit" variant="dark" disabled={busy}><Icon name="save"/>Save database</Button></footer>
  </form>
}

export function AdminLogsPage(){return <><UtilityHeader path="Administration" title="Logs"/><section className="access-surface audit-empty"><EmptyState icon="logs" title="Audit logs are not available" action={<Link className="btn" to="/admin/users">Manage users</Link>}>The current OpenAPI contract does not expose an administrator audit-log endpoint. This screen is ready to consume one when it is added.</EmptyState></section></>}
