import { AlertDialog } from '@base-ui/react/alert-dialog'
import type { ReactNode } from 'react'
import { Icon } from './Icon'

export function Button({children, variant='', type='button', disabled=false, onClick}: {children: ReactNode; variant?: string; type?: 'button'|'submit'; disabled?: boolean; onClick?: () => void}) {
  return <button className={`btn ${variant}`} type={type} disabled={disabled} onClick={onClick}>{children}</button>
}
export function Search({value, onChange, placeholder='Search'}: {value: string; onChange: (value: string) => void; placeholder?: string}) {
  return <label className="search-box"><Icon name="search"/><span className="sr-only">{placeholder}</span><input value={value} onChange={e=>onChange(e.target.value)} placeholder={placeholder}/></label>
}
export function EmptyState({icon='database', title, children, action}: {icon?: Parameters<typeof Icon>[0]['name']; title: string; children: ReactNode; action?: ReactNode}) {
  return <div className="empty-state"><span><Icon name={icon}/></span><strong>{title}</strong><p>{children}</p>{action}</div>
}
export function ErrorNotice({message}: {message: string}) { return <div className="notice error" role="alert">{message}</div> }
export function Loading({page=false}:{page?:boolean}) { return <div className={`loading ${page?'page-loading':''}`} role="status"><span className="loading-mark"><Icon name="database"/></span>Loading workspace…</div> }
export function Status({children, tone='violet'}: {children: ReactNode; tone?: 'violet'|'accent'|'green'|'amber'|'red'}) { return <span className={`status ${tone}`}>{children}</span> }

export function ConfirmAction({trigger,title,description,confirmLabel,onConfirm,triggerClassName='btn',triggerAriaLabel}:{trigger:ReactNode;title:string;description:string;confirmLabel:string;onConfirm:()=>void|Promise<void>;triggerClassName?:string;triggerAriaLabel?:string}) {
  return <AlertDialog.Root>
    <AlertDialog.Trigger className={triggerClassName} aria-label={triggerAriaLabel}>{trigger}</AlertDialog.Trigger>
    <AlertDialog.Portal>
      <AlertDialog.Backdrop className="alert-dialog-backdrop"/>
      <AlertDialog.Viewport className="alert-dialog-viewport">
        <AlertDialog.Popup className="alert-dialog-popup">
          <AlertDialog.Title className="alert-dialog-title">{title}</AlertDialog.Title>
          <AlertDialog.Description className="alert-dialog-description">{description}</AlertDialog.Description>
          <div className="alert-dialog-actions">
            <AlertDialog.Close className="btn">Cancel</AlertDialog.Close>
            <AlertDialog.Close className="btn danger" onClick={()=>void onConfirm()}>{confirmLabel}</AlertDialog.Close>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Viewport>
    </AlertDialog.Portal>
  </AlertDialog.Root>
}
