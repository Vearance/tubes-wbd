import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router'
import { api, session } from '../api'
import { Icon } from '../components/Icon'
import { Button, ErrorNotice } from '../components/ui'
import { cn } from '../lib/utils'

const brandClasses =
  'inline-flex min-h-11 items-center gap-2.5 text-lg font-black tracking-tight text-ink no-underline'
const inputClasses =
  'mt-2 h-12 w-full rounded-xl border border-line-strong bg-white px-3.5 text-base sm:text-sm text-ink outline-none transition-colors focus:border-violet focus:ring-2 focus:ring-violet/25'

export function LoginPage() {
  const navigate = useNavigate()
  const existing = session.user()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (existing) {
    return (
      <Navigate
        to={existing.is_admin ? '/admin/users' : '/databases'}
        replace
      />
    )
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const result = await api.login(username, password)
      session.save(result)
      navigate(result.user.is_admin ? '/admin/users' : '/databases', {
        replace: true,
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to sign in')
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="grid min-h-dvh grid-cols-[minmax(520px,1.05fr)_minmax(520px,.95fr)] bg-white max-[900px]:grid-cols-[minmax(0,1fr)]">
      <section className="relative flex min-h-dvh flex-col overflow-hidden bg-[#f8eef7] px-16 py-12 before:absolute before:right-[-130px] before:bottom-[-120px] before:size-[500px] before:rounded-full before:bg-[#e7d8fb] before:content-[''] after:absolute after:right-[250px] after:bottom-[-150px] after:size-[280px] after:rounded-full after:bg-[#ffd6e8] after:content-[''] max-[900px]:hidden">
        <a className={brandClasses} href="/login">
          <img
            src="/bibi.png"
            alt="BScheme mascot"
            className="size-8 object-contain"
          />
          BScheme
        </a>
        <div className="z-[1] mt-[9vh] max-w-[580px]">
          <p className="mt-0 mb-5 text-[clamp(48px,4.1vw,64px)] leading-[.98] font-black tracking-tight text-ink">
            Browse tables.
            <br />
            Run SQL.
            <br />
            Review changes.
          </p>
          <p className="max-w-[470px] text-sm leading-[1.7] text-muted">
            One shared workspace for your team’s data, schema, and migration
            history.
          </p>
        </div>
        <div className="relative z-[1] mt-auto mr-[-22px] mb-[-38px] ml-auto h-[400px] w-[min(560px,78%)] before:absolute before:right-8 before:bottom-[34px] before:h-[300px] before:w-[370px] before:rounded-full before:border before:border-violet/25 before:bg-[#ffffff59] before:content-['']">
          <span className="absolute bottom-[66px] left-[-10px] text-8xl leading-none font-extrabold text-violet/20 select-none">
            Bibi
          </span>
          <img
            className="absolute right-0 bottom-[-16px] size-[440px] object-contain drop-shadow-[0_26px_28px_rgba(72,34,96,.16)]"
            src="/bibi.png"
            alt="Bibi, the BScheme mascot"
          />
        </div>
      </section>

      <section className="grid place-items-center p-12 max-[900px]:min-h-dvh max-[900px]:p-6 max-[600px]:p-5">
        <form
          className="w-[min(100%,410px)] max-[600px]:w-full"
          onSubmit={submit}
        >
          <div className="mb-6 flex items-center gap-2.5 min-[901px]:hidden">
            <img
              src="/bibi.png"
              alt="BScheme mascot"
              className="size-8 object-contain"
            />
            <span className="text-xl font-black tracking-tight text-ink">
              BScheme
            </span>
          </div>
          <div className="mb-2">
            <h1 className="my-1 text-[32px] font-extrabold tracking-tight text-ink">
              Welcome!
            </h1>
            <p className="mt-1 mb-6 text-sm text-muted">
              Please enter your account details to sign in.
            </p>
          </div>

          {error && <ErrorNotice message={error} onDismiss={() => setError('')} />}

          <label className="mt-4 block">
            <span className="flex items-center text-xs font-bold text-ink">
              Username
              <span className="ml-auto text-xs font-medium text-muted">
                Required
              </span>
            </span>
            <input
              className={inputClasses}
              type="text"
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="e.g. alex"
              required
              autoFocus
            />
          </label>

          <label className="mt-4 block">
            <span className="flex items-center text-xs font-bold text-ink">
              Password
              <span className="ml-auto text-xs font-medium text-muted">
                Required
              </span>
            </span>
            <span className="relative block">
              <input
                className={cn(inputClasses, 'pr-12')}
                type={show ? 'text' : 'password'}
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                required
              />
              <button
                className="absolute right-1 top-1/2 -translate-y-1/2 mt-1 size-10 rounded-lg grid place-items-center text-muted hover:text-ink transition-colors"
                type="button"
                aria-label={show ? 'Hide password' : 'Show password'}
                onClick={() => setShow(!show)}
              >
                <Icon name={show ? 'eye-off' : 'eye'} className="size-4" />
              </button>
            </span>
          </label>

          <div className="mt-6">
            <Button
              type="submit"
              variant="dark"
              loading={busy}
              className="w-full h-12 text-sm"
            >
              Log in <Icon name="chevron" className="size-4" />
            </Button>
          </div>

          <p className="mt-6 text-center text-xs text-muted">
            Protected by your organization’s access policy.
          </p>
        </form>
      </section>
    </main>
  )
}
