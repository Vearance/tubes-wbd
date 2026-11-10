import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router'
import { api, session } from '../api'
import { Icon } from '../components/Icon'
import { ErrorNotice } from '../components/ui'
export function LoginPage(){
  const navigate=useNavigate(), existing=session.user()
  const [username,setUsername]=useState(''),[password,setPassword]=useState(''),[show,setShow]=useState(false),[error,setError]=useState(''),[busy,setBusy]=useState(false)
  if(existing) return <Navigate to={existing.is_admin?'/admin/users':'/databases'} replace/>
  async function submit(e:React.FormEvent){e.preventDefault();setBusy(true);setError('');try{const result=await api.login(username,password);session.save(result);navigate(result.user.is_admin?'/admin/users':'/databases',{replace:true})}catch(err){setError(err instanceof Error?err.message:'Unable to sign in')}finally{setBusy(false)}}
  return <main className="login-page"><section className="login-brand-panel"><a className="brand" href="/login"><span className="brand-mark"><img src="/bibi.png" alt=""/></span>BScheme</a><div className="login-copy"><h1>Browse tables.<br/>Run SQL.<br/>Review changes.</h1><p>One shared workspace for your team’s data, schema, and migration history.</p></div><div className="login-bibi-stage"><span className="bibi-name">Bibi</span><img src="/bibi.png" alt="Bibi, the BScheme mascot"/></div></section><section className="login-form-panel"><form className="login-card" onSubmit={submit}><h2>Welcome Users!</h2><p>Please enter your details.</p>{error&&<ErrorNotice message={error}/>}<label><span>Username <b>Mandatory</b></span><input type="text" autoComplete="username" value={username} onChange={e=>setUsername(e.target.value)} required/></label><label><span>Password <b>Mandatory</b></span><span className="password-field"><input type={show?'text':'password'} autoComplete="current-password" value={password} onChange={e=>setPassword(e.target.value)} required/><button type="button" aria-label={show?'Hide password':'Show password'} onClick={()=>setShow(!show)}>◉</button></span></label><div className="login-options"><label><input type="checkbox"/> Remember me</label></div><button className="btn dark full" disabled={busy}>{busy?'Signing in…':<>Log in <Icon name="chevron"/></>}</button><p className="login-note">Protected by your organization’s access policy.</p></form></section></main>
}
